import { el, button } from "./dom.js";

const selected = new Map();
export function workspace(key, sections) {
  const navigation = el("div", {
    class: "workspace-tabs",
    role: "tablist",
    "aria-label": "工作区",
  });
  const root = el("div", { class: "workspace" }, navigation);
  const tabs = [],
    panels = [];
  function choose(index) {
    selected.set(key, index);
    tabs.forEach((tab, i) => {
      tab.setAttribute("aria-selected", String(i === index));
      tab.tabIndex = i === index ? 0 : -1;
      panels[i].hidden = i !== index;
    });
  }
  sections.forEach(([label, content], i) => {
    const tab = button(label, () => choose(i));
    tab.id = `${key}-tab-${i}`;
    tab.setAttribute("role", "tab");
    tab.setAttribute("aria-controls", `${key}-pane-${i}`);
    const panel = el(
      "section",
      { id: `${key}-pane-${i}`, role: "tabpanel", "aria-labelledby": tab.id },
      content,
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
    navigation.append(tab);
    root.append(panel);
  });
  choose(Math.min(selected.get(key) || 0, sections.length - 1));
  return root;
}
