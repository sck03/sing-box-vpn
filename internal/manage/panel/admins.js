import { refreshAfterSave } from "./lifecycle.js";
import { api, query, mutate } from "./api.js";
import { capabilities } from "./capabilities.js";
import { loadRecords } from "./details.js";
import {
  card,
  row,
  button,
  help,
  table,
  formDialog,
  showResult,
  badge,
  date,
  notice,
} from "./ui.js";
export async function admins(ctx) {
  const policy = (await capabilities()).password_bytes;
  const users = await query("/api/admins", ctx.signal, ctx.request || api);
  const edit = (a) =>
    formDialog(
      "管理员",
      [
        { key: "id", label: "用户名", required: true, readonly: !!a.revision },
        {
          key: "role",
          label: "角色",
          type: "select",
          options: [
            ["admin", "管理员：配置、秘密与系统管理"],
            ["operator", "操作员：运行控制和日志"],
            ["viewer", "只读：状态与用量"],
          ],
        },
        { key: "disabled", label: "禁用", type: "bool" },
        {
          key: "password",
          label: a.revision
            ? "新密码（留空保持）"
            : `密码（${policy.min}–${policy.max} 个 UTF-8 字节）`,
          type: "password",
          required: !a.revision,
          minBytes: policy.min,
          maxBytes: policy.max,
        },
      ],
      a,
      async (v, editor) => {
        const { password, ...admin } = v;
        await mutate("/api/admins", { admin, password });
        notice("管理员已保存，其旧会话已撤销");
        if (v.id === ctx.session.id) ctx.logoutLocal();
        else refreshAfterSave(ctx, editor);
      },
    );
  return [
    card(
      "管理员与角色",
      help(
        "角色在每个 API 中验证。修改密码、角色或禁用状态会撤销该管理员的所有会话，最后一位有效管理员不能被禁用。",
      ),
      row(
        button(
          "新增管理员",
          () => edit({ id: "", role: "viewer", disabled: false, revision: 0 }),
          "primary",
        ),
        button("审计记录", async () =>
          loadRecords(
            "管理审计",
            [
              { label: "时间", value: (v) => date(v.at) },
              { key: "actor", label: "操作人" },
              { key: "action", label: "操作" },
              { key: "target", label: "目标" },
              { key: "result", label: "结果" },
            ],
            "/api/admin/audit",
            ctx.signal,
          ),
        ),
      ),
      table(
        [
          { key: "id", label: "用户名" },
          { key: "role", label: "角色" },
          { label: "MFA", value: (a) => (a.mfa ? "已启用" : "未启用") },
          {
            label: "状态",
            value: (a) => (a.disabled ? "禁用" : "正常"),
            render: (a) =>
              badge(a.disabled ? "禁用" : "正常", a.disabled ? "bad" : "good"),
          },
        ],
        users,
        (a) => [
          button("编辑", () => edit(a)),
          button("撤销会话", async () => {
            await mutate("/api/admin/revoke", { id: a.id });
            notice("会话已撤销");
            if (a.id === ctx.session.id) ctx.logoutLocal();
          }),
        ],
      ),
    ),
    card(
      "本账户 TOTP",
      help(
        "先验证当前密码并生成密钥，在认证器中选择 SHA-1 / 6 位 / 30 秒，再输入验证码确认。确认后需要重新登录；验证码不可重放。失去认证器时通过服务器本机 reset-admin 恢复。",
      ),
      row(
        button("生成认证器密钥", () =>
          formDialog(
            "验证当前密码",
            [
              {
                key: "password",
                label: "当前密码",
                type: "password",
                required: true,
              },
            ],
            {},
            async (v, editor) => {
              const result = await mutate("/api/mfa/enroll", v);
              if (!editor.active() || ctx.signal?.aborted) {
                notice(
                  "认证器密钥已生成；原窗口已关闭，请重新生成后查看并确认。",
                );
                return;
              }
              showResult("TOTP 密钥（10 分钟内确认）", result);
            },
          ),
        ),
        button("确认启用", () =>
          formDialog(
            "确认 TOTP",
            [{ key: "code", label: "6 位动态验证码", required: true }],
            {},
            async (v) => {
              await mutate("/api/mfa/confirm", v);
              ctx.logoutLocal();
              notice("TOTP 已启用，请重新登录");
            },
          ),
        ),
      ),
    ),
  ];
}
