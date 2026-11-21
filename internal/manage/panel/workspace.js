import { el, button } from "./dom.js";
import { sectionLoader } from "./sections.js";
import { sectionsFor } from "./page-catalog.js";

export function workspace(key, contentById, ctx) {
  const sections = sectionsFor(key, ctx.session.role);
  const navigation = el("div", {
    class: "workspace-tabs",
    role: "tablist",
    "aria-label": "工作区",
  });
  const root = el("div", { class: "workspace" }, navigation);
  const tabs = [],
    panels = [],
    loaders = [];
  let selected = 0;
  function choose(index) {
    selected = index;
    ctx.activeSection = sections[index]?.[0];
    tabs.forEach((tab, i) => {
      tab.setAttribute("aria-selected", String(i === index));
      tab.tabIndex = i === index ? 0 : -1;
      panels[i].hidden = i !== index;
    });
    ctx.sectionChanged?.(sections[index]?.[0]);
    return loaders[index]?.();
  }
  const activate = (i) =>
    ctx.navigate ? ctx.navigate(key + "/" + sections[i][0]) : choose(i);
  sections.forEach(([id, label], i) => {
    const content = contentById[id];
    const tab = button(label, () => activate(i));
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
        activate(next);
        tabs[next].focus();
      }
    });
    tabs.push(tab);
    panels.push(panel);
    loaders.push(
      typeof content === "function"
        ? sectionLoader(panel, content, ctx.signal)
        : null,
    );
    navigation.append(tab);
    root.append(panel);
  });
  ctx.showSection = (id) =>
    choose(
      Math.max(
        0,
        sections.findIndex(([key]) => key === id),
      ),
    );
  ctx.invalidateSections = () =>
    loaders.forEach((load, index) => {
      if (index !== selected) load?.invalidate();
    });
  ctx.showSection(ctx.section);
  return root;
}
