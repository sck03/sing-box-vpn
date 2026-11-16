let csrf = "";
export function setCSRF(value) {
  csrf = value || "";
}
export async function api(path, body, { blob = false, signal } = {}) {
  const response = await fetch(path, {
    method: body === undefined ? "GET" : "POST",
    credentials: "same-origin",
    signal,
    headers:
      body === undefined
        ? {}
        : { "Content-Type": "application/json", "X-CSRF-Token": csrf },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  if (!response.ok) {
    const error = new Error(
      (await response.text()).trim() || `HTTP ${response.status}`,
    );
    error.status = response.status;
    throw error;
  }
  return blob ? response.blob() : response.json();
}
export function download(blob, name) {
  const url = URL.createObjectURL(blob),
    link = document.createElement("a");
  link.href = url;
  link.download = name;
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 5000);
}
export async function base64(file) {
  if (file.size > 96 * 1024 * 1024) throw new Error("备份文件超过 96 MiB");
  return new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve(String(r.result).split(",")[1]);
    r.onerror = reject;
    r.readAsDataURL(file);
  });
}
