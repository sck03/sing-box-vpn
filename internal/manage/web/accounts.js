export function installAccounts({ $, api, bind, show, save }) {
  let accounts = [], credentials = [];
  const command = body => api('/api/accounts', body);
  function selected() {
    const id = $('accountSelect').value;
    if (!id) throw Error('请先保存并选择账户');
    return id;
  }
  function display() {
    const a = accounts.find(a => a.id === $('accountSelect').value);
    $('accountID').readOnly = !!a;
    $('accountID').value = a?.id || '';
    $('accountName').value = a?.name || '';
    $('accountQuota').value = a?.quota_bytes ?? '';
    $('accountDisabled').checked = !!a?.disabled;
    const date = a?.expires_at ? new Date(a.expires_at) : null;
    $('accountExpiry').value = date ? new Date(date - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16) : '';
    $('accountUsage').textContent = a ? `已结算上行：${a.upload_bytes} B\n已结算下行：${a.download_bytes} B\n已分配额度：${a.charged_bytes} B（包括运行时预留）\n版本：${a.revision}` : '新建账户；不同格式共用此账户的额度。';
    credentials = []; $('accountCredential').replaceChildren();
  }
  async function load(id = $('accountSelect').value) {
    accounts = JSON.parse(await api('/api/accounts'));
    $('accountSelect').replaceChildren(new Option('新建账户', ''), ...accounts.map(a => new Option(`${a.name} · ${a.id}`, a.id)));
    $('accountSelect').value = id; display();
  }
  $('accountSelect').onchange = display;
  bind('accountLoad', () => load());
  bind('accountSave', async () => {
    const previous = accounts.find(a => a.id === $('accountSelect').value);
    const text = $('accountQuota').value.trim();
    const quota = text === '' ? null : Number(text);
    if (quota !== null && (!Number.isSafeInteger(quota) || quota < 0)) throw Error('额度需要为非负安全整数');
    const id = $('accountID').value.trim();
    await command({ operation: 'save', account: { id, name: $('accountName').value.trim(), quota_bytes: quota, expires_at: $('accountExpiry').value ? new Date($('accountExpiry').value).toISOString() : null, disabled: $('accountDisabled').checked, revision: previous?.revision || 0 } });
    await load(id); show('账户已保存。');
  });
  bind('accountBind', async () => show(await command({ operation: 'bind', id: selected(), scope: $('accountScope').value })));
  bind('accountExport', async () => {
    const id = selected(), client = $('accountClient').value;
    const result = await command({ operation: 'export', id, scope: $('accountScope').value, client });
    save(new Blob([result], { type: 'text/plain' }), `${id}-${client}.${client === 'sing-box' ? 'json' : client === 'mihomo' ? 'yaml' : 'txt'}`);
  });
  bind('accountCredentials', async () => {
    credentials = JSON.parse(await command({ operation: 'credentials', id: selected(), scope: $('accountScope').value }));
    $('accountCredential').replaceChildren(...credentials.map(c => new Option(`${c.protocol} · 第 ${c.generation} 代`, c.id)));
    show(credentials);
  });
  bind('accountRotate', async () => {
    const c = credentials.find(c => c.id === $('accountCredential').value);
    if (!c) throw Error('请先查看并选择凭据');
    await command({ operation: 'rotate', id: c.id, generation: c.generation });
    credentials = []; $('accountCredential').replaceChildren(); show('旧凭据已撤销。请重启对应受管实例后重新下载配置。');
  });
  return () => { accounts = []; credentials = []; $('accountSelect').replaceChildren(new Option('新建账户', '')); display(); };
}
