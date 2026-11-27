import { $, el, action, button, row, getPath, setPath } from "./dom.js";
import { errorMessage } from "./api.js";
import { secretField } from "./secret-field.js";
import { resolverValue } from "./form-values.js";
import { choiceField } from "./choice-field.js";
import { tokenField } from "./token-field.js";
import { dialogSession } from "./dialog-session.js";

export function formDialog(
  title,
  fields,
  value,
  save,
  preserve = false,
  signal = undefined,
) {
  const session = dialogSession(signal);
  const dialog = $("#dialog"),
    form = el("form"),
    area = el("div", { class: "fields" });
  const inputs = [];
  const controls = new Map();
  const optionSnapshots = new WeakMap();
  const dependencies = new Map();
  const fieldSnapshot = (input) =>
    controls.has(input)
      ? JSON.stringify(controls.get(input).value())
      : input.type === "checkbox"
        ? input.checked
        : JSON.stringify(input.value);
  const advanced = el("div", { class: "fields" });
  for (const field of fields) {
    const target = field.advanced ? advanced : area;
    if (field.section)
      target.append(el("h3", { class: "field-section wide" }, field.section));
    const raw = getPath(value, field.key);
    const current = field.scale && raw != null ? raw / field.scale : raw;
    let input;
    let suggestions;
    let control;
    if (["multiselect", "csv", "numbers"].includes(field.type)) {
      const factory = field.type === "multiselect" ? choiceField : tokenField;
      control = factory(field, current ?? field.initial?.(value) ?? [], () => {
        control.root.dispatchEvent(
          new window.Event("change", { bubbles: true }),
        );
      });
      input = control.input;
      controls.set(input, control);
    } else if (["select", "tristate"].includes(field.type)) {
      input = el("select");
      const options =
        typeof field.options === "function"
          ? field.options(value)
          : field.options;
      for (const option of options || []) {
        const [v, label] = Array.isArray(option) ? option : [option, option];
        input.append(el("option", { value: v }, label));
      }
      for (const existing of [current]) {
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
        input.value =
          field.type === "resolver" && current && typeof current === "object"
            ? JSON.stringify(current)
            : (current ?? field.default ?? "");
    }
    if (field.type === "resolver" && field.options) {
      const id = `resolver-${inputs.length}`;
      input.setAttribute("list", id);
      suggestions = el("datalist", { id });
    }
    if (field.required && !control) input.required = true;
    if (field.readonly) input.disabled = true;
    if (field.min !== undefined) input.min = field.min;
    if (field.max !== undefined) input.max = field.max;
    if (field.scale) input.step = "any";
    input.setAttribute("aria-label", field.label);
    const label = el(
      control ? "div" : "label",
      { class: "form-field" + (field.wide ? " wide" : "") },
      el("span", { class: "field-label" }, field.label),
      control
        ? control.root
        : field.type === "password"
          ? secretField(input, field.label)
          : input,
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
            : controls.has(input)
              ? controls.get(input).value()
              : input.value,
      );
    }
    return current;
  };
  const refreshFields = () => {
    let current = formValue();
    for (const [field, input] of inputs) {
      const control = controls.get(input);
      if (field.resetOn) {
        const signature = JSON.stringify(
          field.resetOn.map((key) => getPath(current, key)),
        );
        if (
          dependencies.has(field.key) &&
          dependencies.get(field.key) !== signature
        ) {
          control.setValue(field.initial?.(current) || []);
        }
        dependencies.set(field.key, signature);
      }
      if (control?.update) {
        control.update(
          typeof field.options === "function"
            ? field.options(current)
            : field.options,
          field.filter?.(current),
          field.loading?.(current),
        );
        setPath(current, field.key, control.value());
      } else if (field.type === "resolver" && field.options) {
        const options =
          typeof field.options === "function"
            ? field.options(current)
            : field.options;
        if (optionSnapshots.get(input) !== JSON.stringify(options))
          input
            .closest(".form-field")
            .querySelector("datalist")
            .replaceChildren(
              ...options.map((option) => {
                const [value, label] = Array.isArray(option)
                  ? option
                  : [option, option];
                return el("option", { value }, label);
              }),
            );
        optionSnapshots.set(input, JSON.stringify(options));
      } else if (typeof field.options === "function") {
        const previous = input.value;
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
        const snapshot = JSON.stringify(options);
        if (optionSnapshots.get(input) !== snapshot)
          input.replaceChildren(
            ...options.map((option) => {
              const [v, label] = Array.isArray(option)
                ? option
                : [option, option];
              return el("option", { value: v }, label);
            }),
          );
        optionSnapshots.set(input, snapshot);
        if ([...input.options].some((o) => o.value === previous))
          input.value = previous;
        setPath(current, field.key, input.value);
      }
      const visible = !field.visible || field.visible(current);
      const required =
        typeof field.required === "function"
          ? field.required(current)
          : !!field.required;
      if (!control) input.required = required;
      input.closest(".form-field").hidden = !visible;
      input.disabled =
        !!field.readonly || !visible || !!field.loading?.(current);
      control?.disable(input.disabled);
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
      button("取消", session.close),
      el("button", { class: "primary", type: "submit" }, "保存"),
    ),
  );
  form.addEventListener(
    "submit",
    action(async (event) => {
      event.preventDefault();
      const errorNode = form.querySelector(".form-error");
      errorNode.hidden = true;
      for (const [, input] of inputs) {
        input.removeAttribute("aria-invalid");
        controls.get(input)?.root.removeAttribute("aria-invalid");
      }
      try {
        const result = structuredClone(value);
        for (const [field, input, initial] of inputs) {
          if (field.readonly || input.closest(".form-field").hidden) continue;
          const original = getPath(value, field.key);
          const control = controls.get(input);
          if (control) {
            try {
              control.validate(
                typeof field.required === "function"
                  ? field.required(formValue())
                  : !!field.required,
              );
            } catch (error) {
              error.field = field.key;
              throw error;
            }
          }
          const structured = original !== null && typeof original === "object";
          if (
            (field.type === "date" ||
              ((preserve || structured) &&
                (!field.visible || field.visible(value)))) &&
            fieldSnapshot(input) === initial
          )
            continue;
          let v = input.value;
          if (
            field.forbidden &&
            [...field.forbidden].some((char) => v.includes(char))
          )
            throw Object.assign(
              new Error(`${field.label}不能包含换行、回车或空字符。`),
              { field: field.key },
            );
          if (field.minBytes !== undefined || field.maxBytes !== undefined) {
            const length = new TextEncoder().encode(v).length;
            if (
              (v || field.required) &&
              (length < (field.minBytes || 0) ||
                length > (field.maxBytes ?? Infinity))
            )
              throw Object.assign(
                new Error(
                  `${field.label}需要 ${field.minBytes || 0}–${field.maxBytes} 个 UTF-8 字节，当前为 ${length}。`,
                ),
                { field: field.key },
              );
          }
          switch (field.type) {
            case "multiselect":
              v = control.value();
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
              v = control.value();
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
        await save(result, session);
      } catch (error) {
        if (!session.active() || error.name === "AbortError") return;
        errorNode.textContent = errorMessage(error);
        errorNode.hidden = false;
        const match = inputs.find(
          ([field]) =>
            error.field === field.key || error.field?.endsWith("." + field.key),
        );
        if (match) {
          const input = match[1];
          input.setAttribute("aria-invalid", "true");
          const control = controls.get(input);
          control?.root.setAttribute("aria-invalid", "true");
          for (
            let parent = input.parentElement;
            parent && parent !== form;
            parent = parent.parentElement
          )
            if (parent.tagName === "DETAILS") parent.open = true;
          if (control?.focus) control.focus();
          else input.focus();
        }
        return;
      }
      if (session.active()) session.close();
    }),
  );
  session.replace(el("h2", {}, title), form);
  return {
    form,
    dialog,
    value: formValue,
    refresh: refreshFields,
    input: (key) => inputs.find(([field]) => field.key === key)?.[1],
    setValue: (key, next) => {
      const input = inputs.find(([field]) => field.key === key)?.[1];
      if (controls.has(input)) controls.get(input).setValue(next);
      else if (input) input.value = next;
    },
    active: session.active,
    signal: session.signal,
  };
}
