let csrf = "";
let sessionGeneration = 0;
export function setCSRF(value) {
  sessionGeneration++;
  csrf = value || "";
}
export async function api(path, body, { blob = false, signal } = {}) {
  const generation = sessionGeneration;
  const multipart = body instanceof FormData;
  const response = await fetch(path, {
    method: body === undefined ? "GET" : "POST",
    credentials: "same-origin",
    signal,
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
