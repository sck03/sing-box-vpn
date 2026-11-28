import { button, resourceDialog, resourceEditor } from "./ui.js";
import { listValues } from "./form-values.js";

const peerFields = [
  { key: "address", label: "服务器地址", required: true },
  { key: "port", label: "端口", type: "number", min: 1, max: 65535 },
  { key: "public_key", label: "公钥", required: true },
  { key: "pre_shared_key", label: "预共享密钥（可留空）", type: "password" },
  { key: "allowed_ips", label: "允许的 IP CIDR", type: "csv", required: true },
  {
    key: "persistent_keepalive_interval",
    label: "保活间隔（秒，0 停用）",
    type: "number",
    min: 0,
    max: 65535,
  },
];

export function wireguardEditor(ctx, endpoints, changed) {
  return resourceEditor({
    title: "WARP / WireGuard 端点",
    rowKey: (item) => item.tag,
    local: true,
    signal: ctx.signal,
    items: endpoints,
    fields: [
      { key: "tag", label: "Tag", required: true },
      { key: "private_key", label: "私钥", type: "password" },
      { key: "address", label: "本地地址 CIDR", type: "csv" },
      {
        key: "mtu",
        label: "MTU",
        type: "number",
        min: 576,
        max: 65535,
        advanced: true,
        help: "留空使用核心默认值；根据实际隧道链路调整。",
      },
    ],
    normalize(value) {
      if (!value.mtu) delete value.mtu;
      return value;
    },
    defaults: {
      type: "wireguard",
      tag: "warp-out",
      address: ["172.16.0.2/32"],
      private_key: "",
      peers: [],
    },
    columns: [
      { key: "tag", label: "Tag" },
      { key: "type", label: "类型" },
    ],
    change: changed,
    extraActions: (endpoint) => [
      button("管理对端", () => {
        const peers = structuredClone(endpoint.peers || []);
        resourceDialog({
          title: "WireGuard 对端 · " + endpoint.tag,
          signal: ctx.signal,
          items: peers,
          fields: peerFields,
          defaults: {
            address: "",
            port: 51820,
            public_key: "",
            allowed_ips: ["0.0.0.0/0", "::/0"],
          },
          columns: [
            { key: "address", label: "服务器" },
            { key: "port", label: "端口" },
            {
              label: "允许网段",
              value: (v) => listValues(v.allowed_ips).join(", "),
            },
          ],
          normalize(value) {
            if (!value.pre_shared_key) delete value.pre_shared_key;
            return value;
          },
          change() {
            const current = endpoints.find(
              (value) => value.tag === endpoint.tag,
            );
            if (!current) throw new Error("端点已改变，请重新打开对端管理。");
            current.peers = structuredClone(peers);
            changed({ local: true });
          },
        });
      }),
    ],
  });
}
