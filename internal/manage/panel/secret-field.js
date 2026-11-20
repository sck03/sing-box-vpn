import { el } from "./dom.js";

// Keep the same input and value when toggling, including autofill and validation.
export function secretField(
  input,
  label = input.getAttribute("aria-label") || "密码",
) {
  input.type = "password";
  input.spellcheck = false;
  input.autocapitalize = "off";
  input.autocomplete ||= "new-password";
  const toggle = el(
    "button",
    {
      type: "button",
      class: "secret-toggle",
      "aria-pressed": "false",
      "aria-label": "显示" + label,
    },
    "显示",
  );
  const setVisible = (visible) => {
    input.type = visible ? "text" : "password";
    toggle.textContent = visible ? "隐藏" : "显示";
    toggle.setAttribute("aria-pressed", String(visible));
    toggle.setAttribute("aria-label", (visible ? "隐藏" : "显示") + label);
  };
  toggle.addEventListener("click", () => setVisible(input.type === "password"));
  input.form?.addEventListener("reset", () => setVisible(false));
  return el("span", { class: "secret-field" }, input, toggle);
}
