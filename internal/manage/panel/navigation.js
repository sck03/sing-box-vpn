import { el, button } from "./dom.js";
import {
  groups,
  pageFor,
  canView,
  groupFor,
  groupLabel,
  searchPages,
} from "./page-catalog.js";

export function navigation(role, select, current) {
  const common = [],
    advanced = [];
  for (const group of groups) {
    const visible = group.pages.filter((id) => canView(id, role));
    if (!visible.length) continue;
    if (group.advanced) {
      for (const id of visible) {
        const item = button(pageFor(id, role).title, () => select(id));
        item.dataset.page = id;
        advanced.push(item);
      }
    } else {
      const item = button(groupLabel(group, role), () => select(visible[0]));
      item.dataset.page = visible[0];
      item.dataset.group = group.id;
      common.push(item);
    }
  }
  const items = [
    el("div", { class: "nav-items", "data-navigation": "common" }, common),
  ];
  if (!advanced.length) return items;
  const group = el(
    "details",
    { class: "nav-group", "data-navigation": "advanced" },
    el("summary", { class: "nav-label" }, "高级功能"),
    el("div", { class: "nav-items" }, advanced),
  );
  let saved;
  try {
    saved = window.localStorage.getItem("sbvpn-navigation:advanced");
  } catch {}
  group.open = !!groupFor(current)?.advanced || saved === "open";
  group.addEventListener("toggle", () => {
    try {
      window.localStorage.setItem(
        "sbvpn-navigation:advanced",
        group.open ? "open" : "closed",
      );
    } catch {}
  });
  return [...items, group];
}
export function markNavigation(root, page, reveal) {
  const group = groupFor(page);
  for (const item of root.querySelectorAll("button[data-page]")) {
    const active = item.dataset.group
      ? item.dataset.group === group.id
      : item.dataset.page === page;
    item.setAttribute("aria-current", active ? "page" : "false");
    const container = item.closest("details");
    if (container && active && reveal) container.open = true;
  }
}
export function pageTabs(root, current, role, select) {
  const group = groupFor(current),
    ids = group.advanced ? [] : group.pages.filter((id) => canView(id, role));
  root.hidden = ids.length < 2;
  if (root.hidden) {
    root.replaceChildren();
    delete root.dataset.group;
    return;
  }
  const identity = group.id + ":" + role;
  if (root.dataset.group !== identity) {
    root.dataset.group = identity;
    root.replaceChildren(
      ...ids.map((id, index) => {
        const tab = button(pageFor(id, role).title, () => select(id));
        tab.id = "page-tab-" + id;
        tab.dataset.page = id;
        tab.setAttribute("role", "tab");
        tab.setAttribute("aria-controls", "content");
        tab.addEventListener("keydown", (event) => {
          const next = {
            ArrowRight: (index + 1) % ids.length,
            ArrowLeft: (index + ids.length - 1) % ids.length,
            Home: 0,
            End: ids.length - 1,
          }[event.key];
          if (next !== undefined) {
            event.preventDefault();
            root.querySelectorAll("button")[next].focus();
            select(ids[next]);
          }
        });
        return tab;
      }),
    );
  }
  root.setAttribute("aria-label", group.label);
  for (const tab of root.querySelectorAll("button")) {
    const active = tab.dataset.page === current;
    tab.setAttribute("aria-selected", String(active));
    tab.tabIndex = active ? 0 : -1;
  }
}
export function featureSearch(input, results, role, select) {
  function clear() {
    input.value = "";
    results.replaceChildren();
    results.hidden = true;
  }
  input.oninput = () => {
    const matches = searchPages(input.value, role);
    results.hidden = !input.value.trim();
    results.replaceChildren(
      ...matches.map((page) =>
        button(
          el(
            "span",
            {},
            el("strong", {}, page.title),
            el(
              "small",
              {},
              [
                groupLabel(groupFor(page.id), role),
                pageFor(page.id, role).title,
              ]
                .filter(
                  (value, i, all) =>
                    value !== page.title && all.indexOf(value) === i,
                )
                .join(" → ") || page.description,
            ),
          ),
          () => {
            clear();
            select(page.route);
          },
        ),
      ),
    );
    if (!matches.length)
      results.append(
        el(
          "p",
          { role: "status" },
          "没有匹配功能，请试试节点、分享、证书或日志。",
        ),
      );
  };
  input.onkeydown = (event) => {
    if (event.key === "Escape") clear();
    if (event.key === "ArrowDown") {
      event.preventDefault();
      results.querySelector("button")?.focus();
    }
    if (event.key === "Enter") {
      event.preventDefault();
      results.querySelector("button")?.click();
    }
  };
  results.onkeydown = (event) => {
    const buttons = [...results.querySelectorAll("button")];
    const index = buttons.indexOf(event.target);
    if (event.key === "Escape") {
      clear();
      input.focus();
    } else if (index >= 0 && ["ArrowDown", "ArrowUp"].includes(event.key)) {
      event.preventDefault();
      (event.key === "ArrowUp"
        ? buttons[index - 1] || input
        : buttons[Math.min(index + 1, buttons.length - 1)]
      ).focus();
    }
  };
  return clear;
}
