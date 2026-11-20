import { el, card, help } from "./ui.js";

export function setupGuide(ctx) {
  if (ctx.session.role !== "admin") return null;
  return el(
    "details",
    { class: "setup-guide" },
    el("summary", {}, "首次使用：从安装到分享订阅"),
    card(
      "按顺序完成设置",
      help(
        "入站创建连接入口；用户权益决定谁能连接、可用流量和有效期。只创建节点还不能登录代理。",
      ),
      el(
        "ol",
        { class: "setup-steps" },
        ...[
          [
            "1. 检查节点",
            "确认协议、端口和服务器地址，放行对应的云安全组端口。",
            "inbounds",
            "打开入站设置",
          ],
          [
            "2. 创建用户并绑定入站",
            "设置流量与有效期，绑定允许使用的节点。",
            "accounts",
            "打开权益与用量",
          ],
          [
            "3. 配置 HTTPS（公开分享时）",
            "申请或导入证书并应用，再在公网访问中选择证书、保存并重启。",
            "access/panel",
            "打开公网访问",
          ],
          [
            "4. 分享到客户端",
            "在用户权益中复制订阅链接，选择客户端并导入。",
            "accounts",
            "打开用户分享",
          ],
        ].map(([title, text, route, label]) =>
          el(
            "li",
            {},
            el("strong", {}, title),
            help(text),
            el("a", { href: "#" + route }, label),
          ),
        ),
      ),
      help(
        "表单保存进入草稿；顶部「校验并应用配置」提交并应用。面板访问设置单独保存，需要点击「重启并应用」。",
      ),
    ),
  );
}
