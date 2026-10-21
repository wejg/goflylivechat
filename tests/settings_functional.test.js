const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const { test } = require('node:test');
const html = fs.readFileSync(path.join(__dirname, '../static/templates/setting_bottom.html'), 'utf8');
const script = html.match(/<script>\s*(var app=new Vue[\s\S]*)<\/script>/)[1];

function settings() {
    const requests = [], messages = [];
    const context = {
        Vue: function (options) { return Object.assign({}, options.data, options.methods); },
        getBaseUrl: () => 'https://support.example',
        localStorage: { getItem: () => 'token' },
        window: { location: { origin: 'https://support.example' } },
        $: { ajax: request => requests.push(request) },
    };
    vm.runInNewContext(script, context);
    const app = context.app;
    app.$message = value => messages.push(value);
    return { app, requests, messages };
}

test('generated chat link preserves a username with URL separators', () => {
    const c = settings(); c.app.getKefuInfo();
    c.requests[0].success({ code: 200, result: { username: 'qa&link+#=客服' } });
    assert.equal(new URL(c.app.chatEndpoint).searchParams.get('user_id'), 'qa&link+#=客服');
});

test('generated embed passes usernames containing quotes as data', () => {
    const c = settings(); c.app.getKefuInfo();
    const username = 'qa"</script>\\agent';
    c.requests[0].success({ code: 200, result: { username } });
    let initialized;
    const scriptElement = {};
    vm.runInNewContext(c.app.deployCode.match(/<script>([\s\S]*?)<\/script>/)[1], {
        window: {},
        document: { getElementsByTagName: () => [{ appendChild: () => scriptElement.onload() }], createElement: () => scriptElement },
        CHAT_WIDGET: { initialize: value => { initialized = value; } },
    });
    assert.equal(initialized.AGENT_ID, username);
});

test('configuration save failure is visible and never runs its success callback', () => {
    const c = settings(); c.app.setConfigItem('WelcomeMessage', 'value');
    c.requests[0].error?.({ responseJSON: { code: 500, msg: '保存失败' } });
    assert.equal(c.messages.length, 1);
    assert.equal(c.messages[0].type, 'error');
    assert.equal(c.messages[0].message, '保存失败');
    assert.equal(c.requests.length, 1);
});
