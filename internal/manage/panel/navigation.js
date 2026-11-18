import { el, button } from "./dom.js";

export function navigation(groups, pages, canView, role, select, current) {
  return groups.flatMap(([title, ids]) => {
    const visible = ids.map((id) => pages.find((p) => p[0] === id))
      .filter((p) => p && canView(p[0], role));
    if (!visible.length) return [];
    const group = el("details", { class: "nav-group" },
      el("summary", { class: "nav-label" }, title),
      el("div", { class: "nav-items" }, ...visible.map(([id, label]) => {
        const item = button(label, () => select(id));
        item.dataset.page = id;
        return item;
      })),
    );
    const key = "sbvpn-navigation:" + title;
    let saved;
    try { saved = window.localStorage.getItem(key); } catch {}
    group.open = saved !== "closed" || ids.includes(current);
    group.addEventListener("toggle", () => {
      try { window.localStorage.setItem(key, group.open ? "open" : "closed"); } catch {}
    });
    return [group];
  });
}

export function markNavigation(root, page, reveal) {
  for (const item of root.querySelectorAll("button[data-page]")) {
    const active = item.dataset.page === page;
    item.setAttribute("aria-current", active ? "page" : "false");
    if (active && reveal) item.closest("details").open = true;
  }
}
