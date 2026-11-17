import { api } from "./api.js";
import { formDialog, card, help, button, table, badge } from "./ui.js";
export async function subscribers(ctx) {
  const people = await (ctx.request || api)("/api/subscribers");
  const edit = (v) =>
    formDialog(
      "订阅者",
      [
        { key: "id", label: "ID", required: true, readonly: !!v.revision },
        { key: "name", label: "名称", required: true },
        { key: "disabled", label: "禁用全部所属权益", type: "bool" },
      ],
      v,
      async (v) => {
        await api("/api/subscribers", v);
        ctx.render();
      },
    );
  return [
    card(
      "订阅者",
      help(
        "禁用订阅者会阻止其全部权益的代理访问和下载。订阅者不是面板管理员。",
      ),
      ctx.session.role === "admin"
        ? button(
            "新增订阅者",
            () => edit({ id: "", name: "", disabled: false, revision: 0 }),
            "primary",
          )
        : null,
      table(
        [
          { key: "id", label: "ID" },
          { key: "name", label: "名称" },
          {
            label: "状态",
            render: (v) =>
              badge(v.disabled ? "禁用" : "正常", v.disabled ? "bad" : "good"),
          },
        ],
        people,
        ctx.session.role === "admin"
          ? (v) => [button("编辑", () => edit(v))]
          : null,
      ),
    ),
  ];
}
