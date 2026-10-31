const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');

function chat(kind = 'visitor', options = {}) {
    const requests = [], errors = [], revoked = [];
    const html = fs.readFileSync(path.join(__dirname, `../static/templates/${kind === 'agent' ? 'chat_main' : 'chat_page'}.html`), 'utf8');
    const script = html.match(kind === 'agent' ? /<script>\s*(var app=new Vue[\s\S]*?)<\/script>/ : /<script>\s*(new Vue\([\s\S]*?)<\/script>/)[1];
    function jquery() { return { append() {}, scrollTop() {}, 0: { scrollHeight: 0 } }; }
    jquery.ajax = request => requests.push(request);
    jquery.post = (url, data, success) => requests.push({ url, data, success });
    jquery.get = (url, success) => requests.push({ url, success });
    const context = vm.createContext({
        ChatWindow: require('../static/js/chat-window.js'),
        Vue: function(config) { return Object.assign({}, config.data, config.methods); },
        getWsBaseUrl: () => 'ws://localhost',
        localStorage: { getItem: () => 'test-token' },
        replaceContent: value => value,
        formatDate: vm.runInNewContext(fs.readFileSync(path.join(__dirname, '../static/js/functions.js'), 'utf8') + '; formatDate', { document: {} }),
        getQuery: () => '',
        b64ToUtf8: value => Buffer.from(value, 'base64').toString('utf8'),
        window: { location: { origin: 'http://localhost' }, parent: { postMessage() {} }, matchMedia: () => ({matches:!options.touch}) },
        document: { hidden: false },
        KEFU_ID: options.kefu || 'qa-agent',
        APP_CHAT_ENABLED: !!options.app,
        ReconnectingWebSocket: class { close() {} },
        WebSocket: {OPEN:1}, setInterval: () => 1,
        URL: {createObjectURL:()=>'blob:test-preview',revokeObjectURL:url=>revoked.push(url)},
        FormData: class { constructor(){this.values=[];} append(...args){this.values.push(args);} },
        XMLHttpRequest: class { constructor(){this.upload={addEventListener(){}};} },
        INITIAL_VISITOR_ID: options.initialId || '',
        INITIAL_VISITOR_NAME: '', INITIAL_VISITOR_AVATAR: '', REFER: 'VM regression',
        clearInterval() {}, console: { log() {} }, notify() {}, flashTitle() {}, $: jquery,
    });
    const result = vm.runInContext(script, context);
    const app = kind === 'agent' ? context.app : result;
    const initConn=app.initConn;
    Object.assign(app, {
        $set: (object, key, value) => { object[key] = value; },
        $message: value => errors.push(value),
        $nextTick:callback=>callback(),
        $refs:{composer:{focus(){},style:{},scrollHeight:42}},
        scrollBottom() {}, sendSound() {}, alertSound() {}, initConn() {}, setCache() {},
        getCache: () => options.cache || null,
        socket: { send() {}, close() {} },
    });
    app.currentGuest = 'a';
    app.visitor = { visitor_id: 'a', to_id: 'qa-agent', token: 'visitor-token', name: 'Guest' };
    return {
        app, requests, errors, revoked,
        connect() { initConn.call(app); },
        history() { return requests.findLast(request => request.url === '/2/messagesPages'); },
        receive(id, content = `message ${id}`, time = '2026-10-21T12:00:00Z') {
            app.OnMessage({ data: JSON.stringify({ type: 'message', data: {
                message_id: id, id: kind === 'agent' ? 'a' : 'qa-agent',
                name: 'Agent', avator: '', content, is_kefu: 'yes', time,
            } }) });
        },
    };
}
function message(id, visitor = 'a') {
    return { id, visitor_id: visitor, content: `message ${id}`, mes_type: 'kefu', create_time: '2026-10-21T12:00:00Z', visitor_name: visitor, visitor_avator: '', kefu_name: 'Agent', kefu_avator: '' };
}
function finish(request, response) {
    request.success(response);
    if (request.complete) request.complete();
}
function history(request, ids) { finish(request, { code: 200, result: { list: ids.map(id => message(id)) } }); }
function fail(request) {
    if (request.error) request.error({ status: 0, responseText: '' });
    if (request.complete) request.complete();
}
function ids(app) { return Array.from(app.msgList, item => item.id); }

test('agent distinguishes identical visitor names using their App account IDs in history and realtime', () => {
    const c = chat('agent');
    for (const [visitorId, accountId, recordId] of [['a', '42', 1012], ['b', '43', 1013]]) {
        c.app.talkTo(visitorId, 'App visitor');
        c.app.OnMessage({ data: JSON.stringify({ type: 'message', data: {
            id: visitorId, message_id: 3274, name: 'App visitor', is_kefu: 'no', content: 'hello',
        } }) });
        const realtime = c.app.msgList[0];
        assert.equal(c.app.messageName(realtime), 'App visitor', 'must not show the previous visitor account while details load');
        finish(c.requests.findLast(request => request.url === '/visitor'), { code: 200, result: {
            visitor_id: visitorId, id: recordId, name: `visitor_${accountId}`, status: 1,
            extra: Buffer.from(JSON.stringify({ account_id: accountId, is_guest: 'true' })).toString('base64'),
        } });
        const expected = `visitor_${accountId}`;
        assert.equal(c.app.messageName(realtime), expected);
        finish(c.history(), { code: 200, result: { list: [{ ...message(3273, visitorId), mes_type: 'visitor', visitor_name: 'App visitor' }] } });
        assert.equal(c.app.messageName(c.app.msgList[0]), expected);
        assert.equal(c.app.messageName({ name: '客服', is_kefu: true }), '客服');
    }
});

test('agent keeps registered account names in history and realtime', () => {
    const c = chat('agent');
    c.app.getVistorInfo('a');
    finish(c.requests[0], { code: 200, result: {
        visitor_id: 'a', id: 1012, name: 'user_2088', status: 1,
        extra: Buffer.from(JSON.stringify({ account_id: '2088', is_guest: 'false' })).toString('base64'),
    } });
    c.app.OnMessage({ data: JSON.stringify({ type: 'message', data: {
        id: 'a', message_id: 3274, name: 'user_2088', is_kefu: 'no', content: 'hello',
    } }) });
    assert.equal(c.app.messageName(c.app.msgList[0]), 'user_2088');
    c.app.getHistoryMessage();
    finish(c.history(), { code: 200, result: { list: [{ ...message(3273), mes_type: 'visitor', visitor_name: 'user_2088' }] } });
    assert.equal(c.app.messageName(c.app.msgList[0]), 'user_2088');
});

test('history and realtime messages use the browser timezone, including the next day in China', () => {
    const originalTZ = process.env.TZ;
    try {
        for (const [timezone, expected] of [
            ['Asia/Shanghai', '2026-10-27 01:42:22'],
            ['UTC', '2026-10-26 17:42:22'],
            ['America/New_York', '2026-10-26 13:42:22'],
        ]) {
            process.env.TZ = timezone;
            for (const kind of ['agent', 'visitor']) {
                const c = chat(kind), time = '2026-10-26T17:42:22Z';
                c.receive(3271, 'timezone regression', time);
                const display = item => kind === 'agent' ? c.app.messageTime(item) : c.app.messageDate(item);
                const realtime = display(c.app.msgList[0]);
                if (kind === 'agent') assert.equal(realtime, expected);
                else {
                    assert.equal(realtime.getDate(), Number(expected.slice(8, 10)));
                    assert.equal(realtime.getHours(), Number(expected.slice(11, 13)));
                    assert.equal(realtime.getMinutes(), 42);
                }
                c.app.msgList = [];
                c.app.getHistoryMessage();
                finish(c.history(), { code: 200, result: { list: [{ ...message(3271), create_time: time }] } });
                assert.equal(String(display(c.app.msgList[0])), String(realtime));
                if (kind === 'visitor') {
                    assert.equal(c.app.messageDate({ time: '2026-10-27T01:42:22+08:00' }).getTime(), realtime.getTime());
                }
            }
        }
    } finally {
        if (originalTZ === undefined) delete process.env.TZ;
        else process.env.TZ = originalTZ;
    }
});

test('visitor pending messages have an explicit timezone and keep their label after history reload', () => {
    const c = chat();
    c.app.messageContent = 'time regression';
    c.app.chatToUser();
    const pending = c.app.msgList[0], label = c.app.messageTime(pending);
    assert.match(pending.time, /T\d{2}:\d{2}:\d{2}\.\d{3}Z$/);
    finish(c.requests[0], { code: 200, result: { message_id: 3274 } });
    c.app.msgList = [];
    c.app.getHistoryMessage();
    finish(c.history(), { code: 200, result: { list: [{ ...message(3274), create_time: pending.time }] } });
    assert.equal(c.app.messageTime(c.app.msgList[0]), label);
});

test('visitor presence follows online events, heartbeat offline and reconnect', () => {
    const {app}=chat();
    app.OnMessage({data:JSON.stringify({type:'kfOnline',data:{id:'qa-agent',name:'小飞'}})});
    assert.equal(app.showKfonline,true);
    app.OnMessage({data:JSON.stringify({type:'pong',data:{online:false}})});
    assert.equal(app.showKfonline,false);
    assert.match(app.chatTitle,/暂未在线/);
    app.OnMessage({data:JSON.stringify({type:'pong',data:{online:true}})});
    assert.equal(app.showKfonline,true);
    app.OnClose();
    assert.equal(app.showKfonline,false);
    assert.match(app.chatTitle,/重连/);
});

test('late notice metadata cannot override a newer online event', () => {
    const c=chat();c.app.getNotice();
    c.app.OnMessage({data:JSON.stringify({type:'kfOnline',data:{id:'qa-agent',name:'小飞'}})});
    finish(c.requests[0],{code:200,result:{nickname:'小飞',online:false}});
    assert.equal(c.app.showKfonline,true);
});

test('outside service hours an actually connected agent still appears online', () => {
    const c=chat();
    c.app.getNotice();
    finish(c.requests[0],{code:200,result:{nickname:'小飞',online:true,service_open:false}});
    assert.equal(c.app.showKfonline,true);
    assert.equal(c.app.chatTitle,'客服在线');
    assert.doesNotMatch(c.app.guidanceText(),/非服务时间|查看留言/);
    c.app.OnMessage({data:JSON.stringify({type:'kfOnline',data:{id:'qa-agent',name:'小飞',service_open:false}})});
    assert.equal(c.app.showKfonline,true);
    c.app.messageContent='下班留言';
    c.app.chatToUser();
    assert.ok(c.requests.some(request => request.url==='/2/message'));
});

test('heartbeats switch service hours without reopening the page', () => {
    const {app}=chat();
    app.OnMessage({data:JSON.stringify({type:'pong',data:{online:true,service_open:true}})});
    assert.equal(app.showKfonline,true);
    app.OnMessage({data:JSON.stringify({type:'pong',data:{online:true,service_open:false}})});
    assert.equal(app.showKfonline,true);
    assert.equal(app.chatTitle,'客服在线');
    app.OnMessage({data:JSON.stringify({type:'pong',data:{online:false,service_open:false}})});
    assert.equal(app.showKfonline,false);
    assert.match(app.chatTitle,/当前休息/);
    app.OnMessage({data:JSON.stringify({type:'pong',data:{online:true,service_open:true}})});
    assert.equal(app.showKfonline,true);
    assert.match(app.chatTitle,/在线/);
    app.OnMessage({data:JSON.stringify({type:'pong',data:{online:false,service_open:true}})});
    assert.match(app.chatTitle,/暂未在线/);
});

test('initial metadata reports offline without pretending that an open socket means agent online', () => {
    const c=chat();c.app.getNotice();
    finish(c.requests[0],{code:200,result:{nickname:'小飞',online:false}});
    assert.equal(c.app.showKfonline,false);
    assert.match(c.app.chatTitle,/暂未在线/);
});

test('app session loads history before the socket opens and does not duplicate an in-flight first load', () => {
    const c=chat('visitor',{app:true});
    c.app.getAppSession();
    finish(c.requests[0],{code:200,result:{visitor_id:'a',to_id:'qa-agent'}});
    const initial=c.history();
    assert.ok(initial, 'history must not wait for WebSocket');
    c.app.OnOpen();
    assert.equal(c.history(),initial);
    history(initial,[3,2,1]);
    assert.equal(c.app.historyReady,true);
    assert.deepEqual(ids(c.app),[1,2,3]);
    c.app.OnClose();c.app.OnOpen();
    assert.notEqual(c.history(),initial);
});

test('session network failure is retryable without misreporting expired credentials', () => {
    const c=chat('visitor',{app:true});c.app.getAppSession();
    fail(c.requests[0]);
    assert.equal(c.app.loadFailed,true);
    assert.equal(c.app.sessionFailed,false);
});

test('first history failure exposes a retryable load failure rather than an empty ready chat', () => {
    const c=chat();c.app.getHistoryMessage();
    fail(c.history());
    assert.equal(c.app.historyReady,false);
    assert.equal(c.app.loadFailed,true);
    c.app.getHistoryMessage();history(c.history(),[]);
    assert.equal(c.app.historyReady,true);
    assert.equal(c.app.loadFailed,false);
});

test('first offline heartbeat clears the initial connecting title', () => {
    const c=chat();
    c.app.OnMessage({data:JSON.stringify({type:'pong',data:{online:false}})});
    assert.match(c.app.chatTitle,/暂未在线/);
});

test('a timed-out socket shows reconnecting while keeping already loaded messages', () => {
    const c=chat('visitor',{app:true});c.app.getHistoryMessage();history(c.history(),[1]);
    c.connect();
    c.app.socket.onconnecting({code:1006});
    assert.match(c.app.chatTitle,/重连/);
    assert.deepEqual(ids(c.app),[1]);
    assert.equal(c.app.historyReady,true);
});

test('an ended visitor session keeps its ended status when the socket closes', () => {
    const {app}=chat();app.showKfonline=true;
    app.OnMessage({data:JSON.stringify({type:'close'})});
    app.OnClose();
    assert.equal(app.showKfonline,false);
    assert.match(app.chatTitle,/会话已结束/);
});

test('visitor time labels group nearby messages and keep day boundaries', () => {
    const {app}=chat();
    app.msgList=[
        {time:'2026-10-21 23:59:00'}, {time:'2026-10-22 00:01:00'},
        {time:'2026-10-22 00:02:00'}, {time:'2026-10-22 00:08:00'},
        {time:'invalid'},
    ];
    assert.ok(app.messageTime(app.msgList[0]));
    assert.ok(app.messageTime(app.msgList[1]));
    assert.equal(app.messageTime(app.msgList[2]),'');
    assert.ok(app.messageTime(app.msgList[3]));
    assert.equal(app.messageTime(app.msgList[4]),'');
});

test('visitor sends preserve intentional line breaks', () => {
    const c=chat();c.app.messageContent='first\nsecond';c.app.chatToUser();
    assert.equal(c.requests[0].data.content,'first\nsecond');
});

test('composer does not send during Chinese composition or Shift+Enter', () => {
    const {app}=chat();let sends=0;
    app.chatToUser=()=>sends++;
    app.onComposerKeydown({key:'Enter',isComposing:true});
    app.onComposerKeydown({key:'Enter',keyCode:229});
    app.onComposerKeydown({key:'Enter',shiftKey:true});
    assert.equal(sends,0);
});

test('Enter sends on a desktop keyboard and keeps the mobile return key for new lines', () => {
    for(const touch of [false,true]){
        const {app}=chat('visitor',{touch});let sends=0,prevented=false;
        app.chatToUser=()=>sends++;
        app.onComposerKeydown({key:'Enter',preventDefault(){prevented=true;}});
        assert.equal(sends,touch?0:1);
        assert.equal(prevented,!touch);
    }
});

test('emoji selection keeps the draft within the input length limit', () => {
    const {app}=chat();app.face=[{emoji:'😊'}];
    app.messageContent='a'.repeat(999);app.faceIconClick(0);
    assert.equal(app.messageContent.length,999);
    app.messageContent='a'.repeat(998);app.faceIconClick(0);
    assert.equal(app.messageContent.length,1000);
    assert.ok(app.messageContent.endsWith('😊'));
});

for (const [name, initialId, expected] of [['explicit identity overrides cache', 'new-account', 'new-account'], ['cache restores an unspecified identity', '', 'previous-account']]) {
    test(name, () => {
        const c = chat('visitor', { initialId, cache: { visitor_id: 'previous-account' } });
        c.app.getUserInfo();
        assert.equal(c.requests[0].data.visitor_id, expected);
    });
}

test('socket open loads history once and reconnect exposes all missed pages without losing concurrent replies', () => {
    const c = chat();
    c.app.getUserInfo();
    finish(c.requests[0], { code: 200, result: { visitor_id: 'a', to_id: 'qa-agent', token: 'visitor-token' } });
    assert.equal(c.requests.filter(request => request.url === '/2/messagesPages').length, 0);
    c.app.OnOpen();
    assert.equal(c.history().headers.token, 'visitor-token');
    history(c.history(), [5, 4, 3, 2, 1]);
    c.app.OnClose();
    c.app.OnOpen();
    assert.equal(c.history().data.page, 1);
    c.receive(14);
    history(c.history(), [14, 13, 12, 11, 10]);
    assert.deepEqual(ids(c.app), [10, 11, 12, 13, 14]);
    c.app.getHistoryMessage();
    assert.equal(c.history().data.page, 2);
    history(c.history(), [9, 8, 7, 6, 5]);
    c.app.getHistoryMessage();
    history(c.history(), [4, 3, 2, 1]);
    c.receive(14);
    assert.deepEqual(ids(c.app), [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14]);
    assert.equal(c.app.showLoadMore, false);
});

test('a previous connection history response cannot replace a newer synchronization', () => {
    const c = chat();
    c.app.OnOpen(); const old = c.history();
    c.app.OnClose(); c.app.OnOpen(); const latest = c.history();
    assert.notEqual(old, latest);
    history(latest, [10]); history(old, [1]);
    assert.deepEqual(ids(c.app), [10]);
    assert.equal(c.app.messages.page, 2);
});

test('late visitor details cannot change the selected conversation', () => {
    const c = chat('agent');
    c.app.talkTo('a', 'A');
    c.app.talkTo('b', 'B');
    const details = c.requests.filter(request => request.url === '/visitor');
    finish(details[1], { code: 200, result: { visitor_id: 'b', name: 'B', id: 2, extra: '', status: 1 } });
    finish(details[0], { code: 200, result: { visitor_id: 'a', name: 'A', id: 1, extra: '', status: 1 } });
    assert.equal(c.app.visitor.visitor_id, 'b');
    assert.equal(c.app.currentGuest, 'b');
    assert.equal(c.app.chatTitle, '#2|B');
});

for (const kind of ['agent', 'visitor']) {
    const sendUrl = kind === 'agent' ? '/kefu/message' : '/2/message';
    test(`${kind} blocks repeat submission until a successful send finishes`, () => {
        const c = chat(kind);
        c.app.messageContent = 'hello'; c.app.chatToUser();
        assert.equal(c.app.sendDisabled, true);
        c.app.chatToUser();
        assert.equal(c.requests.filter(request => request.url === sendUrl).length, 1);
        finish(c.requests[0], { code: 200, result: { message_id: 20 } });
        assert.equal(c.app.sendDisabled, false);
        assert.equal(c.app.messageContent, '');
        if (kind === 'visitor') assert.deepEqual(ids(c.app), [20]);
    });
    test(`${kind} unlocks and keeps its draft after a rejected send`, () => {
        const c = chat(kind);
        c.app.messageContent = 'retry me'; c.app.chatToUser();
        finish(c.requests[0], { code: 400, msg: 'slow down' });
        assert.equal(c.app.sendDisabled, false);
        assert.equal(c.app.messageContent, 'retry me');
        assert.equal(c.errors.length, 1);
        if (kind === 'visitor') assert.equal(c.app.msgList[0].failed, true);
    });
    test(`${kind} unlocks and keeps its draft after a network failure`, () => {
        const c = chat(kind);
        c.app.messageContent = 'retry me'; c.app.chatToUser();
        fail(c.requests[0]);
        assert.equal(c.app.sendDisabled, false);
        assert.equal(c.app.messageContent, 'retry me');
        assert.equal(c.errors.length, 1);
        c.app.chatToUser();
        assert.equal(c.requests.filter(request => request.url === sendUrl).length, 2);
        if (kind === 'visitor') assert.equal(c.app.msgList.length, 1);
    });
    test(`${kind} keeps the old reading window bounded when another reply arrives`, () => {
        const c = chat(kind);
        c.app.msgList = Array.from({ length: 200 }, (_, i) => message(i + 26));
        c.app.messages.page = 15;
        c.app.getHistoryMessage();
        history(c.history(), kind === 'agent' ? [25, 24, 23, 22, 21, 20, 19, 18, 17, 16, 15, 14, 13, 12, 11] : [25, 24, 23, 22, 21]);
        assert.equal(c.app.msgList[0].id, kind === 'agent' ? 11 : 21);
        assert.equal(c.app.showLoadMore, true);
        c.receive(226);
        assert.equal(c.app.msgList.length, 200);
        assert.equal(c.app.hasNewer,true);
        assert.equal(ids(c.app).includes(226),false);
        assert.equal(c.app.msgList[0].id, kind === 'agent' ? 11 : 21);
    });
}

test('a failed visitor send retains its retryable bubble and an incoming reply', () => {
    const c = chat();
    c.app.messageContent = 'rejected'; c.app.chatToUser();
    c.receive(30, 'agent reply');
    finish(c.requests[0], { code: 400, msg: 'slow down' });
    assert.deepEqual(Array.from(c.app.msgList, item => item.content), ['agent reply','rejected']);
    assert.equal(c.app.msgList[1].failed,true);
});

test('a reconnect retains an in-flight visitor send and merges its persisted history copy', () => {
    const c = chat();
    c.app.messageContent = 'pending'; c.app.chatToUser();
    const send = c.requests[0];
    c.app.OnOpen();
    history(c.history(), [20]);
    finish(send, { code: 200, result: { message_id: 20 } });
    assert.deepEqual(ids(c.app), [20]);
    assert.equal(c.app.msgList[0].content, 'pending');
});

test('notice lookup preserves a customer service name containing query delimiters', () => {
    const c = chat('visitor', { kefu: 'qa& link+?中/#' });
    c.app.getNotice();
    const url = new URL(c.requests[0].url, 'http://localhost');
    assert.equal(url.searchParams.get('kefu_id'), 'qa& link+?中/#');
    assert.equal(Array.from(url.searchParams).length, 1);
    assert.equal(url.hash, '');
});

for (const kind of ['agent', 'visitor']) {
    test(`${kind} sorts a history reply that contains messages newer than concurrent websocket replies`, () => {
        const c = chat(kind);
        c.app.getHistoryMessage();
        const request = c.history();
        c.receive(20); c.receive(21);
        history(request, [22, 21, 20, 19, 18]);
        assert.deepEqual(ids(c.app), [18, 19, 20, 21, 22]);
    });
    test(`${kind} prevents duplicate pagination requests and advances only the requested page`, () => {
        const c = chat(kind);
        c.app.messages.page = 2;
        c.app.getHistoryMessage();
        c.app.getHistoryMessage();
        assert.equal(c.requests.filter(request => request.url === '/2/messagesPages').length, 1);
        history(c.history(), [15, 14, 13, 12, 11]);
        assert.equal(c.app.messages.page, 3);
        c.app.getHistoryMessage();
        assert.equal(c.history().data.page, 3);
    });
    test(`${kind} releases the history lock on business and network failures without skipping a page`, () => {
        const c = chat(kind);
        c.app.messages.page = 2;
        c.app.getHistoryMessage();
        assert.equal(c.app.historyLoading, true);
        finish(c.history(), { code: 500 });
        assert.equal(c.app.historyLoading, false);
        assert.equal(c.app.messages.page, 2);
        c.app.getHistoryMessage();
        fail(c.history());
        assert.equal(c.app.historyLoading, false);
        assert.equal(c.app.messages.page, 2);
        c.app.getHistoryMessage();
        history(c.history(), [15, 14, 13, 12, 11]);
        assert.equal(c.app.messages.page, 3);
    });
}

test('a visitor send acknowledged after newer history is merged takes its persisted chronological position', () => {
    const c = chat();
    c.app.messageContent = 'pending'; c.app.chatToUser();
    const send = c.requests[0];
    c.app.OnOpen();
    history(c.history(), [21, 20, 19, 18, 17]);
    finish(send, { code: 200, result: { message_id: 20 } });
    assert.deepEqual(ids(c.app), [17, 18, 19, 20, 21]);
    assert.equal(c.app.msgList[3].content, 'pending');
});

test('a visitor send acknowledged while history is loading survives an older history snapshot', () => {
    const c = chat();
    c.app.messageContent = 'pending'; c.app.chatToUser();
    const send = c.requests[0];
    c.app.OnOpen(); const request = c.history();
    finish(send, { code: 200, result: { message_id: 20 } });
    history(request, [19, 18, 17, 16, 15]);
    assert.deepEqual(ids(c.app), [15, 16, 17, 18, 19, 20]);
});

for (const failure of ['network', 'business']) {
    test(`a ${failure} failure while reconnecting retains visible history and allows a first-page retry`, () => {
        const c = chat();
        c.app.msgList = [message(1), message(2), message(3)];
        c.app.messages.page = 2;
        c.app.OnOpen();
        assert.deepEqual(ids(c.app), [1, 2, 3]);
        if (failure === 'network') fail(c.history());
        else finish(c.history(), { code: 500 });
        assert.deepEqual(ids(c.app), [1, 2, 3]);
        assert.equal(c.app.showLoadMore, true);
        c.app.getHistoryMessage();
        assert.equal(c.history().data.page, 1);
        history(c.history(), [12, 11, 10, 9, 8]);
        assert.deepEqual(ids(c.app), [8, 9, 10, 11, 12]);
    });
}

test('an initial visitor history failure exposes a retry entry', () => {
    const c = chat();
    c.app.OnOpen();
    fail(c.history());
    assert.equal(c.app.showLoadMore, true);
    c.app.getHistoryMessage();
    history(c.history(), [1]);
    assert.deepEqual(ids(c.app), [1]);
});

test('switching A to B to A rejects an old A history response and keeps the current pagination lock', () => {
    const c = chat('agent');
    c.app.talkTo('a', 'A'); const oldA = c.history();
    c.app.talkTo('b', 'B'); const oldB = c.history();
    c.app.talkTo('a', 'A'); const newA = c.history();
    history(newA, [20]);
    c.app.getHistoryMessage(); const page2 = c.history();
    history(oldA, [1]); history(oldB, [2]);
    assert.deepEqual(ids(c.app), [20]);
    assert.equal(c.app.messages.page, 2);
    assert.equal(c.app.historyLoading, true);
    history(page2, [19]);
    assert.deepEqual(ids(c.app), [19, 20]);
    assert.equal(c.app.messages.page, 3);
});

test('welcome guidance stays outside message history and never plays a received-message sound', () => {
    const c = chat();
    let sounds=0;c.app.alertSound=()=>sounds++;
    c.app.getNotice();
    finish(c.requests[0], { code: 200, result: { nickname: 'Agent', avatar: '', welcome: 'Welcome' } });
    c.receive(21);
    assert.deepEqual(Array.from(c.app.msgList, item => item.content), ['message 21']);
    c.receive(20);
    assert.deepEqual(Array.from(c.app.msgList, item => item.content), ['message 20', 'message 21']);
    assert.equal(sounds,2);
    c.app.messageContent = 'pending'; c.app.chatToUser();
    c.receive(22);
    assert.deepEqual(Array.from(c.app.msgList, item => item.content), ['message 20', 'message 21', 'message 22', 'pending']);
});

test('visitor retry keeps its original key and bubble without clearing a newer draft', () => {
    const c=chat();c.app.messageContent='original';c.app.chatToUser();
    const first=c.requests[0],bubble=c.app.msgList[0];fail(first);
    c.app.messageContent='new draft';c.app.retryMessage(bubble);
    const retry=c.requests.at(-1);
    assert.equal(retry.data.client_message_id,first.data.client_message_id);
    assert.equal(retry.data.content,'original');
    assert.equal(c.app.msgList.length,1);
    finish(retry,{code:200,result:{message_id:42}});
    assert.equal(c.app.messageContent,'new draft');
    assert.equal(bubble.failed,false);assert.equal(bubble.sending,false);
});

test('retrying an unchanged failed draft cannot create a second message', () => {
    const c=chat();c.app.messageContent='original';c.app.chatToUser();fail(c.requests[0]);
    c.app.chatToUser();
    assert.equal(c.requests[0].data.client_message_id,c.requests[1].data.client_message_id);
    assert.equal(c.app.msgList.length,1);
});

test('failed bubbles survive first-page history refresh', () => {
    const c=chat();c.app.messageContent='original';c.app.chatToUser();fail(c.requests[0]);
    const bubble=c.app.msgList[0];c.app.OnOpen();history(c.history(),[20,19]);
    assert.ok(c.app.msgList.includes(bubble));assert.equal(bubble.failed,true);
});

test('question shortcuts preserve existing draft and never send automatically', () => {
    const c=chat();c.app.messageContent='my draft';
    c.app.selectTopic('连接问题');
    assert.equal(c.app.messageContent,'my draft');
    assert.match(c.app.topicHint,/设备.*时间.*截图/);
    assert.equal(c.requests.length,0);
});

test('visitor composer accepts a long problem description but rejects more than 1000 units', () => {
    const c=chat();c.app.messageContent='问'.repeat(1000);c.app.chatToUser();
    assert.equal(c.requests[0].data.content.length,1000);
    const tooLong=chat();tooLong.app.messageContent='问'.repeat(1001);tooLong.app.chatToUser();
    assert.equal(tooLong.requests.length,0);assert.equal(tooLong.errors.length,1);
});

test('selecting or pasting an image only previews it and preserves the text draft', () => {
    const c=chat();c.app.messageContent='unfinished draft';
    const file={name:'problem.png',type:'image/png',size:123};
    c.app.selectAttachment(file,true);
    assert.equal(c.app.attachment.preview,'blob:test-preview');
    assert.equal(c.app.messageContent,'unfinished draft');assert.equal(c.requests.length,0);
    c.app.clearAttachment();let prevented=false;
    c.app.onPasteUpload({clipboardData:{items:[{type:'image/png',getAsFile:()=>file}]},preventDefault(){prevented=true;}});
    assert.equal(c.app.attachment.file,file);assert.equal(prevented,true);assert.equal(c.requests.length,0);
});

test('attachment confirmation uploads separately and message failure retries without reuploading', () => {
    const c=chat();c.app.messageContent='unfinished draft';
    c.app.selectAttachment({name:'problem.png',type:'image/png',size:123},true);
    c.app.confirmAttachment();const upload=c.requests[0];
    assert.equal(upload.url,'/uploadimg');assert.equal(c.app.attachment.uploading,true);
    finish(upload,{code:200,result:{path:'uploads/test.png'}});
    const send=c.requests[1];assert.equal(send.data.content,'img[/uploads/test.png]');
    fail(send);const bubble=c.app.msgList[0];c.app.retryMessage(bubble);
    assert.equal(c.requests.at(-1).data.client_message_id,send.data.client_message_id);
    assert.equal(c.requests.filter(request=>request.url==='/uploadimg').length,1);
    finish(c.requests.at(-1),{code:200,result:{message_id:70}});
    assert.equal(c.app.messageContent,'unfinished draft');
});

test('an outgoing image keeps its local preview and dimensions through sending and retry', () => {
    const c=chat();c.app.selectAttachment({name:'problem.png',type:'image/png',size:123},true);
    c.app.attachmentPreviewLoaded({target:{src:'blob:test-preview',naturalWidth:600,naturalHeight:900}});
    c.app.confirmAttachment();finish(c.requests[0],{code:200,result:{path:'uploads/test.png'}});
    const bubble=c.app.msgList[0];
    assert.equal(bubble.preview,'blob:test-preview');
    assert.equal(bubble.imageWidth,180);assert.equal(bubble.imageHeight,270);
    assert.equal(c.app.attachment,null);assert.deepEqual(c.revoked,[]);
    fail(c.requests[1]);c.app.retryMessage(bubble);
    assert.equal(bubble.preview,'blob:test-preview');assert.deepEqual(c.revoked,[]);
    finish(c.requests.at(-1),{code:200,result:{message_id:72}});
    assert.equal(bubble.preview,'blob:test-preview');
    c.app.releaseMessagePreviews();assert.deepEqual(c.revoked,[]);
    c.app.imageViewer=bubble.preview;
    c.app.msgList=[];c.app.releaseMessagePreviews();assert.deepEqual(c.revoked,[]);
    c.app.imageViewer='';c.app.releaseMessagePreviews();
    assert.deepEqual(c.revoked,['blob:test-preview']);
});

test('upload failure keeps its preview and exposes a retryable error', () => {
    const c=chat();c.app.selectAttachment({name:'problem.png',type:'image/png',size:123},true);
    c.app.confirmAttachment();fail(c.requests[0]);
    assert.equal(c.app.attachment.uploading,false);
    assert.match(c.app.attachment.error,/上传失败/);
    assert.equal(c.app.attachment.preview,'blob:test-preview');
    c.app.confirmAttachment();assert.equal(c.requests.length,2);
});

test('a connection ending during upload preserves the file and reuses its completed upload', () => {
    const c=chat();c.app.selectAttachment({name:'problem.png',type:'image/png',size:123},true);
    c.app.confirmAttachment();c.app.socketClosed=true;
    finish(c.requests[0],{code:200,result:{path:'uploads/test.png'}});
    assert.equal(c.app.attachment.file.name,'problem.png');
    assert.equal(c.requests.length,1);
    c.app.socketClosed=false;c.app.confirmAttachment();
    assert.equal(c.requests.at(-1).url,'/2/message');
    assert.equal(c.requests.filter(request=>request.url==='/uploadimg').length,1);
});

test('failed sends cannot keep a saved confirmation from an earlier message', () => {
    const c=chat();c.app.messageContent='saved';c.app.chatToUser();
    finish(c.requests[0],{code:200,result:{message_id:30}});assert.equal(c.app.messageSaved,true);
    c.app.messageContent='not saved';c.app.chatToUser();fail(c.requests[1]);
    assert.equal(c.app.messageSaved,false);
});

test('file attachments require confirmation and keep drafts separate from file content', () => {
    const c=chat();c.app.messageContent='draft';
    c.app.selectAttachment({name:'report.pdf',type:'application/pdf',size:5000},false);
    assert.equal(c.requests.length,0);assert.equal(c.app.attachment.preview,'');
    c.app.confirmAttachment();assert.equal(c.requests[0].url,'/uploadfile');
    finish(c.requests[0],{code:200,result:{name:'report.pdf',ext:'.pdf',size:5000,path:'uploads/report.pdf'}});
    const payload=c.requests[1].data.content;
    assert.match(payload,/^attachment\[/);assert.equal(payload.includes('draft'),false);
    assert.equal(c.app.messageContent,'draft');
});

test('unsupported or oversized attachments never upload', () => {
    const c=chat();
    c.app.selectAttachment({name:'script.html',type:'text/html',size:10},false);
    c.app.selectAttachment({name:'large.png',type:'image/png',size:11*1024*1024},true);
    assert.equal(c.app.attachment,null);assert.equal(c.requests.length,0);assert.equal(c.errors.length,2);
});

test('a full cache of unsent messages preserves the oldest failure and the new draft', () => {
    const c=chat();c.app.msgList=Array.from({length:200},(_,i)=>({...c.app.outgoingMessage('failed '+i,false),failed:true}));
    const oldest=c.app.msgList[0];c.app.messageContent='new draft';c.app.chatToUser();
    assert.equal(c.requests.length,0);assert.equal(c.app.msgList[0],oldest);
    assert.equal(c.app.messageContent,'new draft');
});

test('automatic reply metadata survives both realtime and history rendering', () => {
    const c=chat();c.app.OnMessage({data:JSON.stringify({type:'message',data:{message_id:50,id:'qa-agent',content:'notice',auto_reply_type:'offline'}})});
    assert.equal(c.app.msgList[0].auto_reply_type,'offline');
    c.app.getHistoryMessage();finish(c.history(),{code:200,result:{list:[{...message(50),auto_reply_type:'offline'}]}});
    assert.equal(c.app.msgList[0].auto_reply_type,'offline');
});

for (const kind of ['agent', 'visitor']) {
    test(`${kind} retains a synthetic notice between the persisted message positions`, () => {
        const c = chat(kind);
        c.app.msgList = [message(20), { content: 'Notice' }];
        c.receive(19);
        assert.deepEqual(Array.from(c.app.msgList, item => item.content), ['message 19', 'Notice', 'message 20']);
    });
}

for (const kind of ['agent', 'visitor']) {
    test(`${kind} loads immediately before its oldest retained message after realtime traffic reaches the cap`, () => {
        const c = chat(kind);
        c.app.getHistoryMessage();
        const initial = kind === 'agent' ? [20, 19, 18, 17, 16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6] : [20, 19, 18, 17, 16];
        history(c.history(), initial);
        for (let id = 21; id <= 220; id++) c.receive(id);
        assert.equal(c.app.msgList.length, 200);
        assert.equal(c.app.msgList[0].id, 21);
        c.app.getHistoryMessage();
        const older = c.history();
        assert.equal(older.data.before_id, 21);
        history(older, initial);
        assert.equal(c.app.msgList[0].id, kind === 'agent' ? 6 : 16);
        assert.equal(c.app.msgList.length, 200);
        assert.equal(c.app.hasNewer,true);
        c.receive(221);
        c.app.getHistoryMessage();
        assert.equal(c.history().data.before_id, kind === 'agent' ? 6 : 16);
    });
}

for (const kind of ['agent','visitor']) {
    test(`${kind} bounds the history cache and can reload evicted newer records`, () => {
        const c=chat(kind);c.app.messages.page=2;
        c.app.msgList=Array.from({length:200},(_,i)=>message(1001+i));
        c.app.getHistoryMessage();history(c.history(),Array.from({length:20},(_,i)=>1000-i));
        assert.equal(c.app.msgList.length,200);
        assert.equal(ids(c.app)[0],981);assert.equal(ids(c.app).at(-1),1180);
        assert.equal(c.app.hasNewer,true);
        c.app.getNewerMessages();const newer=c.history();
        assert.equal(newer.data.after_id,1180);
        history(newer,Array.from({length:20},(_,i)=>1181+i));
        assert.equal(c.app.msgList.length,200);assert.equal(ids(c.app)[0],1001);
        assert.equal(ids(c.app).at(-1),1200);
        c.app.getNewerMessages();history(c.history(),[]);
        assert.equal(c.app.hasNewer,false);
    });
    test(`${kind} new messages do not accumulate or move the reader away from history`, () => {
        const c=chat(kind);c.app.msgList=[message(10)];c.app.hasNewer=true;c.app.listAtBottom=false;
        let scrolls=0;c.app.scrollBottom=()=>{scrolls++;};
        for(let i=11;i<1000;i++)c.receive(i);
        assert.deepEqual(ids(c.app),[10]);assert.equal(scrolls,0);assert.equal(c.app.hasNewer,true);
    });
}

test('a send acknowledgement cannot advance the reload cursor past a deferred incoming reply', () => {
    const c=chat();c.app.msgList=[message(10)];c.app.hasNewer=true;c.app.listAtBottom=false;
    c.app.messageContent='my reply';c.app.chatToUser();
    const latest=c.history();const send=c.requests.find(r=>r.url==='/2/message');
    c.receive(501);
    finish(send,{code:200,result:{message_id:502}});
    history(latest,[500,499,498,497,496]);
    c.app.getNewerMessages();assert.equal(c.history().data.after_id,500);
    history(c.history(),[501,502]);
    assert.deepEqual(ids(c.app),[496,497,498,499,500,501,502]);
    assert.equal(c.app.hasNewer,false);
});
