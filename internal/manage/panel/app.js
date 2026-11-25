import {
  api,
  setCSRF,
  saveConfiguration,
  submitTask,
  getConfiguration,
  previewConfiguration,
  previewOutput,
} from "./api.js";
import { openTask } from "./tasks.js";
import { secretField } from "./secret-field.js";
import { overviewStream } from "./live.js";
import { pageContext } from "./lifecycle.js";
import { capabilities, validatePassword } from "./capabilities.js";
import { requestConfiguration } from "./config-preview.js";
import {
  navigation,
  markNavigation,
  pageTabs,
  featureSearch,
} from "./navigation.js";
import { pageFor, groupFor, groupLabel, routeFor } from "./page-catalog.js";
import { $, card, help, notice, action, button } from "./ui.js";
const loginPassword = /** @type {HTMLInputElement} */ (
  document.querySelector('input[name="password"]')
);
if (loginPassword) {
  const container = loginPassword.parentElement;
  const control = secretField(loginPassword, "登录密码");
  container.append(control);
}
const setMenu = (open) => {
  $("#shell").classList.toggle("nav-open", open);
  $("#menu-toggle").setAttribute("aria-expanded", String(open));
  $("#menu-toggle").textContent = open ? "收起导航" : "打开导航";
};
$("#menu-toggle").addEventListener("click", () =>
  setMenu(!$("#shell").classList.contains("nav-open")),
);
let page = "overview",
  section = "",
  navigationPage = "",
  draft = /** @type {import("./contracts.js").PlatformConfig | null} */ (null),
  original = null,
  revision = 0,
  draftGeneration = 0,
  configLoad = null,
  loading = 0,
  pageRequest,
  showSection,
  clearSearch;
const ctx = {
  session: null,
  root: "",
  dirty: false,
  async config() {
    if (!draft) {
      const generation = draftGeneration;
      configLoad ??= getConfiguration(ctx.session.role === "admin")
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
    $("#draft-message").textContent = "配置有未保存的更改 · 切换页面会保留草稿";
  },
  observeRevision(value) {
    if (draft && value > revision) {
      $("#draft").hidden = false;
      $("#draft-message").textContent =
        "服务器已有新的配置版本，请核对草稿后重新载入。";
    }
  },
  /** @param {import("./contracts.js").TaskCommand} command */
  async task(command) {
    const response = await submitTask(command);
    notice("任务 #" + response.task_id + " 已排队", false, {
      label: "查看结果",
      action: () => openTask(response.task_id),
    });
    return response.task_id;
  },
  render: renderPage,
  logoutLocal() {
    ++loading;
    pageRequest?.abort();
    showSection = null;
    setMenu(false);
    ctx.session = null;
    resetDraft();
    ctx.overview = null;
    ctx.updateOverview = null;
    clearSearch?.();
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
  showSection = null;
  ctx.updateOverview = null;
  const definition = pageFor(page, ctx.session.role);
  const label = groupLabel(groupFor(page), ctx.session.role);
  $("#page-title").textContent = definition.title;
  $("#page-group").textContent = label;
  $("#page-group").hidden = label === definition.title;
  $("#page-description").textContent = definition.description;
  document.title = definition.title + " · sing-box-vpn";
  pageTabs($("#page-tabs"), page, ctx.session.role, navigate);
  if ($("#page-tabs").hidden) {
    $("#content").removeAttribute("role");
    $("#content").removeAttribute("aria-labelledby");
  } else {
    $("#content").setAttribute("role", "tabpanel");
    $("#content").setAttribute("aria-labelledby", "page-tab-" + page);
  }
  markNavigation($("#navigation"), page, navigationPage !== page);
  navigationPage = page;
  try {
    $("#content").setAttribute("aria-busy", "true");
    $("#content").inert = true;
    // Each render owns its callback; a late render cannot replace the live one.
    const view = pageContext(ctx, signal);
    view.section = section;
    view.navigate = (id) => !signal.aborted && navigate(id);
    const initialSnapshot = ctx.overview;
    const content = await definition.load(view);
    if (generation !== loading) return;
    showSection = view.showSection;
    // Sections register their handlers after asynchronous lazy loading.
    ctx.updateOverview = (snapshot) => {
      if (!signal.aborted) view.updateOverview?.(snapshot);
    };
    $("#content").replaceChildren(...content);
    if (ctx.overview && ctx.overview !== initialSnapshot)
      ctx.updateOverview?.(ctx.overview);
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
    if (generation === loading) {
      $("#content").removeAttribute("aria-busy");
      $("#content").inert = false;
    }
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
    const { active, attention } = ctx.overview.task_counts;
    const status = $("#task-status");
    status.hidden = active === 0 && attention === 0;
    status.href =
      ctx.session.role === "viewer" ? "#overview" : "#maintenance/logs";
    status.textContent = active
      ? `${active} 个任务正在处理${attention ? ` · ${attention} 个需要关注` : ""} · 查看进度`
      : `${attention} 个任务需要关注 · 查看详情`;
    status.classList.toggle("error", attention > 0);
    ctx.updateOverview?.(ctx.overview);
  },
});
function schedule() {
  if (typeof EventSource !== "undefined")
    stream.setActive(!!ctx.session && !document.hidden);
}
function navigate(id) {
  if (!ctx.session) return;
  const next = routeFor(id, ctx.session.role);
  if (location.hash !== "#" + next.hash)
    window.history.pushState(null, "", "#" + next.hash);
  return applyRoute(next);
}
function applyRoute(next) {
  setMenu(false);
  if (page === next.page && section === next.section) return;
  if ($("#dialog").open) $("#dialog").close();
  $("#dialog").replaceChildren();
  const samePage = page === next.page;
  page = next.page;
  section = next.section;
  if (samePage && showSection) return showSection(section);
  return renderPage();
}
async function connected(session) {
  ctx.session = session;
  setCSRF(session.csrf);
  $("#login").hidden = true;
  $("#shell").hidden = false;
  $("#identity").textContent = session.id + " · " + session.role;
  const route = routeFor(location.hash.slice(1), session.role);
  page = route.page;
  section = route.section;
  if (location.hash && location.hash !== "#" + route.hash)
    window.history.replaceState(null, "", "#" + route.hash);
  navigationPage = "";
  delete $("#page-tabs").dataset.group;
  $("#navigation").replaceChildren(...navigation(session.role, navigate, page));
  clearSearch = featureSearch(
    $("#feature-search"),
    $("#feature-results"),
    session.role,
    navigate,
  );
  schedule();
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
    await requestConfiguration(
      async (signal) => {
        const result = await previewConfiguration(submitted, signal);
        if (generation !== draftGeneration)
          throw new DOMException("草稿已重新载入", "AbortError");
        return result;
      },
      changed,
      {
        signal: pageRequest?.signal,
        loadOutput: (id, signal) =>
          previewOutput({ id, config: submitted }, signal),
      },
    );
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
    const response = await saveConfiguration({
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
      false,
      { label: "查看结果", action: () => openTask(response.task_id) },
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
  if (!ctx.session) return;
  const next = routeFor(location.hash.slice(1), ctx.session.role);
  if (location.hash && location.hash !== "#" + next.hash)
    window.history.replaceState(null, "", "#" + next.hash);
  applyRoute(next);
});
api("/api/auth/session")
  .then(connected)
  .catch((error) => {
    if (error.name !== "AbortError") ctx.logoutLocal();
  });
