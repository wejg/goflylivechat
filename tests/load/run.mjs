// Node.js 22+; starts disposable Docker containers and never targets the existing app/database.
import assert from 'node:assert/strict';
import { execFile, spawn } from 'node:child_process';
import { createServer, request as httpRequest } from 'node:http';
import { randomBytes, createHash } from 'node:crypto';
import { gunzipSync } from 'node:zlib';
import { readFile, writeFile, mkdir } from 'node:fs/promises';
import { setTimeout as sleep } from 'node:timers/promises';
import { resolve } from 'node:path';

const duration = Number(process.env.LOADTEST_SECONDS || 600);
const visitorCount = Number(process.env.LOADTEST_VISITORS || 100);
const agentCount = Number(process.env.LOADTEST_AGENTS || 5);
const connectBatch = Number(process.env.LOADTEST_CONNECT_BATCH || 0);
const connectDelayMs = Number(process.env.LOADTEST_CONNECT_DELAY_MS || 0);
const memoryMiB = Number(process.env.LOADTEST_MEMORY_MIB || 512);
assert(Number.isInteger(duration) && duration >= 5 && duration <= 3600);
assert(Number.isInteger(visitorCount) && visitorCount >= 10 && visitorCount <= 2000);
assert(Number.isInteger(agentCount) && agentCount >= 1 && agentCount <= visitorCount);
assert(Number.isInteger(connectBatch) && connectBatch >= 0 && connectBatch <= visitorCount);
assert(Number.isInteger(connectDelayMs) && connectDelayMs >= 0 && connectDelayMs <= 10000);
assert(Number.isInteger(memoryMiB) && memoryMiB >= 256 && memoryMiB <= 8192);
const runId = new Date().toISOString().replace(/[-:.TZ]/g, '') + '-' + randomBytes(3).toString('hex');
const prefix = `gofly-load-${runId}`;
const names = { app: `${prefix}-app`, db: `${prefix}-db`, network: prefix, image: `gofly-loadtest:${runId}` };
const output = resolve(process.env.LOADTEST_OUTPUT || `reports/loadtest-${runId}`);
await mkdir(output, { recursive: true });
const password = randomBytes(24).toString('hex');
const secret = randomBytes(32).toString('hex');
const env = { ...process.env, MARIADB_ROOT_PASSWORD: password, GOFLY_DB_PASSWORD: password, GOFLY_JWT_SECRET: secret };
const faultEnabled = process.env.LOADTEST_FAULT_TEST === '1';
const limitBoundaryEnabled = process.env.LOADTEST_LIMIT_BOUNDARY === '1';
const report = {
    runId, startedAt: new Date().toISOString(), status: 'running',
    config: { visitors: visitorCount, agents: agentCount, seconds: duration, intervalSeconds: 5, appCPU: 2, dbCPU: 2, memoryMiBEach: memoryMiB,
        pacing: 'five seconds after each completed wave; no catch-up',
        sourceIPs: 'distinct X-Forwarded-For per virtual client; only the isolated network gateway is trusted; business rate limiter unchanged' },
    requestErrors: [], waves: [], clockGaps: [], faultInjection: faultEnabled ? { enabled: true } : { enabled: false }, limitBoundary: limitBoundaryEnabled ? { enabled: true } : { enabled: false }, phases: [], checks: [], issues: [], phaseFailures: {}, resources: [], requests: {}, unexpectedCloses: 0,
};
const sockets = new Set(), records = [], recordsByContent = new Map(), background = new Set(), horizons = new Map();
const latencies = [], closes = [], created = new Set();
let base, faultBase, faultProxy, faultProxyResponse, agents = [], visitors = [], phase = 'setup', stats, clockMonitor, stopping = false, heartbeat;
let failures = 0, interrupted = false, cleaning = false, nextWaveAt = 0;
for (const signal of ['SIGINT', 'SIGTERM']) process.once(signal, () => {
    interrupted = true;
    log(`${signal}: stopping after the in-flight operation and cleaning up this run`);
});
function issue(message) { failures++; report.phaseFailures[phase] = (report.phaseFailures[phase] || 0) + 1; if (report.issues.length < 30) report.issues.push(`${phase}: ${message}`); }
function log(message) { console.log(`[${new Date().toISOString()}] ${message}`); }
function command(program, args, options = {}) {
    if (interrupted && !cleaning) return Promise.reject(new Error('Load test interrupted'));
    const { input, timeout = 120000, includeStderr = false } = options;
    return new Promise((yes, no) => {
        const child = execFile(program, args, { env, timeout, maxBuffer: 64 * 1024 * 1024 }, (error, stdout, stderr) => {
            if (error) no(new Error(`${program} ${args.slice(0, 3).join(' ')} failed: ${stderr || error.message}`));
            else yes((stdout + (includeStderr ? stderr : "")).trim());
        });
        child.stdin.on('error', () => {});
        child.stdin.end(input);
    });
}
const docker = (args, options) => command('docker', args, options);
const sql = input => docker(['exec', '-i', names.db, 'sh', '-c', 'MYSQL_PWD="$MARIADB_ROOT_PASSWORD" mariadb -uroot --batch --raw --skip-column-names gofly_loadtest'], { input });
async function waitFor(fn, description, timeout = 30000) {
    const until = Date.now() + timeout;
    while (Date.now() < until) { if (interrupted) throw new Error("Load test interrupted"); if (await fn()) return; await sleep(100); }
    throw new Error(`Timed out: ${description}`);
}
async function api(endpoint, { agent, ip, data, token, expectedCode = 200, metric, fault = false } = {}) {
    const started = performance.now();
    const key = metric || `${data ? 'POST' : 'GET'} ${endpoint.split('?')[0]}`;
    const group = report.requests[key] ||= { count: 0, errors: 0, ms: [] };
    group.count++;
    let httpStatus, businessCode, responseMessage;
    try {
        const response = await fetch((fault ? faultBase : base) + endpoint, {
            method: data ? 'POST' : 'GET',
            headers: { 'X-Forwarded-For': ip || agent?.ip || '10.250.0.1', ...(fault ? { 'X-Loadtest-Drop-Response': '1' } : {}), ...(token || agent?.token ? { token: token || agent.token } : {}) },
            body: data ? new URLSearchParams(data) : undefined,
            signal: AbortSignal.timeout(10000),
        });
        httpStatus = response.status;
        const body = await response.json();
        businessCode = body.code; responseMessage = body.msg;
        assert.equal(response.status, 200, `${endpoint} HTTP status`);
        assert.equal(body.code, expectedCode, `${endpoint}: ${JSON.stringify(body).slice(0, 240)}`);
        return body.result ?? body;
    } catch (error) {
        group.errors++;
        report.requestErrors.push({ at: new Date().toISOString(), phase, endpoint: key, ip: ip || agent?.ip,
            content: data?.content, visitorId: data?.visitor_id, elapsedMs: performance.now() - started,
            httpStatus, businessCode, responseMessage, name: error.name, message: error.message,
            cause: error.cause ? { code: error.cause.code, message: error.cause.message } : undefined });
        throw new Error(`${key}: ${error.message}${error.cause ? `; cause: ${error.cause.code || ''} ${error.cause.message}` : ''}`);
    }
    finally { group.ms.push(performance.now() - started); }
}
function expectedCounts(agent) {
    const counts = {};
    for (const record of records) {
        if (record.accepted && record.kind === 'visitor' && record.visitor.agent === agent && record.id > (horizons.get(record.visitor.id) || 0)) {
            counts[record.visitor.id] = (counts[record.visitor.id] || 0) + 1;
        }
    }
    return counts;
}
async function summary(agent, exact = false) {
    const result = await api('/message_unread', { agent });
    assert.equal(result.total, Object.values(result.counts).reduce((a, b) => a + b, 0), 'summary total');
    for (const id of Object.keys(result.counts)) assert(visitors.some(v => v.id === id && v.agent === agent), `cross-agent unread leak: ${id}`);
    if (exact) assert.deepEqual(result.counts, expectedCounts(agent), `agent ${agent.name}: unread count mismatch`);
    return result;
}
function receive(label, text) {
    try {
        const msg = JSON.parse(text);
        if (msg.type !== 'message') return;
        const record = recordsByContent.get(msg.data?.content);
        if (!record) return;
        record.deliveries[label] = (record.deliveries[label] || 0) + 1;
        record.receivedIds.push(msg.data.message_id);
        if (!record.expected.includes(label)) issue(`Wrong recipient: ${label} for ${record.content}`);
        if (record.deliveries[label] > 1) issue(`Duplicate delivery: ${label} for ${record.content}`);
        const agentChannel = label.startsWith('agent:');
        assert.equal(msg.data.id, record.kind === 'visitor' || agentChannel ? record.visitor.id : record.visitor.agent.name, 'WebSocket conversation/sender ID');
        assert.equal(msg.data.to_id, record.kind === 'visitor' ? record.visitor.agent.name : record.visitor.id, 'WebSocket recipient ID');
        assert.equal(msg.data.name, record.kind === 'visitor' ? record.visitor.id : record.visitor.agent.name, 'WebSocket sender name');
        assert.equal(msg.data.is_kefu, record.kind === 'kefu' && agentChannel ? 'yes' : 'no', 'WebSocket direction');
        if (record.phase === 'steady') latencies.push(performance.now() - record.started);
        if (label.startsWith('agent:') && record.kind === 'visitor') {
            const job = summary(record.visitor.agent).catch(error => issue(error.message)).finally(() => background.delete(job));
            background.add(job);
        }
    } catch (error) { issue(`WebSocket ${label}: ${error.message}`); }
}
async function connect(client, kind) {
    const label = `${kind}:${client.id}`;
    const url = kind === 'agent' ? `/ws_kefu?token=${encodeURIComponent(client.token)}` : `/ws_visitor?visitor_id=${encodeURIComponent(client.id)}`;
    const socket = new WebSocket(base.replace('http:', 'ws:') + url);
    socket.plannedClose = false;
    socket.addEventListener('message', event => receive(label, event.data));
    socket.addEventListener('close', event => {
        sockets.delete(socket);
        closes.push({ label, planned: socket.plannedClose || stopping, code: event.code, phase });
        if (!socket.plannedClose && !stopping) { report.unexpectedCloses++; issue(`Unexpected socket close ${label}, code ${event.code}`); }
    });
    socket.addEventListener('error', () => { if (!socket.plannedClose && !stopping) issue(`WebSocket error ${label}`); });
    await new Promise((yes, no) => {
        const timer = setTimeout(() => no(new Error(`WebSocket connect timeout ${label}`)), 10000);
        socket.addEventListener('open', () => { clearTimeout(timer); yes(); }, { once: true });
        socket.addEventListener('error', () => { clearTimeout(timer); no(new Error(`WebSocket connection failed ${label}`)); }, { once: true });
    });
    client.socket = socket;
    sockets.add(socket);
    socket.send(JSON.stringify({ type: 'ping', data: kind === 'visitor' ? 'visitor:' + client.id : '' }));
}

async function connectMany(clients, kind) {
    const batchSize = connectBatch || clients.length;
    for (let offset = 0; offset < clients.length; offset += batchSize) {
        await Promise.all(clients.slice(offset, offset + batchSize).map(client => connect(client, kind)));
        if (connectDelayMs && offset + batchSize < clients.length) await sleep(connectDelayMs);
    }
}

async function closeClient(client) {
    if (!client.socket || client.socket.readyState === WebSocket.CLOSED) return;
    client.socket.plannedClose = true;
    const socket = client.socket;
    const started = performance.now();
    const closed = new Promise(yes => socket.addEventListener('close', () => yes(true), { once: true }));
    socket.close(1000);
    if (!await Promise.race([closed, sleep(5000).then(() => false)])) {
        (report.closeCompletionTimeouts ||= []).push({ client: client.id, phase, waitedMs: performance.now() - started, readyState: socket.readyState });
        failures++; // Detailed close failures have their own array, preserving room for request errors.
    }
}


async function runLimitBoundary(visitor) {
    const content = `LT:${runId}:${visitor.id}:limit-boundary:accepted:` + 'b'.repeat(128);
    const key = `${runId}:${visitor.id}:limit-boundary`;
    await sleep(500);
    const blocked = await api('/2/message', { ip: visitor.ip, expectedCode: 400, metric: 'limit boundary blocked', data: { type: 'visitor', from_id: visitor.id, to_id: visitor.agent.name, content: content + ':blocked' } });
    assert.match(blocked.msg, /发送频率过快/);
    await sleep(3500);
    const accepted = await api('/2/message', { ip: visitor.ip, metric: 'limit boundary accepted', data: { type: 'visitor', from_id: visitor.id, to_id: visitor.agent.name, content, client_message_id: key } });
    const row = (await sql(`SELECT id,content FROM message WHERE content='${content}';`)).split('\t');
    assert.equal(Number(row[0]) > 0, true);
    const synthetic = { content, visitor, kind: 'visitor', id: Number(row[0]), accepted: true, deliveries: {}, receivedIds: [], expected: [] };
    records.push(synthetic); recordsByContent.set(content, synthetic);
    await acknowledge(visitor, synthetic.id);
    report.limitBoundary = { enabled: true, rejectedAfter500ms: true, acceptedAfter4000ms: true, messageId: accepted.message_id };
}

async function runFaultInjection(visitor) {
    const content = `LT:${runId}:${visitor.id}:fault:response-drop:` + 'f'.repeat(128);
    const prefix = `LT:${runId}:${visitor.id}:fault:response-drop:`;
    const key = `${runId}:${visitor.id}:fault`;
    const beforeRaw = await sql(`SELECT COUNT(*) FROM message WHERE content LIKE '${prefix}%';`);
    const before = Number(beforeRaw);
    let dropped = false;
    await sleep(4100);
    try {
        await api('/2/message', { ip: visitor.ip, fault: true, metric: 'fault response drop', data: { type: 'visitor', from_id: visitor.id, to_id: visitor.agent.name, content, client_message_id: key } });
    } catch (error) {
        dropped = true;
        report.faultInjection.clientError = error.message;
    }
    await sleep(500);
    const afterDropRaw = await sql(`SELECT COUNT(*) FROM message WHERE content LIKE '${prefix}%';`);
    const afterDrop = Number(afterDropRaw);
    report.faultInjection.afterDropRaw = afterDropRaw;
    report.faultInjection.upstreamResponseBody = faultProxyResponse;
    report.faultInjection.rowsAfterDrop = await sql(`SELECT id,content FROM message WHERE content LIKE '${prefix}%';`);
    assert.equal(before, 0, 'fault probe content already exists');
    assert.equal(afterDrop, 1, 'response drop should still leave one saved message');
    assert(dropped, 'response drop must be visible to the client');
    await sleep(4100);
    const retry = await api('/2/message', { ip: visitor.ip, metric: 'fault retry', data: { type: 'visitor', from_id: visitor.id, to_id: visitor.agent.name, content, client_message_id: key } });
    assert(Number.isInteger(retry.message_id) && retry.message_id > 0);
    const afterRetry = Number(await sql(`SELECT COUNT(*) FROM message WHERE content LIKE '${prefix}%';`));
    const maxFaultId = Number(await sql(`SELECT COALESCE(MAX(id),0) FROM message WHERE content LIKE '${prefix}%';`));
    const faultRows = (await sql(`SELECT id,content FROM message WHERE content LIKE '${prefix}%' ORDER BY id;`)).split('\n').filter(Boolean);
    for (const line of faultRows) {
        const [id, savedContent] = line.split('\t');
        const synthetic = { content: savedContent, visitor, kind: 'visitor', id: Number(id), accepted: true, deliveries: {}, receivedIds: [], expected: [] };
        records.push(synthetic); recordsByContent.set(savedContent, synthetic);
    }
    await acknowledge(visitor, maxFaultId);
    report.faultInjection = { enabled: true, clientObservedError: dropped, savedAfterDroppedResponse: afterDrop, savedAfterRetry: afterRetry, duplicateOnBlindRetry: afterRetry !== 1 };
    assert.equal(afterRetry, 1, 'idempotent retry must not duplicate the saved message');
}

async function login(agent) {
    const result = await api('/check', { ip: agent.ip, data: { username: agent.name, password }, metric: 'login' });
    agent.token = result.token;
}
async function send(visitor, kind, sequence) {
    const content = `LT:${runId}:${visitor.id}:${kind}:${sequence}:` + 'x'.repeat(128);
    const record = { content, visitor, kind, phase, started: performance.now(), clientMessageID: `${runId}:${visitor.id}:${kind}:${sequence}`, accepted: false, deliveries: {}, receivedIds: [],
        expected: kind === 'visitor' ? [`agent:${visitor.agent.id}`] : [`agent:${visitor.agent.id}`, `visitor:${visitor.id}`] };
    records.push(record); recordsByContent.set(content, record);
    const result = kind === 'visitor'
        ? await api('/2/message', { ip: visitor.ip, data: { type: 'visitor', from_id: visitor.id, to_id: visitor.agent.name, content, client_message_id: record.clientMessageID } })
        : await api('/kefu/message', { agent: visitor.agent, data: { type: 'kefu', to_id: visitor.id, content, client_message_id: record.clientMessageID } });
    assert(Number.isInteger(result.message_id) && result.message_id > 0, 'successful message response must include its saved ID');
    record.id = result.message_id; record.accepted = true;
    return record;
}
async function acknowledge(visitor, id) {
    if (!id) return;
    let lastError;
    for (let attempt = 0; attempt < 3; attempt++) {
        try {
            await api('/message_status', { agent: visitor.agent, data: { visitor_id: visitor.id, last_message_id: String(id) } });
            horizons.set(visitor.id, Math.max(id, horizons.get(visitor.id) || 0));
            return;
        } catch (error) {
            lastError = error;
            if (attempt < 2) await sleep(250 * (attempt + 1));
        }
    }
    throw lastError;
}
async function drain() {
    await waitFor(() => records.every(r => !r.accepted || r.expected.every(label => r.deliveries[label] === 1)), 'all saved messages delivered', 10000);
    while (background.size) await Promise.all([...background]);
}
async function verify(description) {
    const check = { description, status: 'passed' };
    try {
        await drain();
        for (const record of records.filter(r => r.accepted)) assert(record.receivedIds.every(id => id === record.id), 'WebSocket ID differs from persisted message ID');
        await Promise.all(agents.map(a => summary(a, true)));
        assert.equal(report.phaseFailures[phase] || 0, 0, 'phase contains request/transport failures');
    } catch (error) { check.status = 'failed'; issue(`${description}: ${error.message}`); }
    report.checks.push({ ...check, messages: records.filter(r => r.accepted).length, trackedSockets: sockets.size, openSockets: [...sockets].filter(s => s.readyState === WebSocket.OPEN).length, closingSockets: [...sockets].filter(s => s.readyState === WebSocket.CLOSING).length, at: new Date().toISOString() });
}
async function wave(active, sequence) {
    // Phase transitions must respect the same per-client pause as steady traffic.
    await sleep(Math.max(0, nextWaveAt - performance.now()));
    const waveSample = { phase, sequence, at: new Date().toISOString(), started: performance.now() };
    report.waves.push(waveSample);
    const jobs = active.map(v => send(v, 'visitor', sequence));
    for (const agent of agents) {
        const owned = active.filter(v => v.agent === agent);
        if (!owned.length) continue;
        jobs.push(send(owned[sequence % owned.length], 'kefu', sequence));
        const read = owned.filter((_, index) => (index + sequence) % 5 === 0);
        for (const v of read) {
            const latest = records.filter(r => r.accepted && r.visitor === v && r.kind === 'visitor').at(-1);
            if (latest) jobs.push(acknowledge(v, latest.id));
        }
        const target = owned[sequence % owned.length];
        jobs.push(api(`/2/messagesPages?visitor_id=${target.id}&page=1&pagesize=15`, { agent }).then(result => {
            assert(result.list.every(row => row.visitor_id === target.id), 'history contains another conversation');
        }));
    }
    const done = await Promise.allSettled(jobs);
    for (const result of done) if (result.status === 'rejected') issue(result.reason.message);
    try { await drain(); } catch (error) { issue(error.message); }
    waveSample.elapsedMs = performance.now() - waveSample.started;
    nextWaveAt = performance.now() + 5000;
}
function percentile(values, quantile) {
    if (!values.length) return null;
    const sorted = [...values].sort((a, b) => a - b);
    return Number(sorted[Math.max(0, Math.ceil(quantile * sorted.length) - 1)].toFixed(2));
}
assert.equal(percentile([5, 1, 4, 2, 3], .95), 5);
assert.equal(percentile([5, 1, 4, 2, 3], .5), 3);
assert.equal(percentile([], .95), null);

try {
    const head = await command('git', ['rev-parse', 'HEAD']);
    const diff = await command('git', ['diff', 'HEAD', '--', '*.go', 'go.mod', 'go.sum', 'Dockerfile']);
    report.source = { head, workingCodeDiffSHA256: createHash('sha256').update(diff).digest('hex') };
    report.host = JSON.parse(await docker(['info', '--format', '{"cpus":{{.NCPU}},"memoryBytes":{{.MemTotal}},"docker":"{{.ServerVersion}}"}']));
    log('Building a dedicated snapshot of the current workspace');
    await writeFile(output + '/build.log', await docker(['build', '-t', names.image, '.'], { timeout: 300000, includeStderr: true }));
    created.add('image');
    report.source.imageId = await docker(['image', 'inspect', names.image, '--format', '{{.Id}}']);
    await docker(['network', 'create', names.network]); created.add('network');
    const gateway = JSON.parse(await docker(['network', 'inspect', names.network]))[0].IPAM.Config[0].Gateway;
    await docker(['run', '-d', '--name', names.db, '--network', names.network, '--network-alias', 'db', '--cpus=2', `--memory=${memoryMiB}m`,
        '-e', 'MARIADB_ROOT_PASSWORD', '-e', 'MARIADB_DATABASE=gofly_loadtest', 'm.daocloud.io/docker.io/library/mariadb:10.11']);
    created.add('db');
    await waitFor(async () => { try { await sql('SELECT 1;'); return true; } catch { return false; } }, 'isolated MariaDB ready', 60000);
    await sql(await readFile('import.sql', 'utf8'));
    agents = Array.from({ length: agentCount }, (_, i) => ({ id: `a${i}`, name: `load_agent_${i}`, ip: `10.88.1.${i + 1}` }));
    visitors = Array.from({ length: visitorCount }, (_, i) => ({ id: `load_visitor_${i}`, ip: `10.88.${2 + Math.floor(i / 250)}.${(i % 250) + 1}`, agent: agents[i % agentCount] }));
    const hash = createHash('md5').update(password).digest('hex');
    await sql(agents.map(a => `INSERT INTO user (name,password,nickname,avator) VALUES ('${a.name}','${hash}','${a.name}','/static/images/4.jpg');`).join('\n') + '\n' +
        visitors.map(v => `INSERT INTO visitor (name,visitor_id,to_id,status,avator,client_ip) VALUES ('${v.id}','${v.id}','${v.agent.name}',0,'/static/images/2.png','${v.ip}');`).join('\n'));
    await docker(['run', '-d', '--name', names.app, '--network', names.network, '--cpus=2', `--memory=${memoryMiB}m`, '-p', '127.0.0.1::8081',
        '-e', 'GOFLY_DB_SERVER=db', '-e', 'GOFLY_DB_PORT=3306', '-e', 'GOFLY_DB_DATABASE=gofly_loadtest', '-e', 'GOFLY_DB_USERNAME=root',
        '-e', 'GOFLY_DB_PASSWORD', '-e', 'GOFLY_JWT_SECRET', '-e', `GOFLY_TRUSTED_PROXIES=${gateway}`, names.image, 'server']);
    created.add('app');
    const port = (await docker(['port', names.app, '8081/tcp'])).split(':').at(-1);
    base = `http://127.0.0.1:${port}`;
    await waitFor(async () => { try { return (await fetch(base + '/login', { signal: AbortSignal.timeout(1000) })).ok; } catch { return false; } }, 'isolated app ready');
    report.config.baseURL = base;
    if (faultEnabled) {
        const target = new URL(base);
        faultProxy = createServer((req, res) => {
            const upstream = httpRequest({ hostname: target.hostname, port: target.port, path: req.url, method: req.method, headers: { ...req.headers, host: target.host } }, upstreamRes => {
                if (req.headers['x-loadtest-drop-response'] === '1') {
                    const chunks = [];
                    upstreamRes.on('data', chunk => chunks.push(chunk));
                    upstreamRes.on('end', () => { const raw = Buffer.concat(chunks);
                        try { faultProxyResponse = gunzipSync(raw).toString(); } catch { faultProxyResponse = raw.toString(); }
                        res.destroy(); });
                    return;
                }
                res.writeHead(upstreamRes.statusCode || 502, upstreamRes.headers);
                upstreamRes.pipe(res);
            });
            upstream.on('error', error => res.destroy(error));
            req.pipe(upstream);
        });
        await new Promise(resolveProxy => faultProxy.listen(0, '127.0.0.1', resolveProxy));
        faultBase = `http://127.0.0.1:${faultProxy.address().port}`;
    }
    await Promise.all(agents.map(async a => { await login(a); }));
    await connectMany(agents, 'agent');
    heartbeat = setInterval(() => {
        for (const a of agents) if (a.socket?.readyState === WebSocket.OPEN) a.socket.send(JSON.stringify({ type: 'ping', data: '' }));
        for (const v of visitors) if (v.socket?.readyState === WebSocket.OPEN) v.socket.send(JSON.stringify({ type: 'ping', data: 'visitor:' + v.id }));
    }, 20000);
    stats = spawn('docker', ['stats', '--format', '{{json .}}', names.app, names.db]);
    let statsText = '';
    stats.stdout.on('data', chunk => {
        statsText += chunk;
        const lines = statsText.split('\n'); statsText = lines.pop();
        for (const line of lines) { try { report.resources.push({ at: new Date().toISOString(), phase, ...JSON.parse(line.replace(/\x1b\[[0-9;]*[A-Za-z]/g, '')) }); } catch {} }
    });
    stats.stderr.on('data', () => {});
    stats.on('error', error => issue('resource monitor: ' + error.message));
    let lastWall = Date.now(), lastMono = performance.now();
    clockMonitor = setInterval(() => {
        const wall = Date.now(), mono = performance.now();
        const wallMs = wall - lastWall, monotonicMs = mono - lastMono;
        if (wallMs > 1500 || monotonicMs > 1500 || Math.abs(wallMs - monotonicMs) > 100) {
            report.clockGaps.push({ at: new Date().toISOString(), wallMs, monotonicMs });
        }
        lastWall = wall; lastMono = mono;
    }, 1000);
    phase = 'rate-limit';
    log('Checking same-sender message rate limiting');
    const v = visitors[0];
    await connect(v, 'visitor');
    const first = await send(v, 'visitor', 'limit-probe');
    const blocked = await api('/2/message', { ip: v.ip, expectedCode: 400, metric: 'expected rate-limit rejection', data: { type: 'visitor', from_id: v.id, to_id: v.agent.name, content: 'blocked rate-limit probe' } });
    assert.match(blocked.msg, /发送频率过快/);
    await acknowledge(v, first.id); await verify('same-sender limit accepted first and rejected immediate second message');
    if (limitBoundaryEnabled) await runLimitBoundary(v);
    if (faultEnabled) await runFaultInjection(v);
    await sleep(3100);
    let sequence = 0;
    for (const [count, seconds, name] of [[Math.min(10, visitorCount), 10, 'warmup-10'], [Math.min(50, visitorCount), 10, 'warmup-50'], [visitorCount, duration, 'steady']]) {
        phase = name;
        await connectMany(visitors.slice(0, count).filter(v => !v.socket), 'visitor');
        assert.equal(sockets.size, count + agents.length, 'connection count');
        const start = performance.now(), initial = records.length;
        log(`${name}: ${count} visitors + ${agents.length} agents, ${seconds}s`);
        let round = 0;
        while (round < Math.ceil(seconds / 5) && (round === 0 || performance.now() - start < seconds * 1000)) {
            if (interrupted) throw new Error('Load test interrupted');
            await wave(visitors.slice(0, count), sequence++);
            if (round % 12 === 0) { await verify(`${name} checkpoint ${round}`); log(`${name}: ${Math.floor((performance.now() - start) / 1000)}s, saved ${records.length}, sockets ${sockets.size}, errors ${failures}`); }
            round++;
            // Each client waits after completion; never bunch delayed rounds to catch up.
            const remaining = start + seconds * 1000 - performance.now();
            await sleep(Math.max(0, Math.min(5000, remaining)));
            if (remaining <= 5000) break;
        }
        const elapsed = (performance.now() - start) / 1000;
        await verify(`${name} complete`);
        report.phases.push({ name, visitors: count, seconds: elapsed, attemptedMessages: records.length - initial, acceptedMessages: records.slice(initial).filter(r => r.accepted).length, status: report.phaseFailures[name] ? 'failed' : 'passed' });
        if (name === 'steady') report.steadyOutcome = { status: report.phaseFailures.steady ? 'failed' : 'passed', unexpectedCloses: report.unexpectedCloses, failures, completedAt: new Date().toISOString() };
    }
    phase = 'reconnect';
    log('Checking planned reconnect, re-login and unread restoration');
    await Promise.all([agents[0], ...visitors.slice(0, 10)].map(closeClient));
    await login(agents[0]);
    await connectMany([agents[0]], 'agent');
    await connectMany(visitors.slice(0, 10), 'visitor');
    await verify('planned reconnect and new login retain unread counts');
    await wave(visitors, sequence++);
    await verify('fresh messages delivered after reconnect and re-login');
    phase = 'restart';
    log('Restarting only the isolated app to verify persisted unread state');
    await Promise.all([...agents, ...visitors].map(closeClient));
    await docker(['restart', names.app]);
    base = `http://127.0.0.1:${(await docker(['port', names.app, '8081/tcp'])).split(':').at(-1)}`;
    report.config.restartBaseURL = base;
    await waitFor(async () => { try { return (await fetch(base + '/login', { signal: AbortSignal.timeout(1000) })).ok; } catch { return false; } }, 'app restarted');
    await waitFor(() => sockets.size === 0, 'old sockets closed after isolated app restart');
    await Promise.all(agents.map(a => connect(a, 'agent')));
    await connectMany(visitors, 'visitor');
    await verify('app restart retains unread state for every agent');
    await wave(visitors, sequence++);
    await verify('fresh messages delivered after app restart');
    phase = 'final-audit';
    const rows = (await sql('SELECT id,visitor_id,kefu_id,mes_type,status,content FROM message ORDER BY id;')).split('\n').filter(Boolean).map(line => {
        const [id, visitorId, agentId, kind, status, content] = line.split('\t'); return { id: Number(id), visitorId, agentId, kind, status, content };
    });
    const saved = records.filter(r => r.accepted);
    report.messageAudit = { status: 'running', attempted: records.length, saved: saved.length, databaseRows: rows.length,
        expectedDeliveries: saved.reduce((n, r) => n + r.expected.length, 0),
        receivedDeliveries: saved.reduce((n, r) => n + Object.values(r.deliveries).reduce((a, b) => a + b, 0), 0),
        lost: saved.reduce((n, r) => n + r.expected.filter(label => !r.deliveries[label]).length, 0),
        duplicates: records.reduce((n, r) => n + Object.values(r.deliveries).reduce((a, b) => a + Math.max(0, b - 1), 0), 0),
        savedWithoutAcknowledgement: rows.filter(r => !recordsByContent.get(r.content)?.accepted).length,
        unread: saved.filter(r => r.kind === 'visitor' && r.id > (horizons.get(r.visitor.id) || 0)).length };
    await writeFile(output + '/database-audit.json', JSON.stringify(rows));
    assert.equal(rows.length, saved.length, 'database message count');
    const isFaultRow = row => faultEnabled && row.content.startsWith(`LT:${runId}:${visitors[0].id}:fault:response-drop:`);
    const nonFaultRows = rows.filter(row => !isFaultRow(row));
    assert.equal(new Set(nonFaultRows.map(r => r.content)).size, nonFaultRows.length, 'duplicate database messages');
    for (const row of rows) {
        if (isFaultRow(row)) {
            assert.equal(row.visitorId, visitors[0].id, 'fault row visitor');
            assert.equal(row.agentId, visitors[0].agent.name, 'fault row owner');
            assert.equal(row.kind, 'visitor', 'fault row direction');
            continue;
        }
        const record = recordsByContent.get(row.content);
        assert(record && record.accepted && row.id === record.id && row.visitorId === record.visitor.id && row.kind === record.kind && row.agentId === record.visitor.agent.name, 'database content/ID/owner mismatch');
        const expectedStatus = row.kind === 'visitor' && row.id <= (horizons.get(row.visitorId) || 0) ? 'read' : 'unread';
        assert.equal(row.status, expectedStatus, `database read state: ${row.id}`);
    }
    report.databaseStatus = await sql("SHOW GLOBAL STATUS WHERE Variable_name IN ('Threads_connected','Threads_running','Max_used_connections','Slow_queries');");
    assert.equal(report.messageAudit.lost, 0, 'saved messages missing delivery');
    assert.equal(report.messageAudit.duplicates, 0, 'duplicate delivery');
    report.messageAudit.status = 'passed';
    assert.equal(failures, 0);
    assert(report.resources.length > 0, 'resource samples were not captured');
    report.status = 'passed';
} catch (error) {
    report.status = interrupted ? 'interrupted' : 'failed'; report.fatalError = error.message; if (report.messageAudit?.status === 'running') report.messageAudit.status = 'failed'; issue(error.stack || error.message); log(`FAILED: ${error.message}`);
} finally {
    cleaning = true; stopping = true; clearInterval(heartbeat); faultProxy?.close(); clearInterval(clockMonitor); stats?.kill('SIGTERM');
    for (const socket of sockets) { socket.plannedClose = true; socket.close(); }
    await writeFile(output + '/message-ledger.json', JSON.stringify(records.map(({ visitor, ...record }) => ({ ...record, visitorId: visitor.id, agentName: visitor.agent.name }))));
    report.finishedAt = new Date().toISOString();
    report.failures = failures;
    report.closes = closes;
    report.messageLatencyMs = { samples: latencies.length, p50: percentile(latencies, .5), p95: percentile(latencies, .95), p99: percentile(latencies, .99), max: latencies.length ? Math.max(...latencies) : null };
    for (const group of Object.values(report.requests)) {
        group.p50 = percentile(group.ms, .5); group.p95 = percentile(group.ms, .95); group.p99 = percentile(group.ms, .99); delete group.ms;
    }
    if (created.has('app')) {
        try {
            const logs = await docker(['logs', names.app], { includeStderr: true });
            await writeFile(output + '/app.log', logs.replace(/token=[^&\s"]+/g, 'token=[REDACTED]'));
            const accessLogs = await docker(['exec', names.app, 'sh', '-c', 'cat /app/logs/*.log']);
            await writeFile(output + '/access.log', accessLogs.replace(/token=[^&\s"]+/g, 'token=[REDACTED]'));
            const state = JSON.parse(await docker(['inspect', names.app]))[0].State;
            report.containerState = { oomKilled: state.OOMKilled, restartCount: JSON.parse(await docker(['inspect', names.app]))[0].RestartCount };
        } catch (error) { issue('log collection: ' + error.message); report.status = 'failed'; }
    }
    await writeFile(output + '/result.json', JSON.stringify(report, null, 2));
    for (const service of ['app', 'db']) if (created.has(service)) await docker(['rm', '-f', '-v', names[service]]).catch(error => { report.status = 'failed'; issue('cleanup: ' + error.message); });
    if (created.has('network')) await docker(['network', 'rm', names.network]).catch(error => { report.status = 'failed'; issue('cleanup: ' + error.message); });
    if (created.has('image')) await docker(['image', 'rm', names.image]).catch(() => {});
    report.failures = failures;
    await writeFile(output + '/result.json', JSON.stringify(report, null, 2));
    log(`${report.status.toUpperCase()}: ${output}/result.json`);
    process.exitCode = report.status === 'passed' ? 0 : 1;
}
