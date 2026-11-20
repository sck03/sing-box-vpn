import { api } from "./api.js";
import { button, formDialog, notice } from "./ui.js";

// Creation returns an asset to the current draft. Applying remains one explicit transaction.
/** @param {any} ctx @param {any} config @param {string} provider @param {(asset: any) => any} [created] @param {() => any} [back] */
export function certificateEditor(
  ctx,
  config,
  provider,
  created = () => ctx.render(),
  back,
) {
  const importing = provider === "import";
  const fields = [
    {
      key: "id",
      label: "证书名称（唯一 ID）",
      required: true,
      help: "使用字母、数字、短横线或下划线，例如 panel-cert。",
    },
    { key: "domain", label: "证书域名", required: true },
    ...(importing
      ? [
          {
            key: "certificate",
            label: "证书 PEM",
            type: "textarea",
            wide: true,
            required: true,
          },
          {
            key: "key",
            label: "私钥 PEM",
            type: "textarea",
            wide: true,
            required: true,
          },
        ]
      : [
          {
            key: "email",
            label: "联系邮箱",
            type: "email",
            required: true,
            help: "申请前将域名指向本机，并放行公网 TCP 80。应用草稿后会自动申请，后续自动续期。",
          },
        ]),
  ];
  const editor = formDialog(
    importing ? "导入证书" : "申请自动证书",
    fields,
    {},
    async (value) => {
      value.id = value.id.trim();
      value.domain = value.domain.trim();
      if (config.certificates.some((asset) => asset.id === value.id))
        throw new Error("证书名称已存在，请使用新的名称");
      const asset = await api(
        importing ? "/api/certificates" : "/api/certificates/prepare",
        value,
        { signal: ctx.signal },
      );
      if (
        !editor.dialog.contains(editor.form) ||
        !editor.dialog.open ||
        ctx.signal?.aborted
      )
        return;
      config.certificates.push(asset);
      ctx.changed();
      notice(
        importing
          ? "证书已导入并加入草稿；应用后保存到证书管理。"
          : "自动证书已加入草稿；请先应用并等待申请成功，再用于公网 HTTPS。",
      );
      created(asset);
    },
  );
  if (back) {
    const cancel = editor.form.querySelector('button[type="button"]');
    cancel?.replaceWith(button("返回访问设置", back));
  }
  return editor;
}
