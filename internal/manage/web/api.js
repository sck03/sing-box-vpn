export function createAPI(token, status) {
  let controller;
  async function api(path, body, binary = false) {
    if (!token()) throw Error('请先输入管理令牌');
    if (controller) throw Error('已有请求进行中，可取消后重试');
    controller = new AbortController();
    status('正在处理…');
    try {
      const response = await fetch(path, {
        method: body === undefined ? 'GET' : 'POST',
        headers: { Authorization: 'Bearer ' + token(), 'Content-Type': 'application/json' },
        body: body === undefined ? undefined : JSON.stringify(body),
        signal: controller.signal,
      });
      if (response.status === 409) throw Error('配置已被其他操作修改。请保留草稿，重新载入后合并修改。');
      if (!response.ok) throw Error(await response.text());
      const result = binary ? await response.blob() : await response.text();
      status('操作完成');
      return result;
    } finally {
      controller = null;
    }
  }
  return { api, cancel: () => controller?.abort() };
}
