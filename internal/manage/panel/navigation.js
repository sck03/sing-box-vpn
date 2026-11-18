import { el, button } from "./dom.js";
import {
  groups,
  pages,
  canView,
  groupFor,
  searchPages,
} from "./page-catalog.js";

const selected = new Map();
export function navigation(role, select, current) {
  selected.clear();
  const common = [],
    advanced = [];
  for (const group of groups) {
    const visible = group.pages.filter((id) => canView(id, role));
    if (!visible.length) continue;
    if (group.advanced) {
      for (const id of visible) {
        const item = button(pages.find((page) => page.id === id).title, () =>
          select(id),
        );
        item.dataset.page = id;
        advanced.push(item);
      }
    } else {
      const item = button(group.label, () =>
        select(selected.get(group.id) || visible[0]),
      );
      item.dataset.page = visible[0];
      item.dataset.group = group.id;
      common.push(item);
    }
  }
  return [
    ["common", "常用功能", common],
    ["advanced", "高级功能（可选）", advanced],
  ]
    .filter(([, , items]) => items.length)
    .map(([id, title, items]) => {
      const group = el(
        "details",
        { class: "nav-group", "data-navigation": id },
        el("summary", { class: "nav-label" }, title),
        el("div", { class: "nav-items" }, items),
      );
      let saved;
      try {
        saved = window.localStorage.getItem("sbvpn-navigation:" + id);
      } catch {}
      group.open =
        saved === "open" ||
        (id === "common" && saved !== "closed") ||
        (id === "advanced" && groupFor(current)?.advanced);
      group.addEventListener("toggle", () => {
        try {
          window.localStorage.setItem(
            "sbvpn-navigation:" + id,
            group.open ? "open" : "closed",
          );
        } catch {}
      });
      return group;
    });
}
export function markNavigation(root, page, reveal) {
  const group = groupFor(page);
  selected.set(group.id, page);
  for (const item of root.querySelectorAll("button[data-page]")) {
    const active = item.dataset.group
      ? item.dataset.group === group.id
      : item.dataset.page === page;
    item.setAttribute("aria-current", active ? "page" : "false");
    if (active && reveal) item.closest("details").open = true;
  }
}
export function pageTabs(root, current, role, select) {
  const group = groupFor(current),
    ids = group.pages.filter((id) => canView(id, role));
  root.hidden = ids.length < 2;
  const identity = group.id + ":" + role;
  if (root.dataset.group !== identity) {
    root.dataset.group = identity;
    root.replaceChildren(
      ...ids.map((id, index) => {
        const tab = button(pages.find((page) => page.id === id).title, () =>
          select(id),
        );
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
            el("small", {}, groupFor(page.id).label + " · " + page.description),
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
  return clear;
}
