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

test('profile save reloads the published avatar URL', () => {
    const c = settings();
    c.app.kefuInfo = { uid: 1, nickname: 'Agent', avator: '/attachments/avatar-id' };
    c.app.setUser();
    c.requests[0].success({ code: 200, result: '' });
    assert.equal(c.requests[1]?.url, '/kefuinfo');
    c.requests[1].success({ code: 200, result: { username: 'agent', avator: '/static/avatars/published.png' } });
    assert.equal(c.app.kefuInfo.avator, '/static/avatars/published.png');
});

test('avatar upload failure displays the server error and keeps the current avatar', () => {
    const c = settings();
    c.app.kefuInfo.avator = '/static/images/4.jpg';
    c.app.handleAvatarSuccess({ code: 400, msg: '上传失败!' });
    assert.equal(c.messages[0]?.message, '上传失败!');
    assert.equal(c.messages[0]?.type, 'error');
    assert.equal(c.app.kefuInfo.avator, '/static/images/4.jpg');
});

test('password change rejects missing, short and mismatched input without a request', () => {
    for (const fields of [
        ['', 'new-pass-123', 'new-pass-123'],
        ['old-pass', '', ''],
        ['old-pass', 'new-pass-123', ''],
        ['old-pass', 'short', 'short'],
        ['old-pass', 'new-pass-123', 'different-pass'],
    ]) {
        const c = settings();
        c.app.modifyPass = { old_pass: fields[0], new_pass: fields[1], confirm_new_pass: fields[2] };
        c.app.setModifyPass();
        assert.equal(c.requests.length, 0);
        assert.equal(c.messages[0]?.type, 'error');
        assert.equal(c.app.passwordSaving, false);
    }
});

test('password change sends credentials once and clears them only after success', () => {
    const c = settings();
    c.app.modifyPass = { old_pass: 'old-pass', new_pass: 'new-pass-123', confirm_new_pass: 'new-pass-123' };
    c.app.setModifyPass();
    c.app.setModifyPass();
    assert.equal(c.requests.length, 1);
    const request = c.requests[0];
    assert.equal(request.url, '/modifypass');
    assert.equal(request.type, 'POST');
    assert.equal(request.headers.token, 'token');
    assert.equal(request.data.old_pass, 'old-pass');
    assert.equal(request.data.new_pass, 'new-pass-123');
    assert.equal(request.data.confirm_new_pass, 'new-pass-123');
    assert.equal(c.app.passwordSaving, true);
    request.success({ code: 200, result: '' });
    request.complete();
    assert.deepEqual(Object.values(c.app.modifyPass), ['', '', '']);
    assert.equal(c.app.passwordSaving, false);
    assert.equal(c.messages[0]?.type, 'success');
});

test('password change preserves input and enables retry after server or network failure', () => {
    for (const networkFailure of [false, true]) {
        const c = settings();
        c.app.modifyPass = { old_pass: 'wrong-pass', new_pass: 'new-pass-123', confirm_new_pass: 'new-pass-123' };
        c.app.setModifyPass();
        const request = c.requests[0];
        if (networkFailure) request.error({});
        else request.success({ code: 400, msg: '旧密码不正确' });
        request.complete();
        assert.equal(c.app.modifyPass.old_pass, 'wrong-pass');
        assert.equal(c.app.modifyPass.new_pass, 'new-pass-123');
        assert.equal(c.app.passwordSaving, false);
        assert.equal(c.messages[0]?.type, 'error');
        c.app.setModifyPass();
        assert.equal(c.requests.length, 2);
    }
});

test('saving profile never sends a password', () => {
    const c = settings();
    c.app.kefuInfo = { uid: 1, nickname: 'Agent', avator: '/static/images/4.jpg', password: 'stale-value' };
    c.app.setUser();
    assert.equal(Object.hasOwn(c.requests[0].data, 'password'), false);
});
