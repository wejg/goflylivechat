const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');

const html = fs.readFileSync(path.join(__dirname, '../static/templates/chat_main.html'), 'utf8');
const script = html.match(/<script>\s*(var app=new Vue[\s\S]*?)<\/script>/)[1];

function chat(server = { messages: [], nextId: 1 }) {
    const requests = [], notifications = [], errors = [];
    function summary() {
        const counts = {};
        for (const m of server.messages) {
            if (m.mes_type === 'visitor' && m.status === 'unread') counts[m.visitor_id] = (counts[m.visitor_id] || 0) + 1;
        }
        return { counts, total: Object.values(counts).reduce((a, b) => a + b, 0) };
    }
    function ajax(request) {
        requests.push(request);
        if (request.url === '/message_unread') {
            request.reply = { code: 200, result: summary() };
            if (!server.deferUnread) request.success(request.reply);
        }
        if (request.url === '/message_status') {
            if (server.failRead) return request.success({ code: 500, msg: 'test database write failed' });
            for (const m of server.messages) {
                if (m.visitor_id === request.data.visitor_id && m.mes_type === 'visitor' && m.id <= request.data.last_message_id) m.status = 'read';
            }
            request.success({ code: 200 });
        }
        if (request.url === '/2/messagesPages') {
            request.reply = { code: 200, result: { list: server.messages.filter(m => m.visitor_id === request.data.visitor_id).map(m => ({ ...m })).reverse() } };
            if (!server.deferHistory) request.success(request.reply);
        }
    }
    const context = vm.createContext({
        Vue: function(options) { return Object.assign({}, options.data, options.methods); },
        getWsBaseUrl: () => 'ws://localhost',
        localStorage: { getItem: () => 'test-token' },
        replaceContent: value => value,
        window: { location: { origin: 'http://localhost' }, parent: { postMessage: value => notifications.push(value) } },
        document: { hidden: false },
        $: { ajax },
    });
    vm.runInContext(script, context);
    const app = context.app;
    Object.assign(app, {
        $set: (object, key, value) => { object[key] = value; },
        $message: value => errors.push(value),
        scrollBottom() {}, alertSound() {}, socket: { send() {} }, getVistorInfo() {},
    });
    app.currentGuest = 'a';
    app.visitor.visitor_id = 'a';
    app.users = [{ uid: 'a' }, { uid: 'b' }];
    return {
        app, context, server, requests, notifications, errors,
        receive(id = 'b', isKefu = 'no') {
            const messageId = server.nextId++;
            server.messages.push({ id: messageId, visitor_id: id, mes_type: isKefu === 'yes' ? 'kefu' : 'visitor', status: 'unread', content: 'hello' });
            app.OnMessage({ data: JSON.stringify({ type: 'message', data: {
                message_id: messageId, id, name: 'Guest', avator: '', content: 'hello', is_kefu: isKefu, time: '',
            } }) });
        },
        refresh() { app.getOnlineVisitors(); },
        respond() {
            requests.findLast(request => request.url === '/visitors_kefu_online').success({
                code: 200, result: [{ uid: 'a' }, { uid: 'b', last_message: 'hello' }],
            });
        },
        count(id = 'b') { return app.unreadVisitors[id] || 0; },
    };
}

test('counts every incoming message and retains counts across list switching and refresh', () => {
    const c = chat();
    c.receive(); c.receive();
    assert.equal(c.count(), 2);
    c.app.handleTabClick({ name: 'second' });
    c.app.handleTabClick({ name: 'first' });
    c.respond(); c.refresh(); c.respond();
    assert.equal(c.count(), 2);
    assert.equal(c.count('a'), 0);
});

test('restores unread counts from the server in a fresh page including offline visitors', () => {
    const c = chat();
    c.receive(); c.receive('offline');
    const restored = chat(c.server);
    restored.app.getUnreadMessages();
    assert.equal(restored.count(), 1);
    assert.equal(restored.count('offline'), 1);
    assert.deepEqual(JSON.parse(JSON.stringify(restored.notifications.at(-1))), { type: 'unread_count', count: 2 });
});

test('message received while online list is loading remains unread', () => {
    const c = chat();
    c.refresh(); c.receive(); c.respond();
    assert.equal(c.count(), 1);
});

test('opening a conversation acknowledges its displayed messages in the database', () => {
    const c = chat();
    c.receive(); c.receive();
    c.app.talkTo('b', 'Guest');
    assert.equal(c.count(), 0);
    assert.equal(c.server.messages.every(m => m.status === 'read'), true);
    c.receive();
    assert.equal(c.count(), 0);
    assert.equal(c.server.messages.at(-1).status, 'read');
});

test('failed history loading does not clear unread messages', () => {
    const c = chat();
    c.receive(); c.server.deferHistory = true;
    c.app.talkTo('b', 'Guest');
    c.requests.findLast(r => r.url === '/2/messagesPages').success({ code: 500 });
    assert.equal(c.count(), 1);
});

test('failed read acknowledgement preserves the badge', () => {
    const c = chat();
    c.receive(); c.server.failRead = true;
    c.app.talkTo('b', 'Guest');
    assert.equal(c.count(), 1);
    assert.equal(c.server.messages[0].status, 'unread');
    assert.equal(c.errors.length > 0, true);
});

test('the selected conversation controls read state while visitor details are loading', () => {
    const c = chat();
    c.app.talkTo('b', 'Guest'); c.receive(); c.receive('a');
    assert.equal(c.count(), 0);
    assert.equal(c.count('a'), 1);
});

test('a customer service reply does not count as an unread visitor message', () => {
    const c = chat(); c.receive('b', 'yes');
    assert.equal(c.count(), 0);
});

test('messages received while the iframe is hidden remain unread until displayed', () => {
    const c = chat();
    c.context.window.frameElement = { style: { display: 'none' } };
    c.receive('a');
    assert.equal(c.count('a'), 1);
    assert.equal(c.server.messages[0].status, 'unread');
    c.context.window.frameElement.style.display = '';
    c.app.readCurrentMessages();
    assert.equal(c.count('a'), 0);
});

test('messages received in a background browser tab remain unread', () => {
    const c = chat(); c.context.document.hidden = true; c.receive('a');
    assert.equal(c.count('a'), 1);
});

test('an old summary response cannot resurrect a cleared unread count', () => {
    const c = chat(); c.receive(); c.server.deferUnread = true;
    c.app.getUnreadMessages();
    const old = c.requests.at(-1);
    c.app.talkTo('b', 'Guest');
    const latest = c.requests.at(-1);
    latest.success(latest.reply); old.success(old.reply);
    assert.equal(c.count(), 0);
});

test('history for a previously selected visitor cannot clear or replace the current conversation', () => {
    const c = chat(); c.context.document.hidden = true; c.receive('a'); c.receive('b');
    c.context.document.hidden = false; c.server.deferHistory = true;
    c.app.talkTo('a', 'Guest A'); const a = c.requests.findLast(r => r.url === '/2/messagesPages');
    c.app.talkTo('b', 'Guest B'); const b = c.requests.findLast(r => r.url === '/2/messagesPages');
    b.success(b.reply); a.success(a.reply);
    assert.equal(c.count('a'), 1);
    assert.equal(c.count('b'), 0);
    assert.equal(c.app.msgList.every(m => m.visitor_id === 'b'), true);
});
