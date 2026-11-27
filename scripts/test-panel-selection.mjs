import assert from 'node:assert/strict';
import { JSDOM } from 'jsdom';
const dom = new JSDOM('<main></main><dialog id="dialog"></dialog><div id="notice"></div>', { url: 'http://localhost/', pretendToBeVisual: true });
for (const key of ['window', 'document', 'Node', 'Event', 'FormData']) globalThis[key] = dom.window[key];
dom.window.HTMLDialogElement.prototype.showModal = function () { this.open = true; };
dom.window.HTMLDialogElement.prototype.close = function () { this.open = false; this.dispatchEvent(new Event('close')); };
const { formDialog } = await import('../internal/manage/panel/form-dialog.js');
const { outboundFields } = await import('../internal/manage/panel/outbound-fields.js');
const { accountOutputDialog } = await import('../internal/manage/panel/account-outputs.js');
const { el } = await import('../internal/manage/panel/dom.js');
assert.equal(el('button', { disabled: false }).disabled, false);
assert.equal(el('button', { disabled: true }).disabled, true);
assert.equal(el('div', { 'aria-hidden': false }).getAttribute('aria-hidden'), 'false', 'ARIA false must remain an explicit string');
const tick = () => new Promise((resolve) => setTimeout(resolve, 0));
const check = (value) => document.querySelector(`.choice-list input[value="${value}"]`);
const submit = (form) => form.dispatchEvent(new Event('submit', { cancelable: true }));
let saved;
let editor = formDialog('group', outboundFields(['selector', 'direct'], ['a', 'b', 'c', 'group'], [], {}),
  { type: 'selector', tag: 'group', outbounds: ['b', 'a'], default: '' }, (value) => { saved = value; }, true);
editor.input('outbounds').click();
check('c').focus(); check('c').click();
assert.equal(document.activeElement, check('c'), 'toggling must retain focus');
document.querySelector('[aria-label="上移 a"]').click();
assert.equal(document.activeElement.getAttribute('aria-label'), '下移 a', 'moving to the first position must focus an enabled action');
document.querySelector('[aria-label="下移 a"]').click();
submit(editor.form); await tick();
assert.deepEqual(saved.outbounds, ['b', 'a', 'c'], 'adding a member must retain the default member');

editor = formDialog('batch', [{ key: 'ids', label: '权益', type: 'multiselect', required: true, maxSelected: 3,
  options: Array.from({ length: 1000 }, (_, i) => [`id-${i}`, `权益 ${i}`]) }], { ids: [] }, (value) => { saved = value; });
editor.input('ids').click();
assert.equal(document.querySelectorAll('.choice-list input').length, 40, 'candidate rendering must be bounded');
document.querySelectorAll('.choice-pager button')[1].click();
assert.equal(document.activeElement, check('id-40'), 'paging must focus an option on the new page');
document.querySelectorAll('.choice-pager button')[0].click();
check('id-1').click();
const search = document.querySelector('.choice-search');
search.value = '权益 99'; search.dispatchEvent(new Event('input'));
check('id-99').click();
assert.deepEqual(editor.value().ids, ['id-1', 'id-99']);
document.querySelector('.choice-tools button').click();
assert.equal(editor.value().ids.length, 3);
assert.match(document.querySelector('.choice-feedback').textContent, /上限/);
search.value = ''; search.dispatchEvent(new Event('input'));
assert.equal(check('id-1').checked, true);
document.querySelector('.choice-tools button:nth-child(2)').click();
saved = undefined; submit(editor.form); await tick();
assert.equal(saved, undefined, 'empty required selection must not save');
assert.equal(editor.input('ids').getAttribute('aria-invalid'), 'true');
assert.equal(document.activeElement, search, 'validation must focus the visible search instead of the disabled trigger');

const profiles = [
  { profile: 'uri', label: 'URI', platforms: ['通用导入'] },
  { profile: 'stash-config', label: 'Stash', platforms: ['iOS'] },
  { profile: 'mihomo-provider', label: 'Mihomo', platforms: ['Windows'] },
].map((p) => ({ ...p, available: true, nodes: 1, input_nodes: 1 }));
await accountOutputDialog({ accounts: [{ id: 'a', name: 'A', download_scopes: ['normal'] }],
  ctx: { workflowRequest: async () => profiles }, title: 'share', multiple: true, save(value) { saved = value; } });
document.querySelector('.choice-trigger').click(); check('stash-config').click();
const platform = document.querySelector('select[aria-label="设备平台"]');
platform.value = 'Windows'; platform.dispatchEvent(new Event('change', { bubbles: true }));
assert.equal(check('stash-config'), null);
platform.value = ''; platform.dispatchEvent(new Event('change', { bubbles: true }));
assert.equal(check('stash-config').checked, true, 'platform filtering must preserve hidden selections');
submit(document.querySelector('form')); await tick();
assert(saved.profiles.includes('stash-config'));
assert(!('device_platform' in saved));

// A failed capability refresh must never authorize submission using the previous
// account's capabilities. Filtering must leave the retry action available.
let failCompatibility = true;
editor = await accountOutputDialog({ accounts: [
  { id: 'a', name: 'A', download_scopes: ['normal'] },
  { id: 'b', name: 'B', download_scopes: ['normal'] },
], ctx: { workflowRequest: async (path) => {
  if (path.includes('id=b') && failCompatibility) throw new Error('offline');
  return profiles;
} }, title: 'share retry', multiple: true, save(value) { saved = value; } });
assert.equal(editor.form.querySelector('.client-compatibility details').open, false, 'compatibility details start collapsed');
editor.input('account_id').value = 'b';
editor.input('account_id').dispatchEvent(new Event('change', { bubbles: true }));
await tick();
editor.input('device_platform').value = 'Windows';
editor.input('device_platform').dispatchEvent(new Event('change', { bubbles: true }));
const retry = [...editor.form.querySelectorAll('button')].find((button) => button.textContent === '重试检查');
assert(retry, 'platform filtering must preserve retry after failed capability checks');
saved = undefined; submit(editor.form); await tick();
assert.equal(saved, undefined);
assert.match(editor.form.querySelector('.form-error').textContent, /重试检查/);
failCompatibility = false; retry.click(); await tick();
submit(editor.form); await tick();
assert.equal(saved.account_id, 'b');

// Replacing the editor aborts follow-up compatibility reads as well as its UI.
let compatibilitySignal, releaseCompatibility;
editor = await accountOutputDialog({ accounts: [
  { id: 'a', name: 'A', download_scopes: ['normal'] },
  { id: 'b', name: 'B', download_scopes: ['normal'] },
], ctx: { workflowRequest: async (path, _, options) => {
  if (path.includes('id=a')) return profiles;
  compatibilitySignal = options.signal;
  return new Promise((resolve) => { releaseCompatibility = resolve; });
} }, title: 'compatibility lifetime', save() {} });
editor.setValue('account_id', 'b');
editor.input('account_id').dispatchEvent(new Event('change', { bubbles: true }));
const { jsonDialog } = await import('../internal/manage/panel/ui.js');
jsonDialog('replacement', {}, () => {});
await tick();
assert.equal(editor.signal.aborted, true);
assert.equal(compatibilitySignal.aborted, true, 'replaced dialog left its query running');
releaseCompatibility(profiles); await tick();
assert.equal(document.querySelector('dialog h2').textContent, 'replacement');

const { accessFields, accessValue, accessOptions, wireAccessMode } = await import('../internal/manage/panel/public-access-fields.js');
for (const trusted_proxy of [[], ['10.2.0.0/24']]) {
  editor = formDialog('access mode', accessFields([]), accessValue({
    listen: '127.0.0.1:2096', public_url: 'https://panel.example.com', trusted_proxy,
  }), (value) => { saved = accessOptions(value, []); });
  wireAccessMode(editor);
  for (const mode of ['tls', 'proxy']) {
    editor.setValue('mode', mode);
    editor.input('mode').dispatchEvent(new Event('change', { bubbles: true }));
  }
  assert(editor.form.checkValidity());
  editor.form.requestSubmit(); await tick();
  assert.deepEqual(saved.trusted_proxy, trusted_proxy.length ? trusted_proxy : ['127.0.0.1/32']);
}

const { dnsServerFields, normalizeDNSServer } = await import('../internal/manage/panel/dns-server-fields.js');
for (const type of ['udp', 'tls', 'https', 'local']) {
  editor = formDialog('DNS protocol', dnsServerFields(['udp', 'tls', 'https', 'local'], []),
    { tag: 'dns-test', type: 'udp', server: '1.1.1.1' }, (value) => { saved = normalizeDNSServer(value); });
  editor.setValue('type', type);
  editor.input('type').dispatchEvent(new Event('change', { bubbles: true }));
  assert(editor.form.checkValidity());
  editor.form.requestSubmit(); await tick();
  assert.equal(saved.type, type);
  assert.equal(saved.server_port, undefined, 'an implicit port must follow the transport');
  assert.equal(saved.path, type === 'https' ? '/dns-query' : undefined);
  assert.equal(saved.server, type === 'local' ? undefined : '1.1.1.1');
}
editor = formDialog('custom DNS', dnsServerFields(['udp', 'tls'], []),
  { tag: 'custom', type: 'tls', server: '192.0.2.1', server_port: 8853, tls: { server_name: 'dns.example.com', alpn: ['dot'] } },
  (value) => { saved = normalizeDNSServer(value); }, true);
editor.form.requestSubmit(); await tick();
assert.equal(saved.server_port, 8853);
assert.deepEqual(saved.tls, { server_name: 'dns.example.com', alpn: ['dot'] });

editor = formDialog('tokens', [{ key: 'values', type: 'csv', label: '域名' }, { key: 'numbers', type: 'numbers', label: 'ASN' }], { values: ['old.test'], numbers: [] }, (value) => { saved = value; });
editor.input('values').value = 'new.test, old.test';
editor.input('numbers').value = 'abc';
saved = undefined; submit(editor.form); await tick();
assert.equal(saved, undefined);
assert.match(document.querySelector('.form-error').textContent, /abc/);
editor.input('numbers').value = '123, 456,123';
submit(editor.form); await tick();
assert.deepEqual(saved.values, ['old.test', 'new.test']);
assert.deepEqual(saved.numbers, [123, 456]);

editor = formDialog('protocols', [{ key: 'protocols', type: 'multiselect', presentation: 'inline', label: '协议', options: ['ss', 'vmess'] }], {}, (value) => { saved = value; });
assert.equal(document.querySelector('.choice-panel').hidden, false, 'inline choices must be visible without a trigger');
check('ss').click(); submit(editor.form); await tick();
assert.deepEqual(saved.protocols, ['ss']);

// Scope changes reset to the explicit new snapshot, but clearing never means all.
editor = formDialog('binding', [
  { key: 'scope', label: '实例', type: 'select', options: ['normal', 'vpngate'] },
  { key: 'ids', label: '入口', type: 'multiselect', required: true, resetOn: ['scope'], initial: (v) => [v.scope], options: (v) => [v.scope] },
], { scope: 'vpngate' }, (value) => { saved = value; });
assert.deepEqual(editor.value().ids, ['vpngate']);
editor.input('scope').value = 'normal'; editor.input('scope').dispatchEvent(new Event('change', { bubbles: true }));
assert.deepEqual(editor.value().ids, ['normal']);
editor.setValue('ids', []); submit(editor.form); await tick();
assert.match(document.querySelector('.form-error').textContent, /请选择/);

const { accounts } = await import('../internal/manage/panel/accounts.js');
const request = async (path) => {
  if (path.startsWith('/api/accounts?')) return { items: [], total: 0 };
  if (path === '/api/account-options') return [{ id: 'account', name: 'Account' }];
  if (path === '/api/inbound-options?scope=normal') return [];
  if (path === '/api/inbound-options?scope=vpngate') return [{ id: 'vpn-node', name: 'VPN node' }];
  throw new Error('unexpected request: ' + path);
};
document.querySelector('main').replaceChildren(...await accounts({
  session: { role: 'admin' }, request, workflowRequest: request, task(command) { saved = command; },
}));
[...document.querySelectorAll('main button')].find((button) => button.textContent === '批量绑定入站').click();
await tick();
assert.equal(document.querySelector('[aria-label="运行实例"]').value, 'vpngate', 'VPNGate-only deployments must select their available scope');
document.querySelector('.choice-trigger').click(); check('account').click();
submit(document.querySelector('form')); await tick();
assert.equal(saved.account.scope, 'vpngate');
assert.deepEqual(saved.account.inbound_ids, ['vpn-node']);
dom.window.close();
console.log('Selection passed: order, focus, filtering, limits, paging, capability retry, VPNGate-only binding and free-value validation.');
