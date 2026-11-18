import { el, button, help, setDisabled, actionGroup } from "./dom.js";

export function table(columns, items, actions, options = {}) {
  const wrapper = el("div", { class: "data-table" }),
    search = el("input", {
      class: "search",
      placeholder: "搜索…",
      "aria-label": "搜索列表",
    }),
    body = el("div"),
    pager = el("div", { class: "pager" });
  let page = 0,
    signature = "",
    waitingForFocus = false,
    indexed = [];
  const scalar = (value) => (value instanceof Node ? value.textContent : value);
  const rowKey = (item) =>
    options.rowKey?.(item) ??
    item.id ??
    item.instance ??
    item.runtime ??
    item.name;
  const collator = new Intl.Collator(undefined, { numeric: true });
  const preferenceKey =
    "table:" + (options.key || columns.map((c) => c.key || c.label).join("|"));
  let preference = { sort: -1, descending: false, compact: false, hidden: [] };
  if (options.controls !== false) {
    try {
      preference = {
        ...preference,
        ...JSON.parse(window.localStorage.getItem(preferenceKey) || "{}"),
      };
    } catch {}
  }
  if (!Array.isArray(preference.hidden)) preference.hidden = [];
  function savePreference() {
    try {
      window.localStorage.setItem(preferenceKey, JSON.stringify(preference));
    } catch {}
  }
  const controls = el("div", { class: "table-tools" }, search);
  if (options.controls !== false) {
    const mobileSort = el(
      "select",
      { class: "mobile-sort", "aria-label": "列表排序" },
      el("option", { value: -1 }, "默认顺序"),
      columns.map((c, i) => el("option", { value: i }, c.label)),
    );
    mobileSort.value = preference.sort;
    mobileSort.addEventListener("change", () => {
      preference.sort = Number(mobileSort.value);
      page = 0;
      savePreference();
      render();
    });
    const mobileDirection = button(
      "升序 / 降序",
      () => {
        preference.descending = !preference.descending;
        savePreference();
        render();
      },
      "mobile-sort",
    );
    controls.append(mobileSort, mobileDirection);
    controls.append(
      button("切换密度", () => {
        preference.compact = !preference.compact;
        savePreference();
        render();
      }),
    );
    const choices = el(
      "details",
      { class: "column-picker" },
      el("summary", {}, "显示列"),
    );
    columns.forEach((c, i) => {
      const input = el("input", {
        type: "checkbox",
        "aria-label": "显示" + c.label,
      });
      input.checked = !preference.hidden.includes(i);
      input.addEventListener("change", () => {
        preference.hidden = columns
          .map((_, j) => j)
          .filter((j) =>
            j === i ? !input.checked : preference.hidden.includes(j),
          );
        if (preference.hidden.length === columns.length) {
          preference.hidden = preference.hidden.filter((j) => j !== i);
          input.checked = true;
        }
        savePreference();
        render();
      });
      choices.append(el("label", {}, input, c.label));
    });
    controls.append(choices);
  }
  function index() {
    indexed = items.map((item) => {
      const values = columns.map((c) =>
        scalar(
          c.value ? c.value(item) : c.key ? item[c.key] : c.render?.(item),
        ),
      );
      return {
        item,
        values,
        text: columns
          .map((c, i) => scalar(c.search ? c.search(item) : values[i]) ?? "")
          .join(" ")
          .toLocaleLowerCase(),
      };
    });
  }
  index();
  function render() {
    const term = search.value.trim().toLocaleLowerCase();
    const entries = indexed.filter((entry) => entry.text.includes(term));
    if (options.controls !== false && columns[preference.sort]) {
      entries.sort((a, b) => {
        const av = a.values[preference.sort],
          bv = b.values[preference.sort];
        const order =
          typeof av === "number" && typeof bv === "number"
            ? av - bv
            : collator.compare(String(av ?? ""), String(bv ?? ""));
        return preference.descending ? -order : order;
      });
    }
    const data = entries.map((entry) => entry.item);
    wrapper.classList.toggle("compact", !!preference.compact);
    const visible = columns
      .map((c, i) => ({ c, i }))
      .filter(({ i }) => !preference.hidden.includes(i));
    const pages = Math.max(1, Math.ceil(data.length / 20));
    page = Math.min(page, pages - 1);
    const t = el("table"),
      head = el("tr");
    for (const { c, i } of visible) {
      head.append(
        options.controls === false
          ? el("th", {}, c.label)
          : el(
              "th",
              {
                "aria-sort":
                  preference.sort === i
                    ? preference.descending
                      ? "descending"
                      : "ascending"
                    : "none",
              },
              button(
                c.label,
                () => {
                  preference.descending =
                    preference.sort === i && !preference.descending;
                  preference.sort = i;
                  page = 0;
                  savePreference();
                  render();
                },
                "column-sort",
              ),
            ),
      );
    }
    if (actions) head.append(el("th", {}, "操作"));
    t.append(el("thead", {}, head));
    const tbody = el("tbody");
    for (const item of data.slice(page * 20, (page + 1) * 20)) {
      const tr = el("tr");
      tr.rowItem = item;
      tr.rowKey = rowKey(item);
      tr.addEventListener(
        "click",
        (event) => {
          if (tr.stale && event.target.closest("button")) {
            event.preventDefault();
            event.stopImmediatePropagation();
          }
        },
        true,
      );
      for (const { c } of visible) {
        const value = c.render
          ? c.render(item)
          : c.value
            ? c.value(item)
            : item[c.key];
        tr.append(el("td", { "data-label": c.label }, value ?? "—"));
      }
      if (actions)
        tr.append(
          el("td", { "data-label": "操作" }, actionGroup(actions(item))),
        );
      tbody.append(tr);
    }
    t.append(tbody);
    body.replaceChildren(
      data.length
        ? el("div", { class: "table-wrap" }, t)
        : el(
            "div",
            { class: "empty", role: "status" },
            el("strong", {}, term ? "没有匹配的记录" : "暂无记录"),
            help(
              term
                ? "试试其他关键词，或清空搜索。"
                : "创建第一条记录后，将在这里显示。",
            ),
          ),
    );
    pager.replaceChildren(
      button("上一页", () => {
        page = Math.max(0, page - 1);
        render();
      }),
      el("span", {}, `${page + 1} / ${pages} · ${data.length} 项`),
      button("下一页", () => {
        page = Math.min(pages - 1, page + 1);
        render();
      }),
    );
    const buttons = pager.querySelectorAll("button");
    setDisabled(buttons[0], page === 0);
    setDisabled(buttons[1], page >= pages - 1);
  }
  search.addEventListener("input", () => {
    page = 0;
    render();
  });
  wrapper.append(controls, body, pager);
  wrapper.updateItems = (next) => {
    const serialized = JSON.stringify(next);
    items = next;
    if (serialized === signature) return;
    // Patch visible data immediately, preserving active controls and row order.
    // Reorder/filter and replace action closures once focus leaves the table.
    if (body.contains(document.activeElement)) {
      signature = "";
      const byKey = new Map(next.map((item) => [rowKey(item), item]));
      for (const tr of body.querySelectorAll("tbody tr")) {
        const item = tr.rowKey === undefined ? null : byKey.get(tr.rowKey);
        const actionVersion = options.actionVersion || ((v) => v);
        const changed =
          !item ||
          JSON.stringify(actionVersion(item)) !==
            JSON.stringify(actionVersion(tr.rowItem));
        tr.stale ||= changed;
        if (changed)
          tr.querySelectorAll("button").forEach((control) => {
            control.setAttribute("aria-disabled", "true");
            control.title = "数据已更新，请移开焦点以刷新操作";
          });
        if (!item) continue;
        const visible = columns.filter(
          (_, i) => !preference.hidden.includes(i),
        );
        visible.forEach((c, i) => {
          const cell = tr.children[i];
          if (cell.contains(document.activeElement)) return;
          const value = c.render
            ? c.render(item)
            : c.value
              ? c.value(item)
              : item[c.key];
          cell.replaceChildren(value ?? "—");
        });
      }
      if (!waitingForFocus) {
        waitingForFocus = true;
        body.addEventListener(
          "focusout",
          () =>
            queueMicrotask(() => {
              waitingForFocus = false;
              wrapper.updateItems(items);
            }),
          { once: true },
        );
      }
      return;
    }
    signature = serialized;
    index();
    render();
  };
  signature = JSON.stringify(items);
  render();
  return wrapper;
}
export async function remoteTable(columns, load, actions, options = {}) {
  const search = el("input", {
    class: "search",
    "aria-label": options.label || "搜索授权",
    placeholder: options.placeholder || "搜索 ID、权益或状态…",
  });
  const body = el("div"),
    pager = el("div", { class: "pager" }),
    errors = el("div", { role: "status" }),
    controls = el("div", { class: "table-tools" }, search),
    sort = el("select", { "aria-label": "排序字段" }),
    direction = el(
      "select",
      { "aria-label": "排序方向" },
      el("option", { value: "asc" }, "升序"),
      el("option", { value: "desc" }, "降序"),
    );
  if (options.sortOptions) {
    sort.append(
      ...options.sortOptions.map(([value, label]) =>
        el("option", { value }, label),
      ),
    );
    controls.append(sort, direction);
  }
  if (options.filter) controls.prepend(options.filter);
  const wrapper = el(
    "div",
    { class: "remote-table" },
    controls,
    errors,
    body,
    pager,
  );
  const view = table(columns, [], actions, {
    controls: false,
    actionVersion: options.actionVersion,
  });
  view.querySelector(".table-tools").remove();
  view.querySelector(".pager").remove();
  body.append(view);
  let offset = 0,
    generation = 0,
    timer,
    busy = false;
  const previous = button("上一页", () => {
    offset = Math.max(0, offset - 20);
    return refresh(true);
  });
  const next = button("下一页", () => {
    offset += 20;
    return refresh(true);
  });
  const count = el("span");
  pager.append(previous, count, next);
  async function refresh(force = false) {
    if (options.signal?.aborted || (busy && !force) || (timer && !force))
      return;
    const current = ++generation;
    busy = true;
    wrapper.setAttribute("aria-busy", "true");
    try {
      const params = { q: search.value, offset, limit: 20 };
      if (options.sortOptions)
        Object.assign(params, { sort: sort.value, direction: direction.value });
      const page = await load(params);
      if (!page || !Array.isArray(page.items) || !Number.isInteger(page.total))
        throw new Error("列表接口响应格式错误");
      if (current !== generation || options.signal?.aborted) return;
      if (offset > 0 && offset >= page.total) {
        offset = Math.max(0, Math.floor((page.total - 1) / 20) * 20);
        return refresh(true);
      }
      errors.replaceChildren();
      view.updateItems(page.items);
      wrapper.classList.toggle("show-onboarding", page.total === 0 && !search.value && (!options.filter || options.filter.value === "current"));
      setDisabled(previous, offset === 0);
      setDisabled(next, offset + 20 >= page.total);
      count.textContent = `${Math.floor(offset / 20) + 1} / ${Math.max(1, Math.ceil(page.total / 20))} · ${page.total} 项`;
    } catch (error) {
      if (current === generation && error.name !== "AbortError") {
        errors.replaceChildren(
          help(error.message),
          button("重试", () => refresh(true)),
        );
      }
    } finally {
      if (current === generation) {
        busy = false;
        wrapper.removeAttribute("aria-busy");
      }
    }
  }
  search.addEventListener("input", () => {
    clearTimeout(timer);
    ++generation;
    offset = 0;
    timer = setTimeout(() => {
      timer = null;
      void refresh(true);
    }, 180);
  });
  for (const field of [sort, direction, options.filter].filter(Boolean))
    field.addEventListener("change", () => {
      offset = 0;
      void refresh(true);
    });
  await refresh();
  wrapper.refresh = refresh;
  options.signal?.addEventListener(
    "abort",
    () => {
      clearTimeout(timer);
      generation++;
    },
    { once: true },
  );
  return wrapper;
}
