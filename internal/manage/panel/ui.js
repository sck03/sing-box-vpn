export const $ = (selector, root = document) => root.querySelector(selector);
export function el(tag, attrs = {}, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(attrs)) {
    if (key.startsWith("on")) node.addEventListener(key.slice(2), value);
    else if (key === "class") node.className = value;
    else if (value !== undefined && value !== null)
      node.setAttribute(key, String(value));
  }
  for (const child of children.flat()) {
    if (child !== null && child !== undefined)
      node.append(
        child instanceof Node ? child : document.createTextNode(String(child)),
      );
  }
  return node;
}
export function notice(message, error = false) {
  const node = $("#notice");
  node.textContent = message;
  node.classList.toggle("error", error);
  node.hidden = false;
  clearTimeout(notice.timer);
  notice.timer = setTimeout(() => (node.hidden = true), error ? 16000 : 6500);
}
export function action(fn) {
  return async (event) => {
    const button = event?.currentTarget;
    try {
      if (button) button.disabled = true;
      await fn(event);
    } catch (error) {
      if (error.name !== "AbortError") notice(error.message, true);
    } finally {
      if (button?.isConnected) button.disabled = false;
    }
  };
}
export const button = (label, fn, kind = "") =>
  el("button", { type: "button", class: kind, onclick: action(fn) }, label);
export const card = (title, ...body) =>
  el("section", { class: "card" }, el("h2", {}, title), ...body);
export const help = (text) => el("p", { class: "help" }, text);
export const row = (...children) => el("div", { class: "row" }, ...children);
export const badge = (text, kind = "") =>
  el("span", { class: "tag " + kind }, text);
export function bytes(value) {
  if (value === null || value === undefined) return "不限";
  const n = Number(value);
  if (n < 1024) return `${n} B`;
  const units = ["KiB", "MiB", "GiB", "TiB", "PiB"];
  let v = n / 1024,
    i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(2)} ${units[i]}`;
}
export function date(value) {
  if (!value) return "—";
  return new Date(
    typeof value === "number" ? value * 1000 : value,
  ).toLocaleString();
}
export function getPath(object, path) {
  return path.split(".").reduce((v, k) => v?.[k], object);
}
export function setPath(object, path, value) {
  const parts = path.split(".");
  let cursor = object;
  for (const key of parts.slice(0, -1)) {
    cursor[key] ??= {};
    cursor = cursor[key];
  }
  cursor[parts.at(-1)] = value;
}
export function showResult(title, value) {
  const dialog = $("#dialog");
  dialog.replaceChildren(
    el("h2", {}, title),
    el(
      "pre",
      {},
      typeof value === "string" ? value : JSON.stringify(value, null, 2),
    ),
    button("关闭", () => dialog.close()),
  );
  dialog.showModal();
}
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
          dialog.close();
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
  for (const field of fields) {
    const current = getPath(value, field.key);
    let input;
    if (field.type === "select") {
      input = el("select");
      for (const option of field.options || []) {
        const [v, label] = Array.isArray(option) ? option : [option, option];
        input.append(el("option", { value: v }, label));
      }
      input.value = current ?? field.default ?? "";
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
            .slice(0, 16);
        }
      } else
        input.value =
          field.type === "csv"
            ? (current || []).join(", ")
            : (current ?? field.default ?? "");
    }
    if (field.required) input.required = true;
    if (field.readonly) input.disabled = true;
    if (field.min !== undefined) input.min = field.min;
    if (field.max !== undefined) input.max = field.max;
    input.setAttribute("aria-label", field.label);
    const label = el(
      "label",
      { class: field.wide ? "wide" : "" },
      field.label,
      input,
    );
    if (field.help) label.append(el("small", {}, field.help));
    area.append(label);
    inputs.push([field, input]);
  }
  form.append(
    area,
    row(
      button("取消", () => dialog.close()),
      el("button", { class: "primary", type: "submit" }, "保存"),
    ),
  );
  form.addEventListener(
    "submit",
    action(async (event) => {
      event.preventDefault();
      const result = structuredClone(value);
      for (const [field, input] of inputs) {
        if (field.readonly) continue;
        let v = input.value;
        switch (field.type) {
          case "bool":
            v = input.checked;
            break;
          case "number":
            if (v === "") {
              v = field.nullable ? null : 0;
            } else {
              v = Number(v);
              if (!Number.isSafeInteger(v))
                throw new Error(field.label + "需要安全范围内的整数");
            }
            break;
          case "json":
            v = JSON.parse(v);
            break;
          case "csv":
            v = v
              .split(",")
              .map((s) => s.trim())
              .filter(Boolean);
            break;
          case "date":
            v = v ? new Date(v).toISOString() : null;
            break;
        }
        setPath(result, field.key, v);
      }
      await save(result);
      if (dialog.contains(form)) dialog.close();
    }),
  );
  dialog.replaceChildren(el("h2", {}, title), form);
  dialog.showModal();
}
export function table(columns, items, actions) {
  const wrapper = el("div", { class: "data-table" }),
    search = el("input", {
      class: "search",
      placeholder: "搜索…",
      "aria-label": "搜索列表",
    }),
    body = el("div"),
    pager = el("div", { class: "pager" });
  let page = 0;
  function render() {
    const term = search.value.toLowerCase();
    const data = items.filter((item) =>
      columns.some((c) =>
        String(c.value ? c.value(item) : (item[c.key] ?? ""))
          .toLowerCase()
          .includes(term),
      ),
    );
    const pages = Math.max(1, Math.ceil(data.length / 20));
    page = Math.min(page, pages - 1);
    const t = el("table"),
      head = el("tr");
    for (const c of columns) head.append(el("th", {}, c.label));
    if (actions) head.append(el("th", {}, "操作"));
    t.append(el("thead", {}, head));
    const tbody = el("tbody");
    for (const item of data.slice(page * 20, (page + 1) * 20)) {
      const tr = el("tr");
      for (const c of columns) {
        const value = c.render
          ? c.render(item)
          : c.value
            ? c.value(item)
            : item[c.key];
        tr.append(el("td", {}, value ?? "—"));
      }
      if (actions)
        tr.append(el("td", {}, el("div", { class: "actions" }, actions(item))));
      tbody.append(tr);
    }
    t.append(tbody);
    body.replaceChildren(
      data.length
        ? el("div", { class: "table-wrap" }, t)
        : el("div", { class: "empty" }, "暂无记录"),
    );
    pager.replaceChildren(
      button("上一页", () => {
        page = Math.max(0, page - 1);
        render();
      }),
      el("span", {}, `${page + 1} / ${pages} · ${data.length} 项`),
      button("下一页", () => {
        page = Math.min(pages - 1, page + 1);
        render();
      }),
    );
  }
  search.addEventListener("input", () => {
    page = 0;
    render();
  });
  wrapper.append(search, body, pager);
  wrapper.updateItems = (next) => {
    items = next;
    render();
  };
  render();
  return wrapper;
}
export function resourceEditor({
  title,
  items,
  fields,
  defaults,
  change,
  columns,
  normalize = (v) => v,
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
