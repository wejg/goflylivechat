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
        return { counts, total: Object.values(counts).reduce((a, b) => a + b, 0), last_message_id: Math.max(0, ...server.messages.filter(m => m.mes_type === 'visitor' && m.status === 'unread').map(m => m.id)) };
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
        if (request.url === '/message_read_all') {
            if (!server.deferReadAll) finishReadAll(request);
        }
        if (request.url === '/2/messagesPages') {
            request.reply = { code: 200, result: { list: server.messages.filter(m => m.visitor_id === request.data.visitor_id).map(m => ({ ...m })).reverse() } };
            if (!server.deferHistory) request.success(request.reply);
        }
    }
    function finishReadAll(request) {
        if (server.failRead) {
            request.success({ code: 500, msg: 'test database write failed' });
            request.complete();
            return;
        }
        for (const message of server.messages) {
            if (message.mes_type === 'visitor' && message.id <= request.data.last_message_id) message.status = 'read';
        }
        request.success({ code: 200 });
        request.complete();
    }
    const context = vm.createContext({
        ChatWindow: require('../static/js/chat-window.js'),
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
        app, context, server, requests, notifications, errors, finishReadAll,
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

test('one click marks online and offline incoming messages read and updates the menu total', () => {
    const c = chat();
    c.receive(); c.receive('offline'); c.receive('b', 'yes');
    c.app.markAllMessagesRead();
    assert.equal(c.count(), 0);
    assert.equal(c.count('offline'), 0);
    assert.equal(c.server.messages[2].status, 'unread');
    assert.deepEqual(JSON.parse(JSON.stringify(c.notifications.at(-1))), { type: 'unread_count', count: 0 });
    assert.equal(c.app.markingAllRead, false);
    const restored = chat(c.server);
    restored.app.getUnreadMessages();
    assert.equal(restored.count(), 0);
});

test('one click keeps later messages unread and prevents duplicate submissions', () => {
    const c = chat();
    c.receive(); c.server.deferReadAll = true;
    c.app.markAllMessagesRead();
    const request = c.requests.findLast(request => request.url === '/message_read_all');
    c.receive(); c.app.markAllMessagesRead();
    assert.equal(c.requests.filter(request => request.url === '/message_read_all').length, 1);
    assert.equal(request.data.last_message_id, 1);
    c.finishReadAll(request);
    assert.equal(c.server.messages[0].status, 'read');
    assert.equal(c.server.messages[1].status, 'unread');
    assert.equal(c.count(), 1);
});

test('one click preserves unread counts after a server or network failure', () => {
    const c = chat();
    c.receive(); c.server.failRead = true;
    c.app.markAllMessagesRead();
    assert.equal(c.count(), 1);
    assert.equal(c.errors.at(-1).type, 'error');
    assert.equal(c.app.markingAllRead, false);
    c.server.deferReadAll = true;
    c.app.markAllMessagesRead();
    const request = c.requests.findLast(request => request.url === '/message_read_all');
    request.error(); request.complete();
    assert.equal(c.count(), 1);
    assert.equal(c.app.markingAllRead, false);
});

test('one click does nothing when there are no unread incoming messages', () => {
    const c = chat();
    c.app.markAllMessagesRead();
    assert.equal(c.requests.length, 0);
});

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

test('new messages remain unread while the selected conversation is scrolled into history', () => {
    const c=chat();
    c.app.listAtBottom=false;
    c.receive('a');
    assert.equal(c.count('a'),1);
    assert.equal(c.app.msgList.length,0);
    assert.equal(c.app.hasNewer,true);
    assert.equal(c.server.messages[0].status,'unread');
    c.app.showLatestMessages();
    assert.equal(c.count('a'),0);
});
