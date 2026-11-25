export * from "./dom.js";
export { table, remoteTable } from "./tables.js";
import { $, el, button, card, help, row } from "./dom.js";
import { table } from "./tables.js";
import { formDialog } from "./form-dialog.js";
export { formDialog } from "./form-dialog.js";

export function jsonDialog(title, value, save) {
  const dialog = $("#dialog"),
    input = el("textarea", {
      class: "json",
      "aria-label": title,
      spellcheck: "false",
    });
  input.value = JSON.stringify(value, null, 2);
  dialog.replaceChildren(
    el("h2", {}, title),
    help("保留已有字段；服务端会校验资源引用、能力与配置版本。"),
    input,
    row(
      button("取消", () => dialog.close()),
      button(
        "保存到草稿",
        async () => {
          await save(JSON.parse(input.value));
          if (dialog.contains(input)) dialog.close();
        },
        "primary",
      ),
    ),
  );
  dialog.showModal();
}
export function resourceEditor({
  title,
  items,
  fields,
  defaults,
  change: publishChange,
  columns = undefined,
  normalize = (v, _previous = undefined) => v,
  preserve = false,
  extraActions = (_item = undefined) => [],
  local = false,
  rowKey = undefined,
  pageSize = undefined,
  compact = true,
}) {
  let view;
  const change = () => {
    if (local) view.updateItems(items);
    publishChange(local ? { local: true } : undefined);
  };
  return card(
    title,
    row(
      button(
        "新增",
        () =>
          formDialog("新增" + title, fields, structuredClone(defaults), (v) => {
            items.push(normalize(v));
            change();
          }),
        "primary",
      ),
      button("高级 JSON", () =>
        jsonDialog(title, items, (v) => {
          if (!Array.isArray(v)) throw new Error("需要数组");
          items.splice(0, items.length, ...v);
          change();
        }),
      ),
    ),
    (view = table(
      columns || [
        { key: "id", label: "ID" },
        { key: "name", label: "名称" },
      ],
      items,
      (item) => [
        button("编辑", () =>
          formDialog(
            "编辑" + title,
            fields,
            item,
            (v) => {
              items[items.indexOf(item)] = normalize(v, item);
              change();
            },
            preserve,
          ),
        ),
        ...extraActions(item),
        button("复制", () => {
          const v = structuredClone(item);
          if (v.id !== undefined) v.id += "-copy";
          else if (v.tag) v.tag += "-copy";
          formDialog(
            "复制" + title,
            fields,
            v,
            (result) => {
              items.push(normalize(result, v));
              change();
            },
            preserve,
          );
        }),
        button("JSON", () =>
          jsonDialog(title, item, (v) => {
            items[items.indexOf(item)] = v;
            change();
          }),
        ),
        button(
          "删除",
          () => {
            if (confirm("删除此资源？保存时会检查仍在使用的引用。")) {
              items.splice(items.indexOf(item), 1);
              change();
            }
          },
          "danger",
        ),
      ],
      { key: `resource:${title}`, rowKey, pageSize, compact },
    )),
  );
}
