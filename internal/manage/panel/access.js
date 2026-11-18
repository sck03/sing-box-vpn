import { api } from "./api.js";
import { card, help, el, row, button, formDialog, showResult } from "./ui.js";

export async function access(ctx) {
  const request = ctx.request || api;
  const settings = await request("/api/panel/settings");
  const c = await ctx.config();
  const edit = () => {
    const options = settings.options;
    const value = {
      ...options,
      mode: options.certificate ? "tls" : options.trusted_proxy?.length ? "proxy" : "local",
      certificate_asset: "",
      trusted_proxy: options.trusted_proxy || [],
    };
    formDialog("配置面板访问", [
      { key: "mode", label: "访问方式", type: "select", options: [
        ["tls", "直接 HTTPS（域名/IP + 证书）"],
        ["proxy", "HTTPS 反向代理（Nginx/Caddy）"],
        ["local", "本机 / SSH 转发"],
      ] },
      { key: "public_url", label: "面板公网 URL", required: (v) => v.mode !== "local",
        help: "例如 https://panel.example.com:2096；只填写协议、域名/IP 与端口。公网方式必须使用 HTTPS。" },
      { key: "listen", label: "监听地址", required: true,
        help: "直接 HTTPS：0.0.0.0:2096；本机反向代理或 SSH：127.0.0.1:2096。IPv6 使用 [::]:2096。" },
      { key: "certificate_asset", label: "使用已有证书资产（可选）", type: "select",
        options: [["", "手动填写证书路径"], ...(c.certificates || []).map((a) => [a.id, a.domain])],
        visible: (v) => v.mode === "tls" },
      { key: "certificate", label: "TLS 证书路径", visible: (v) => v.mode === "tls" && !v.certificate_asset,
        help: "填写服务器上的 PEM 完整链路径，或先在「证书资产」申请/导入证书。" },
      { key: "key", label: "TLS 私钥路径", visible: (v) => v.mode === "tls" && !v.certificate_asset },
      { key: "trusted_proxy", label: "可信 HTTPS 代理 CIDR（逗号分隔）", type: "csv",
        visible: (v) => v.mode === "proxy", help: "本机 Nginx/Caddy 通常填写 127.0.0.1/32。代理应保留 Host 并设置 X-Forwarded-Proto: https。" },
    ], value, async (v) => {
      const asset = c.certificates.find((a) => a.id === v.certificate_asset);
      const next = {
        listen: v.listen.trim(), public_url: v.public_url.trim().replace(/\/$/, ""),
        certificate: v.mode === "tls" ? asset?.certificate || v.certificate : "",
        key: v.mode === "tls" ? asset?.key || v.key : "",
        trusted_proxy: v.mode === "proxy" ? v.trusted_proxy : [],
      };
      const validated = await api("/api/panel/settings/preview", { options: next });
      const dialog = document.querySelector("#dialog");
      dialog.replaceChildren(
        el("h2", {}, "确认面板访问配置"),
        help("保存后，在服务器执行 systemctl restart sb-panel，然后用新地址重新登录。云安全组需放行实际 HTTPS 端口。"),
        el("pre", {}, JSON.stringify(validated.options, null, 2)),
        row(button("返回编辑", edit), button("保存配置", async () => {
          await api("/api/panel/settings", { options: validated.options, revision: settings.revision });
          showResult("面板访问配置已保存", "在服务器执行：\nsystemctl restart sb-panel\n\n随后访问：" + (next.public_url || "SSH 转发地址") + "\n重启后新分享会使用生效的公网地址。");
          ctx.render();
        }, "primary")),
      );
      dialog.showModal();
    });
    const form = document.querySelector("#dialog form");
    form.addEventListener("change", (event) => {
      if (event.target.getAttribute("aria-label") !== "访问方式") return;
      const listen = form.querySelector('[aria-label="监听地址"]');
      const port = listen.value.match(/:(\d+)$/)?.[1] || "2096";
      listen.value = (event.target.value === "tls" ? "0.0.0.0:" : "127.0.0.1:") + port;
      if (event.target.value === "local") form.querySelector('[aria-label="面板公网 URL"]').value = "";
      const proxy = form.querySelector('[aria-label="可信 HTTPS 代理 CIDR（逗号分隔）"]');
      if (event.target.value === "proxy" && !proxy.value) proxy.value = "127.0.0.1/32";
    });
  };
  return [
    card("面板公网访问", help("普通账户订阅可以直接使用 HTTPS 面板域名，无需另开下载端口。域名 DNS 应指向 VPS；证书可在「证书资产」申请或导入。"),
      el("p", {}, "当前访问地址：", el("code", {}, settings.active.public_url)),
      settings.restart_required ? help("已保存配置与当前运行配置不同；重启面板后生效。") : null,
      button("配置面板访问", edit, "primary"),
    ),
    card("独立订阅地址（可选）", help("需要将订阅与后台分开时启用。下载监听本身使用 HTTP，公网 HTTPS 可由 Nginx/Caddy 转发到本机 2097；公网 URL 可以含反向代理路径前缀。普通分享与高级输出都会使用这个地址。"),
      el("p", {}, c.listener.enabled ? "已启用：" + (c.listener.public_base_url || "尚未填写公网 URL") : "未启用，账户分享使用面板地址。"),
      button("配置独立下载监听", () => formDialog("独立下载监听", [
        { key: "enabled", label: "启用独立下载监听", type: "bool" },
        { key: "listen", label: "监听 IP", required: true },
        { key: "port", label: "端口", type: "number", min: 1, max: 65535 },
        { key: "public_base_url", label: "公网下载 URL", type: "url", required: (v) => v.enabled,
          help: "例如 https://sub.example.com；回环监听必须配合反向代理。" },
      ], c.listener, (v) => { c.listener = v; ctx.changed(); ctx.render(); })),
    ),
  ];
}
