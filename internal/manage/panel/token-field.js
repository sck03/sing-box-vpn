import { el } from "./dom.js";
import { listValues } from "./form-values.js";

const split = (text) =>
  text
    .split(/[,，\r\n]+/)
    .map((part) => part.trim())
    .filter(Boolean);

// Free-form multi-value fields share parsing, de-duplication and keyboard
// editing. Domain-specific validation remains in the authoritative backend.
export function tokenField(field, initial, changed) {
  let values = listValues(initial),
    disabled = false;
  const input = el("input", {
    type: "text",
    placeholder: "输入后按 Enter，可粘贴逗号或换行分隔的多项",
    "aria-label": field.label,
  });
  const tags = el("div", { class: "token-values" });
  const root = el("div", { class: "token-control" }, tags, input);
  const value = () => [...new Set([...values, ...split(input.value)])];
  function render() {
    tags.replaceChildren(
      ...values.map((item) =>
        el(
          "span",
          { class: "token" },
          el("span", {}, item),
          el(
            "button",
            {
              type: "button",
              disabled,
              "aria-label": `移除 ${item}`,
              onclick: () => {
                values = values.filter((v) => v !== item);
                render();
                changed();
                input.focus();
              },
            },
            "×",
          ),
        ),
      ),
    );
  }
  function commit() {
    if (!input.value.trim()) return;
    values = value();
    input.value = "";
    render();
    changed();
  }
  input.addEventListener("keydown", (event) => {
    if (event.key === "Enter" || event.key === ",") {
      event.preventDefault();
      commit();
    }
    if (event.key === "Backspace" && !input.value && values.length) {
      input.value = values.pop();
      render();
      changed();
    }
  });
  input.addEventListener("paste", (event) => {
    const text = event.clipboardData?.getData("text");
    if (text && /[,，\r\n]/.test(text)) {
      event.preventDefault();
      values = [...new Set([...value(), ...split(text)])];
      input.value = "";
      render();
      changed();
    }
  });
  input.addEventListener("change", (event) => {
    event.stopPropagation();
    commit();
  });
  input.addEventListener("blur", commit);
  render();
  return {
    root,
    input,
    value,
    setValue(next) {
      values = listValues(next);
      input.value = "";
      render();
    },
    disable(next) {
      disabled = next;
      input.disabled = next;
      for (const button of tags.querySelectorAll("button"))
        button.disabled = next;
    },
    validate(required) {
      if (required && !value().length) throw new Error(`请填写${field.label}`);
      if (field.type === "numbers") {
        const invalid = value().filter(
          (item) =>
            !/^\d+$/.test(item) ||
            !Number.isSafeInteger(Number(item)) ||
            Number(item) < 1 ||
            Number(item) > 4294967295,
        );
        if (invalid.length)
          throw new Error(
            `${field.label}包含无效整数：${invalid.join("、")}（允许 1–4294967295）`,
          );
      }
    },
  };
}
