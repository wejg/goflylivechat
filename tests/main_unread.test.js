const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');
const html = fs.readFileSync(path.join(__dirname, '../static/templates/main.html'), 'utf8');
const script = html.match(/<script>\s*(new Vue[\s\S]*?)<\/script>/)[1];

function main() {
    let app, handler;
    const child = { postMessage() {} };
    const context = {
        Vue: function(options) { app = Object.assign({}, options.data, options.methods); return app; },
        window: { location: { origin: 'http://localhost' }, addEventListener: (_, fn) => { handler = fn; } },
        localStorage: { getItem: () => 'token', removeItem() {} },
        notify() {},
        $: Object.assign(() => ({ bind() {} }), { ajax: request => request.success({ code: 200, result: { avator: '' } }) }),
    };
    vm.runInNewContext(script, context);
    app.$refs = { chatFrame: { contentWindow: child } };
    app.$nextTick = fn => fn();
    app.focusWindow();
    return { app, child, send(count, overrides = {}) { handler({ origin: 'http://localhost', source: child, data: { type: 'unread_count', count }, ...overrides }); } };
}

test('navigation total comes from the authenticated chat and is not cleared by opening chat', () => {
    const c = main(); c.send(5);
    assert.equal(c.app.haveNewMessage, 5);
    c.app.openIframeUrl('/chat_main');
    assert.equal(c.app.haveNewMessage, 5);
    c.send(0);
    assert.equal(c.app.haveNewMessage, 0);
});

test('unread updates from unrelated windows, origins or invalid data are ignored', () => {
    const c = main(); c.send(2);
    c.send(99, { source: {} }); c.send(99, { origin: 'https://other.example' });
    for (const value of [-1, '99', null, 1.5]) c.send(value);
    assert.equal(c.app.haveNewMessage, 2);
});

test('authentication starts the hidden chat receiver on the home page and logout removes it', () => {
    const c = main(); c.app.checkAuth();
    assert.equal(c.app.iframeUrl, '/pannel');
    assert.equal(c.app.chatLoaded, true);
    c.app.logout();
    assert.equal(c.app.chatLoaded, false);
});
