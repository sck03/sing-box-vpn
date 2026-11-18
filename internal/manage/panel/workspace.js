import { el, button } from "./dom.js";
import { sectionLoader } from "./sections.js";

const selected = new Map();
export function workspace(key, sections, { signal, active } = {}) {
  const navigation = el("div", {
    class: "workspace-tabs",
    role: "tablist",
    "aria-label": "工作区",
  });
  const root = el("div", { class: "workspace" }, navigation);
  const tabs = [],
    panels = [],
    loaders = [];
  function choose(index) {
    selected.set(key, index);
    tabs.forEach((tab, i) => {
      tab.setAttribute("aria-selected", String(i === index));
      tab.tabIndex = i === index ? 0 : -1;
      panels[i].hidden = i !== index;
    });
    return loaders[index]?.();
  }
  sections.forEach(([label, content], i) => {
    const tab = button(label, () => choose(i));
    tab.id = `${key}-tab-${i}`;
    tab.setAttribute("role", "tab");
    tab.setAttribute("aria-controls", `${key}-pane-${i}`);
    const panel = el(
      "section",
      { id: `${key}-pane-${i}`, role: "tabpanel", "aria-labelledby": tab.id },
      typeof content === "function" ? null : content,
    );
    tab.addEventListener("keydown", (event) => {
      const next = {
        ArrowRight: (i + 1) % sections.length,
        ArrowLeft: (i + sections.length - 1) % sections.length,
        Home: 0,
        End: sections.length - 1,
      }[event.key];
      if (next !== undefined) {
        event.preventDefault();
        choose(next);
        tabs[next].focus();
      }
    });
    tabs.push(tab);
    panels.push(panel);
    loaders.push(
      typeof content === "function"
        ? sectionLoader(panel, content, signal)
        : null,
    );
    navigation.append(tab);
    root.append(panel);
  });
  const requested = active
    ? sections.findIndex((section) => section[2] === active)
    : -1;
  choose(
    requested >= 0
      ? requested
      : Math.min(selected.get(key) || 0, sections.length - 1),
  );
  return root;
}
