let csrf = "";
let sessionGeneration = 0;
let sessionRequests = new AbortController();
export function setCSRF(value) {
  sessionRequests.abort();
  sessionRequests = new AbortController();
  sessionGeneration++;
  csrf = value || "";
}
export async function api(
  path,
  body,
  { blob = false, signal, timeout = body === undefined ? 15000 : 120000 } = {},
) {
  const generation = sessionGeneration;
  const requestSignal = AbortSignal.any([
    sessionRequests.signal,
    AbortSignal.timeout(timeout),
    ...(signal ? [signal] : []),
  ]);
  const multipart = body instanceof FormData;
  const response = await fetch(path, {
    method: body === undefined ? "GET" : "POST",
    credentials: "same-origin",
    signal: requestSignal,
    headers:
      body === undefined
        ? {}
        : multipart
          ? { "X-CSRF-Token": csrf }
          : { "Content-Type": "application/json", "X-CSRF-Token": csrf },
    body:
      body === undefined ? undefined : multipart ? body : JSON.stringify(body),
  });
  const stale = () => {
    requestSignal.throwIfAborted();
    if (generation !== sessionGeneration)
      throw new DOMException("会话已更改", "AbortError");
  };
  stale();
  if (!response.ok) {
    const error = new Error(
      (await response.text()).trim() || `HTTP ${response.status}`,
    );
    error.status = response.status;
    stale();
    throw error;
  }
  const value = await (blob ? response.blob() : response.json());
  stale();
  return value;
}
export function download(blob, name) {
  const url = URL.createObjectURL(blob),
    link = document.createElement("a");
  link.href = url;
  link.download = name;
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 5000);
}
export function restoreForm(file, password, revision) {
  if (file.size > 96 * 1024 * 1024) throw new Error("备份文件超过 96 MiB");
  const form = new FormData();
  form.set("backup", file);
  form.set("password", password);
  if (revision !== undefined) {
    form.set("revision", String(revision));
    form.set("apply", "true");
  }
  return form;
}
