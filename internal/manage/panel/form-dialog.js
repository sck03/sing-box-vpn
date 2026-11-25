import { $, el, action, button, row, getPath, setPath } from "./dom.js";
import { errorMessage } from "./api.js";
import { secretField } from "./secret-field.js";
import { resolverValue } from "./form-values.js";

function selection(input) {
  return input.multiple
    ? [...input.selectedOptions].map((o) => o.value)
    : input.value;
}
function fieldSnapshot(input) {
  return input.type === "checkbox"
    ? input.checked
    : JSON.stringify(selection(input));
}
export function formDialog(title, fields, value, save, preserve = false) {
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
    let suggestions;
    if (["select", "multiselect", "tristate"].includes(field.type)) {
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
      const selected = input.multiple ? current || [] : [current];
      for (const existing of selected) {
        if (
          existing != null &&
          ![...input.options].some((o) => o.value === String(existing))
        )
          input.append(
            el(
              "option",
              { value: existing },
              typeof existing === "object"
                ? "高级配置（保留原值）"
                : `${existing}（当前值）`,
            ),
          );
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
                : field.type === "resolver"
                  ? "text"
                  : field.type || "text",
      });
      if (field.type === "bool") input.checked = !!(current ?? field.default);
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
          : field.type === "resolver" && current && typeof current === "object"
            ? JSON.stringify(current)
            : (current ?? field.default ?? "");
    }
    if (field.type === "resolver" && field.options) {
      const id = `resolver-${inputs.length}`;
      input.setAttribute("list", id);
      suggestions = el("datalist", { id });
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
      field.type === "password" ? secretField(input, field.label) : input,
    );
    if (field.help) label.append(el("small", {}, field.help));
    if (suggestions) label.append(suggestions);
    target.append(label);
    inputs.push([field, input, fieldSnapshot(input)]);
  }
  const formValue = () => {
    const current = structuredClone(value);
    for (const [field, input, initial] of inputs) {
      if (
        fieldSnapshot(input) === initial &&
        getPath(value, field.key) !== undefined
      )
        continue;
      setPath(
        current,
        field.key,
        field.type === "bool"
          ? input.checked
          : field.type === "tristate"
            ? input.value === ""
              ? undefined
              : input.value === "true"
            : field.type === "multiselect"
              ? [...input.selectedOptions].map((o) => o.value)
              : input.value,
      );
    }
    return current;
  };
  const refreshFields = () => {
    let current = formValue();
    for (const [field, input] of inputs) {
      if (field.type === "resolver" && field.options) {
        const options =
          typeof field.options === "function"
            ? field.options(current)
            : field.options;
        input
          .closest("label")
          .querySelector("datalist")
          .replaceChildren(
            ...options.map((option) => {
              const [value, label] = Array.isArray(option)
                ? option
                : [option, option];
              return el("option", { value }, label);
            }),
          );
      } else if (typeof field.options === "function") {
        const previous = input.multiple
          ? [...input.selectedOptions].map((o) => o.value)
          : input.value;
        const options = [...field.options(current)];
        // Keep structured values that have no simple option; scalar dependencies
        // still remove incompatible choices when another field changes.
        for (const selected of input.selectedOptions) {
          const original = getPath(value, field.key);
          if (
            !original ||
            typeof original !== "object" ||
            Array.isArray(original) ||
            selected.value !== String(original)
          )
            continue;
          if (
            !options.some(
              (option) =>
                String(Array.isArray(option) ? option[0] : option) ===
                selected.value,
            )
          )
            options.push([selected.value, selected.textContent]);
        }
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
      for (const [, input] of inputs) input.removeAttribute("aria-invalid");
      try {
        const result = structuredClone(value);
        for (const [field, input, initial] of inputs) {
          if (field.readonly || input.disabled) continue;
          const original = getPath(value, field.key);
          const structured = original !== null && typeof original === "object";
          if (
            (field.type === "date" ||
              ((preserve || structured) &&
                (!field.visible || field.visible(value)))) &&
            fieldSnapshot(input) === initial
          )
            continue;
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
            case "tristate":
              v = v === "" ? undefined : v === "true";
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
            case "resolver":
              try {
                v = resolverValue(v);
              } catch (error) {
                error.field = field.key;
                throw error;
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
        errorNode.textContent = errorMessage(error);
        errorNode.hidden = false;
        const match = inputs.find(
          ([field]) =>
            error.field === field.key || error.field?.endsWith("." + field.key),
        );
        if (match) {
          const input = match[1];
          input.setAttribute("aria-invalid", "true");
          for (
            let parent = input.parentElement;
            parent && parent !== form;
            parent = parent.parentElement
          )
            if (parent.tagName === "DETAILS") parent.open = true;
          input.focus();
        }
        return;
      }
      if (dialog.contains(form)) dialog.close();
    }),
  );
  dialog.replaceChildren(el("h2", {}, title), form);
  dialog.showModal();
  return {
    form,
    dialog,
    value: formValue,
    refresh: refreshFields,
    input: (key) => inputs.find(([field]) => field.key === key)?.[1],
    active: () => dialog.open && dialog.contains(form),
  };
}
