import { api, setCSRF } from "./api.js";
import { overviewStream } from "./live.js";
import { $, el, card, help, notice, action, button, showResult } from "./ui.js";
import {
  inbounds,
  routing,
  outbounds,
  vpngate,
  certificates,
} from "./network.js";
import { accounts, subscribers, grants } from "./accounts.js";
import { subscriptions } from "./subscriptions.js";
import { overview, maintenance, admins } from "./maintenance.js";
const pages = [
  ["overview", "运行概览", overview],
  ["inbounds", "入站与发布", inbounds],
  ["accounts", "权益与用量", accounts],
  ["subscribers", "订阅者", subscribers],
  ["grants", "下载授权", grants],
  ["subscriptions", "订阅与分发", subscriptions],
  ["routing", "路由与 DNS", routing],
  ["outbounds", "出站与隧道", outbounds],
  ["vpngate", "VPNGate", vpngate],
  ["certificates", "证书资产", certificates],
  ["maintenance", "维护与任务", maintenance],
  ["admins", "管理员安全", admins],
];
const navigationGroups = [
  ["工作台", ["overview"]],
  ["用户与订阅", ["subscribers", "accounts", "grants", "subscriptions"]],
  ["网络与节点", ["inbounds", "routing", "outbounds", "vpngate"]],
  ["系统管理", ["certificates", "maintenance", "admins"]],
];
const canView = (id, role) =>
  role === "admin" ||
  ["overview", "accounts", "subscribers"].includes(id) ||
  (role === "operator" && id === "maintenance");
const setMenu = (open) => {
  $("#shell").classList.toggle("nav-open", open);
  $("#menu-toggle").setAttribute("aria-expanded", String(open));
  $("#menu-toggle").textContent = open ? "收起导航" : "打开导航";
};
$("#menu-toggle").addEventListener("click", () =>
  setMenu(!$("#shell").classList.contains("nav-open")),
);
$("#theme-toggle").addEventListener("click", () => {
  const theme =
    document.documentElement.dataset.theme === "light" ? "dark" : "light";
  document.documentElement.dataset.theme = theme;
  try {
    window.localStorage.setItem("panel-theme", theme);
  } catch {}
});
try {
  document.documentElement.dataset.theme =
    window.localStorage.getItem("panel-theme") ||
    (window.matchMedia?.("(prefers-color-scheme: light)").matches
      ? "light"
      : "dark");
} catch {}
let page = "overview",
  draft = null,
  original = null,
  revision = 0,
  draftGeneration = 0,
  configLoad = null,
  loading = 0,
  pageRequest;
const ctx = {
  session: null,
  root: "",
  dirty: false,
  async config() {
    if (!draft) {
      const generation = draftGeneration;
      configLoad ??= api(
        ctx.session.role === "admin" ? "/api/config" : "/api/state",
      )
        .then((response) => {
          if (generation !== draftGeneration)
            throw new DOMException("草稿已重新载入", "AbortError");
          draft = response.config;
          original = structuredClone(draft);
          revision = response.revision;
          ctx.root = response.root;
          return draft;
        })
        .finally(() => {
          if (generation === draftGeneration) configLoad = null;
        });
      return configLoad;
    }
    return draft;
  },
  replaceConfig(c) {
    draft = c;
  },
  changed() {
    ctx.dirty = true;
    $("#draft").hidden = false;
    $("#draft-message").textContent = "配置有未保存的更改 · 草稿保留在本页";
  },
  observeRevision(value) {
    if (draft && value > revision) {
      $("#draft").hidden = false;
      $("#draft-message").textContent =
        "服务器已有新的配置版本，请核对草稿后重新载入。";
    }
  },
  async task(command) {
    const response = await api("/api/tasks", command);
    notice("任务 #" + response.task_id + " 已排队，可在运行概览查看结果");
  },
  render: renderPage,
  logoutLocal() {
    ++loading;
    pageRequest?.abort();
    ctx.session = null;
    resetDraft();
    ctx.overview = null;
    ctx.updateOverview = null;
    $("#task-status").hidden = true;
    setCSRF("");
    stream.stop();
    $("#shell").hidden = true;
    $("#login").hidden = false;
    $("#login-form").reset();
    $("#content").replaceChildren();
    $("#dialog").replaceChildren();
    $("#dialog").close();
  },
};
function resetDraft() {
  draftGeneration++;
  draft = null;
  original = null;
  configLoad = null;
  ctx.dirty = false;
  $("#draft").hidden = true;
}
async function renderPage() {
  if (!ctx.session) return;
  pageRequest?.abort();
  pageRequest = new AbortController();
  const signal = pageRequest.signal;
  const generation = ++loading;
  ctx.updateOverview = null;
  const definition = pages.find((v) => v[0] === page) || pages[0];
  $("#page-title").textContent = definition[1];
  document.title = definition[1] + " · sing-box-vpn";
  for (const item of $("#navigation").querySelectorAll("button"))
    item.setAttribute(
      "aria-current",
      item.dataset.page === page ? "page" : "false",
    );
  try {
    $("#content").setAttribute("aria-busy", "true");
    // Each render owns its callback; a late render cannot replace the live one.
    const view = Object.create(ctx);
    view.signal = signal;
    view.request = (path, body) => api(path, body, { signal });
    const content = await definition[2](view);
    if (generation === loading) ctx.updateOverview = view.updateOverview;
    if (generation !== loading) return;
    $("#content").replaceChildren(...content);
    schedule();
  } catch (error) {
    if (generation !== loading || !ctx.session || error.name === "AbortError")
      return;
    if (error.status === 401) {
      ctx.logoutLocal();
      notice("会话已失效，请重新登录", true);
    } else {
      notice(error.message, true);
      if (generation === loading)
        $("#content").replaceChildren(
          card("无法载入", help(error.message), button("重试", renderPage)),
        );
    }
  } finally {
    if (generation === loading) $("#content").removeAttribute("aria-busy");
  }
}
const stream = overviewStream({
  request: api,
  unauthorized: () => ctx.logoutLocal(),
  disconnected: () => {
    $("#task-status").hidden = false;
    $("#task-status").textContent = "实时连接中断，正在重连";
  },
  snapshot(data) {
    ctx.overview = data;
    ctx.observeRevision(ctx.overview.revision);
    const active = ctx.overview.tasks.filter((task) =>
      ["queued", "running"].includes(task.state),
    );
    const failed = ctx.overview.tasks.filter(
      (task) =>
        task.state === "failed" ||
        ["partial", "cached", "failed"].includes(task.outcome),
    );
    const status = $("#task-status");
    status.hidden = active.length === 0 && failed.length === 0;
    status.href = ctx.session.role === "viewer" ? "#overview" : "#maintenance";
    status.textContent = active.length
      ? `${active.length} 个任务正在处理 · 查看进度`
      : `${failed.length} 个任务需要关注 · 查看详情`;
    status.classList.toggle("error", !active.length && failed.length > 0);
    ctx.updateOverview?.(ctx.overview);
  },
});
function schedule() {
  stream.setActive(!!ctx.session && !document.hidden);
}
async function connected(session) {
  ctx.session = session;
  setCSRF(session.csrf);
  $("#login").hidden = true;
  $("#shell").hidden = false;
  $("#identity").textContent = session.id + " · " + session.role;
  $("#navigation").replaceChildren(
    ...navigationGroups
      .map(([title, ids]) => {
        const visible = ids
          .map((id) => pages.find((p) => p[0] === id))
          .filter((p) => canView(p[0], session.role));
        if (!visible.length) return null;
        return el(
          "section",
          { class: "nav-group", "aria-label": title },
          el("span", { class: "nav-label" }, title),
          ...visible.map(([id, label]) => {
            const b = button(label, () => {
              page = id;
              location.hash = id;
              setMenu(false);
              renderPage();
            });
            b.dataset.page = id;
            return b;
          }),
        );
      })
      .filter(Boolean),
  );
  page = location.hash.slice(1) || "overview";
  if (!pages.some((p) => p[0] === page) || !canView(page, session.role))
    page = "overview";
  await renderPage();
}
$("#login-form").addEventListener(
  "submit",
  action(async (event) => {
    event.preventDefault();
    $("#login-error").textContent = "";
    const values = Object.fromEntries(new FormData(event.target));
    try {
      await connected(await api("/api/auth/login", values));
      event.target.reset();
    } catch (error) {
      $("#login-error").textContent = error.message;
    }
  }),
);
$("#logout").addEventListener(
  "click",
  action(async () => {
    await api("/api/auth/logout", {});
    ctx.logoutLocal();
  }),
);
$("#refresh").addEventListener(
  "click",
  action(() => {
    ctx.overview = null;
    return renderPage();
  }),
);
$("#preview").addEventListener(
  "click",
  action(async () => {
    if (!draft) throw new Error("没有可校验的配置");
    const submitted = structuredClone(draft);
    const baseline = original;
    const generation = draftGeneration;
    const result = await api("/api/config/preview", submitted);
    if (generation !== draftGeneration) return;
    const changed = [];
    function diff(a, b, path = "") {
      if (JSON.stringify(a) === JSON.stringify(b)) return;
      if (
        a &&
        b &&
        typeof a === "object" &&
        typeof b === "object" &&
        !Array.isArray(a) &&
        !Array.isArray(b)
      ) {
        for (const key of new Set([...Object.keys(a), ...Object.keys(b)]))
          diff(a[key], b[key], path + "/" + key);
      } else changed.push(path);
    }
    diff(baseline, submitted);
    showResult("配置校验与变更字段", { changed, ...result });
  }),
);
$("#discard").addEventListener(
  "click",
  action(async () => {
    if (ctx.dirty && !confirm("丢弃未保存的配置草稿？")) return;
    resetDraft();
    $("#dialog").close();
    await renderPage();
  }),
);
$("#save").addEventListener(
  "click",
  action(async () => {
    if (!draft) throw new Error("没有可保存的配置");
    const submitted = structuredClone(draft);
    const submittedRevision = revision;
    const generation = draftGeneration;
    const response = await api("/api/config", {
      revision: submittedRevision,
      config: submitted,
    });
    if (generation !== draftGeneration) return;
    revision = submittedRevision + 1;
    original = submitted;
    ctx.dirty = JSON.stringify(draft) !== JSON.stringify(original);
    $("#draft").hidden = !ctx.dirty;
    if (ctx.dirty) ctx.changed();
    notice(
      "校验通过，应用任务 #" +
        response.task_id +
        " 已排队。请查看实际应用结果。",
    );
  }),
);
window.addEventListener("beforeunload", (event) => {
  if (ctx.dirty) {
    event.preventDefault();
    event.returnValue = "";
  }
});
document.addEventListener("visibilitychange", () => {
  schedule();
});
window.addEventListener("hashchange", () => {
  const next = location.hash.slice(1);
  if (next && next !== page) {
    page =
      ctx.session &&
      pages.some((p) => p[0] === next) &&
      canView(next, ctx.session.role)
        ? next
        : "overview";
    setMenu(false);
    renderPage();
  }
});
api("/api/auth/session")
  .then(connected)
  .catch((error) => {
    if (error.name !== "AbortError") ctx.logoutLocal();
  });
