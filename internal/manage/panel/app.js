import { api, setCSRF } from "./api.js";
import { overviewStream } from "./live.js";
import { pageContext } from "./lifecycle.js";
import { capabilities, validatePassword } from "./capabilities.js";
import { showConfiguration } from "./config-preview.js";
import { navigation, markNavigation } from "./navigation.js";
import { $, el, card, help, notice, action, button, showResult } from "./ui.js";
const lazy = (module, name) => async (context) =>
  (await import(module))[name](context);
const inbounds = lazy("./network.js", "inbounds");
const certificates = lazy("./network.js", "certificates");
const routing = lazy("./routing.js", "routing");
const outbounds = lazy("./outbounds.js", "outbounds");
const vpngate = lazy("./vpngate.js", "vpngate");
const accounts = lazy("./accounts.js", "accounts");
const access = lazy("./access.js", "access");
const subscriptions = lazy("./subscriptions.js", "subscriptions");
const overview = lazy("./overview.js", "overview");
const maintenance = lazy("./maintenance.js", "maintenance");
const admins = lazy("./admins.js", "admins");
const pages = [
  ["overview", "运行概览", overview],
  ["inbounds", "入站与发布", inbounds],
  ["accounts", "权益与用量", accounts],
  ["access", "公网访问", access],
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
  ["用户与订阅", ["accounts", "subscriptions"]],
  ["网络与节点", ["inbounds", "routing", "outbounds", "vpngate"]],
  ["系统管理", ["access", "certificates", "maintenance", "admins"]],
];
const descriptions = {
  overview: "查看服务健康、配置同步与最近任务。",
  inbounds: "配置用户连接的入口和对外发布地址。",
  accounts: "管理额度、有效期与节点授权。",
  access: "配置 HTTPS 面板入口与对外订阅地址。",
  subscriptions: "组合节点来源，生成客户端配置并自动分发。",
  routing: "设置流量去向与域名解析规则。",
  outbounds: "管理外部代理、网络检测与隧道。",
  vpngate: "选择出口地区，查看网关连接状态。",
  certificates: "统一管理节点证书与续期。",
  maintenance: "查看操作结果、运行日志与恢复工具。",
  admins: "管理控制台访问权限与账户安全。",
};
const canView = (id, role) =>
  role === "admin" ||
  ["overview", "accounts"].includes(id) ||
  (role === "operator" && ["maintenance", "vpngate"].includes(id));
const setMenu = (open) => {
  $("#shell").classList.toggle("nav-open", open);
  $("#menu-toggle").setAttribute("aria-expanded", String(open));
  $("#menu-toggle").textContent = open ? "收起导航" : "打开导航";
};
$("#menu-toggle").addEventListener("click", () =>
  setMenu(!$("#shell").classList.contains("nav-open")),
);
let page = "overview",
  navigationPage = "",
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
  async selectCountry(country) {
    const current = await ctx.config();
    const generation = draftGeneration;
    const submitted = JSON.stringify(current);
    const next = await api("/api/country", { country, config: current });
    if (generation !== draftGeneration || current !== draft)
      throw new DOMException("草稿已重新载入", "AbortError");
    if (submitted !== JSON.stringify(current))
      throw new Error("准备国家期间草稿发生变化，请重试。");
    draft = next;
    ctx.changed();
    return ctx.render();
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
  $("#page-description").textContent = descriptions[definition[0]];
  document.title = definition[1] + " · sing-box-vpn";
  markNavigation($("#navigation"), page, navigationPage !== page);
  navigationPage = page;
  try {
    $("#content").setAttribute("aria-busy", "true");
    // Each render owns its callback; a late render cannot replace the live one.
    const view = pageContext(ctx, signal);
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
  heartbeat: () => {
    if (page === "accounts") ctx.updateOverview?.(ctx.overview);
  },
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
  page = location.hash.slice(1) || "overview";
  if (!pages.some((p) => p[0] === page) || !canView(page, session.role))
    page = "overview";
  $("#navigation").replaceChildren(...navigation(
    navigationGroups, pages, canView, session.role, (id) => {
      page = id;
      location.hash = id;
      setMenu(false);
      renderPage();
    }, page,
  ));
  await renderPage();
}
$("#login-form").addEventListener(
  "submit",
  action(async (event) => {
    event.preventDefault();
    $("#login-error").textContent = "";
    const values = Object.fromEntries(new FormData(event.target));
    try {
      validatePassword(values.password, (await capabilities()).password_bytes);
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
    showConfiguration(result, changed);
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
  let next = location.hash.slice(1);
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
