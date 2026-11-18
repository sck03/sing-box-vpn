export * from "./dom.js";
export { table, remoteTable } from "./tables.js";
import {
  $,
  el,
  action,
  button,
  card,
  help,
  row,
  getPath,
  setPath,
} from "./dom.js";
import { table } from "./tables.js";

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
export function formDialog(title, fields, value, save) {
  const dialog = $("#dialog"),
    form = el("form"),
    area = el("div", { class: "fields" });
  const inputs = [];
  const advanced = el("div", { class: "fields" });
  for (const field of fields) {
    const target = field.advanced ? advanced : area;
    if (field.section)
      target.append(el("h3", { class: "field-section wide" }, field.section));
    const raw = getPath(value, field.key);
    const current = field.scale && raw != null ? raw / field.scale : raw;
    let input;
    if (field.type === "select" || field.type === "multiselect") {
      input = el("select");
      input.multiple = field.type === "multiselect";
      const options =
        typeof field.options === "function"
          ? field.options(value)
          : field.options;
      for (const option of options || []) {
        const [v, label] = Array.isArray(option) ? option : [option, option];
        input.append(el("option", { value: v }, label));
      }
      input.value = current ?? field.default ?? "";
      if (input.multiple)
        for (const option of input.options)
          option.selected = (current || []).includes(option.value);
    } else if (field.type === "json" || field.type === "textarea") {
      input = el("textarea", { spellcheck: "false" });
      input.value =
        field.type === "json"
          ? JSON.stringify(current ?? field.default ?? {}, null, 2)
          : (current ?? "");
    } else {
      input = el("input", {
        type:
          field.type === "bool"
            ? "checkbox"
            : field.type === "number"
              ? "number"
              : field.type === "date"
                ? "datetime-local"
                : field.type || "text",
      });
      if (field.type === "bool") input.checked = !!current;
      else if (field.type === "date") {
        if (current) {
          const d = new Date(current);
          input.value = new Date(d.getTime() - d.getTimezoneOffset() * 60000)
            .toISOString()
            .slice(0, 19);
        }
        input.step = "1";
      } else
        input.value = ["csv", "numbers"].includes(field.type)
          ? (current || []).join(", ")
          : (current ?? field.default ?? "");
    }
    if (field.required) input.required = true;
    if (field.readonly) input.disabled = true;
    if (field.min !== undefined) input.min = field.min;
    if (field.max !== undefined) input.max = field.max;
    if (field.scale) input.step = "any";
    input.setAttribute("aria-label", field.label);
    const label = el(
      "label",
      { class: field.wide ? "wide" : "" },
      field.label,
      input,
    );
    if (field.help) label.append(el("small", {}, field.help));
    target.append(label);
    inputs.push([field, input, input.value]);
  }
  const formValue = () => {
    const current = structuredClone(value);
    for (const [field, input] of inputs)
      setPath(
        current,
        field.key,
        field.type === "bool"
          ? input.checked
          : field.type === "multiselect"
            ? [...input.selectedOptions].map((o) => o.value)
            : input.value,
      );
    return current;
  };
  const refreshFields = () => {
    let current = formValue();
    for (const [field, input] of inputs) {
      if (typeof field.options === "function") {
        const previous = input.multiple
          ? [...input.selectedOptions].map((o) => o.value)
          : input.value;
        const options = field.options(current);
        input.replaceChildren(
          ...options.map((option) => {
            const [v, label] = Array.isArray(option)
              ? option
              : [option, option];
            return el("option", { value: v }, label);
          }),
        );
        if (input.multiple) {
          for (const option of input.options)
            option.selected = previous.includes(option.value);
        } else if ([...input.options].some((o) => o.value === previous))
          input.value = previous;
        setPath(
          current,
          field.key,
          input.multiple
            ? [...input.selectedOptions].map((o) => o.value)
            : input.value,
        );
      }
      const visible = !field.visible || field.visible(current);
      input.required =
        typeof field.required === "function"
          ? field.required(current)
          : !!field.required;
      input.closest("label").hidden = !visible;
      input.disabled = !!field.readonly || !visible;
    }
  };
  form.addEventListener("change", refreshFields);
  form.addEventListener(
    "invalid",
    (event) => {
      for (
        let parent = event.target.parentElement;
        parent && parent !== form;
        parent = parent.parentElement
      )
        if (parent.tagName === "DETAILS") parent.open = true;
    },
    true,
  );
  refreshFields();
  if (advanced.childElementCount)
    area.append(
      el(
        "details",
        { class: "advanced wide" },
        el("summary", {}, "高级设置（可选）"),
        advanced,
      ),
    );
  form.append(
    area,
    el("p", { class: "form-error", role: "alert", hidden: true }),
    row(
      button("取消", () => dialog.close()),
      el("button", { class: "primary", type: "submit" }, "保存"),
    ),
  );
  form.addEventListener(
    "submit",
    action(async (event) => {
      event.preventDefault();
      const errorNode = form.querySelector(".form-error");
      errorNode.hidden = true;
      try {
        const result = structuredClone(value);
        for (const [field, input, initial] of inputs) {
          if (field.readonly || input.disabled) continue;
          if (field.type === "date" && input.value === initial) continue;
          let v = input.value;
          if (field.minBytes !== undefined || field.maxBytes !== undefined) {
            const length = new TextEncoder().encode(v).length;
            if (
              (v || field.required) &&
              (length < (field.minBytes || 0) ||
                length > (field.maxBytes ?? Infinity))
            )
              throw new Error(
                `${field.label}需要 ${field.minBytes || 0}–${field.maxBytes} 个 UTF-8 字节，当前为 ${length}。`,
              );
          }
          switch (field.type) {
            case "multiselect":
              v = [...input.selectedOptions].map((o) => o.value);
              break;
            case "bool":
              v = input.checked;
              break;
            case "number":
              if (v === "") {
                v = field.nullable ? null : 0;
              } else {
                v = Number(v) * (field.scale || 1);
                if (field.scale) v = Math.round(v);
                if (!Number.isSafeInteger(v))
                  throw new Error(field.label + "需要安全范围内的整数");
              }
              break;
            case "json":
              v = JSON.parse(v);
              break;
            case "csv":
            case "numbers":
              v = v
                .split(",")
                .map((s) => s.trim())
                .filter(Boolean);
              if (field.type === "numbers") {
                v = v.map(Number);
                if (
                  v.some(
                    (n) => !Number.isSafeInteger(n) || n < 1 || n > 4294967295,
                  )
                )
                  throw new Error(
                    field.label + "需要 1–4294967295 范围内的整数",
                  );
              }
              break;
            case "date":
              v = v ? new Date(v).toISOString() : null;
              break;
          }
          setPath(result, field.key, v);
        }
        await save(result);
      } catch (error) {
        errorNode.textContent = error.message;
        errorNode.hidden = false;
        return;
      }
      if (dialog.contains(form)) dialog.close();
    }),
  );
  dialog.replaceChildren(el("h2", {}, title), form);
  dialog.showModal();
}
export function resourceEditor({
  title,
  items,
  fields,
  defaults,
  change,
  columns,
  normalize = (v) => v,
  extraActions = () => [],
}) {
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
    table(
      columns || [
        { key: "id", label: "ID" },
        { key: "name", label: "名称" },
      ],
      items,
      (item) => [
        button("编辑", () =>
          formDialog("编辑" + title, fields, item, (v) => {
            items[items.indexOf(item)] = normalize(v);
            change();
          }),
        ),
        ...extraActions(item),
        button("复制", () => {
          const v = structuredClone(item);
          if (v.id !== undefined) v.id += "-copy";
          else if (v.tag) v.tag += "-copy";
          formDialog("复制" + title, fields, v, (result) => {
            items.push(normalize(result));
            change();
          });
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
    ),
  );
}
