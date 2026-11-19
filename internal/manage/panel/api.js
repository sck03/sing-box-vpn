/** @typedef {import("./contracts.js").APIError} ErrorDetail */
export class RequestError extends Error {
  /** @param {string} message @param {number} status @param {ErrorDetail} [detail] */
  constructor(message, status, detail) {
    super(message);
    this.name = "RequestError";
    this.status = status;
    this.code = detail?.code || "http_error";
    this.field = detail?.field;
    this.retryable = detail?.retryable === true;
  }
}
let csrf = "";
let sessionGeneration = 0;
let sessionRequests = new AbortController();
/** @param {string | null | undefined} value */
export function setCSRF(value) {
  sessionRequests.abort();
  sessionRequests = new AbortController();
  sessionGeneration++;
  csrf = value || "";
}
/**
 * @param {string} path
 * @param {unknown} [body]
 * @param {{blob?: boolean, signal?: AbortSignal, timeout?: number}} [options]
 */
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
    const text = (await response.text()).trim();
    let detail;
    try {
      detail = JSON.parse(text).error;
    } catch {}
    const error = new RequestError(
      detail?.message || text || `HTTP ${response.status}`,
      response.status,
      detail,
    );
    stale();
    throw error;
  }
  const value = await (blob ? response.blob() : response.json());
  stale();
  return value;
}
/** @param {Blob} blob @param {string} name */
export function download(blob, name) {
  const url = URL.createObjectURL(blob),
    link = document.createElement("a");
  link.href = url;
  link.download = name;
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 5000);
}
/** @param {File} file @param {string} password @param {number} [revision] */
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
