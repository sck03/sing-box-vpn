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

/** @param {RequestError | Error} error */
export function errorMessage(error) {
  if (error instanceof RequestError) {
    if (error.code === "revision_conflict")
      return "服务器内容已更新。请保留当前修改，重新载入并核对后保存。";
    if (error.code === "unauthorized") return "登录已失效，请重新登录。";
    return (
      error.message +
      (error.retryable ? " 可稍后重试；提交前请先确认上一次操作结果。" : "")
    );
  }
  return error.message;
}
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

/** @param {import("./contracts.js").ConfigurationSave} input
 * @returns {Promise<import("./contracts.js").QueuedTask>} */
export const saveConfiguration = (input) => api("/api/config", input);
/** @param {import("./contracts.js").TaskCommand} input
 * @returns {Promise<import("./contracts.js").QueuedTask>} */
export const submitTask = (input) => api("/api/tasks", input);
/** @param {number} id @param {AbortSignal} [signal]
 * @returns {Promise<import("./contracts.js").Task>} */
export const getTask = (id, signal) =>
  api("/api/tasks/detail?id=" + id, undefined, { signal });
/** @param {number} [before] @param {AbortSignal} [signal]
 * @returns {Promise<import("./contracts.js").TaskPage>} */
export const getTasks = (before = 0, signal) =>
  api("/api/tasks?before=" + before + "&limit=50", undefined, { signal });
/** @param {AbortSignal} [signal]
 * @returns {Promise<import("./contracts.js").OverviewSnapshot>} */
export const getOverview = (signal) =>
  api("/api/overview", undefined, { signal });
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

/** @param {boolean} [check] @param {AbortSignal} [signal]
 * @returns {Promise<import("./contracts.js").ProgramStatus>} */
export const getProgram = (check = false, signal) =>
  api("/api/program" + (check ? "?check=true" : ""), undefined, {
    signal,
    timeout: 45000,
  });
/** @param {import("./contracts.js").SubscriptionTemplateImport} input @param {AbortSignal} [signal]
 * @returns {Promise<import("./contracts.js").SubscriptionTemplateImportResult>} */
export const previewTemplateImport = (input, signal) =>
  api("/api/subscription-templates/import", input, { signal });
/** @param {string} id @param {string} scope @param {AbortSignal} [signal] @param {typeof api} [request]
 * @returns {Promise<import("./contracts.js").SubscriptionAccountOutputOption[]>} */
export const getAccountOutputs = (id, scope, signal, request = api) =>
  request(
    "/api/accounts/outputs?id=" +
      encodeURIComponent(id) +
      "&scope=" +
      encodeURIComponent(scope || ""),
    undefined,
    { signal },
  );
/** @param {import("./contracts.js").AccountExportRequest} input @param {typeof api} [request]
 * @returns {Promise<import("./contracts.js").AccountExportResponse>} */
export const exportAccount = (input, request = api) =>
  request("/api/accounts/export", input);
/** @param {boolean} secret @param {AbortSignal} [signal]
 * @returns {Promise<import("./contracts.js").ConfigurationState>} */
export const getConfiguration = (secret, signal) =>
  api(secret ? "/api/config" : "/api/state", undefined, { signal });
