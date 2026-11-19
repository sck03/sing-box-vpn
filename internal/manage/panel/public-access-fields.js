// Shared form contract for built-in TLS and an existing HTTPS proxy.
export const accessValue = (options) => ({
  ...options,
  mode: options.certificate
    ? "tls"
    : options.trusted_proxy?.length
      ? "proxy"
      : "local",
  certificate_asset: "",
  trusted_proxy: options.trusted_proxy || [],
});
export function accessFields(certificates, subscription = false) {
  return [
    ...(subscription
      ? [{ key: "enabled", label: "启用独立下载监听", type: "bool" }]
      : []),
    {
      key: "mode",
      label: "访问方式",
      type: "select",
      options: [
        ["tls", "直接 HTTPS（内置 TLS，无需 Nginx/Caddy）"],
        ["proxy", "HTTPS 反向代理（自行配置 Nginx/Caddy）"],
        ...(!subscription ? [["local", "本机 / SSH 转发"]] : []),
      ],
    },
    {
      key: "public_url",
      label: subscription ? "公网下载 URL" : "面板公网 URL",
      visible: (v) => v.mode !== "local",
      required: (v) => v.mode !== "local" && (!subscription || v.enabled),
      help: subscription
        ? "例如 https://sub.example.com。只有反向代理允许路径前缀，代理应去掉前缀再转发。"
        : "例如 https://panel.example.com:2096；只填写协议、域名/IP 与端口。",
    },
    {
      key: "listen",
      label: "监听地址",
      required: true,
      help: "直接 HTTPS：0.0.0.0:2096（订阅常用 2097）；本机代理 / SSH：127.0.0.1:端口；IPv6：[::]:端口。",
    },
    {
      key: "certificate_asset",
      label: "使用已有证书资产（可选）",
      type: "select",
      options: [
        ["", "手动填写证书路径"],
        ...certificates.map((a) => [a.id, a.domain]),
      ],
      visible: (v) => v.mode === "tls",
    },
    {
      key: "certificate",
      label: "TLS 证书路径",
      visible: (v) => v.mode === "tls" && !v.certificate_asset,
      help: "填写服务器 PEM 完整链路径，或先申请 / 导入证书。",
    },
    {
      key: "key",
      label: "TLS 私钥路径",
      visible: (v) => v.mode === "tls" && !v.certificate_asset,
    },
    {
      key: "trusted_proxy",
      label: "可信 HTTPS 代理 CIDR（逗号分隔）",
      type: "csv",
      visible: (v) => v.mode === "proxy",
      help: "需要自行安装和配置 Nginx/Caddy。本机代理通常填 127.0.0.1/32，保留 Host 并设置 X-Forwarded-Proto: https。",
    },
  ];
}
export function accessOptions(v, certificates) {
  const asset = certificates.find((a) => a.id === v.certificate_asset);
  return {
    listen: v.listen.trim(),
    public_url:
      v.mode === "local" ? "" : v.public_url.trim().replace(/\/$/, ""),
    certificate: v.mode === "tls" ? asset?.certificate || v.certificate : "",
    key: v.mode === "tls" ? asset?.key || v.key : "",
    trusted_proxy: v.mode === "proxy" ? v.trusted_proxy : [],
  };
}
export function wireAccessMode() {
  const form = document.querySelector("#dialog form");
  form.addEventListener("change", (event) => {
    const target = /** @type {HTMLSelectElement} */ (event.target);
    if (target.getAttribute("aria-label") !== "访问方式") return;
    const listen = /** @type {HTMLInputElement} */ (form.querySelector('[aria-label="监听地址"]')),
      port = listen.value.match(/:(\d+)$/)?.[1] || "2096";
    listen.value =
      (target.value === "tls" ? "0.0.0.0:" : "127.0.0.1:") + port;
    const proxy = /** @type {HTMLInputElement} */ (form.querySelector('[aria-label="可信 HTTPS 代理 CIDR（逗号分隔）"]'));
    if (target.value === "proxy" && !proxy.value)
      proxy.value = "127.0.0.1/32";
  });
}
