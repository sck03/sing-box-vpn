import { api, setCSRF } from "./api.js";
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
let page = "overview",
  draft = null,
  original = null,
  revision = 0,
  draftGeneration = 0,
  configLoad = null,
  loading = 0,
  refreshTimer,
  events;
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
    ctx.session = null;
    resetDraft();
    ctx.overview = null;
    ctx.updateOverview = null;
    setCSRF("");
    clearTimeout(refreshTimer);
    events?.close();
    events = null;
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
  const generation = ++loading;
  ctx.updateOverview = null;
  const definition = pages.find((v) => v[0] === page) || pages[0];
  $("#page-title").textContent = definition[1];
  document.title = definition[1] + " · sing-box-vpn";
  for (const item of $("#navigation").children)
    item.setAttribute(
      "aria-current",
      item.dataset.page === page ? "page" : "false",
    );
  try {
    let content;
    if (
      ctx.session.role !== "admin" &&
      !["overview", "accounts", "subscribers"].includes(page) &&
      !(ctx.session.role === "operator" && page === "maintenance")
    ) {
      const data = await api("/api/state");
      content = [
        card(
          "只读配置摘要",
          help(
            "当前角色可以查看状态与用量。编辑配置、代理秘密和管理员设置需要管理员角色。",
          ),
          el("pre", {}, JSON.stringify(data.config, null, 2)),
        ),
      ];
    } else {
      // Each render owns its callback; a late render cannot replace the live one.
      const view = Object.create(ctx);
      content = await definition[2](view);
      if (generation === loading) ctx.updateOverview = view.updateOverview;
    }
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
  }
}
function schedule() {
  clearTimeout(refreshTimer);
  if (!ctx.session || page !== "overview" || document.hidden) {
    events?.close();
    events = null;
    return;
  }
  if (!events) {
    const source = new EventSource("/api/events");
    events = source;
    source.addEventListener("overview", (event) => {
      if (events !== source || !ctx.session || page !== "overview") return;
      ctx.overview = JSON.parse(event.data);
      ctx.updateOverview?.(ctx.overview);
    });
    source.onerror = () => {
      if (events !== source) return;
      source.close();
      events = null;
      refreshTimer = setTimeout(async () => {
        try {
          await api("/api/auth/session");
          schedule();
        } catch (error) {
          if (error.name !== "AbortError") ctx.logoutLocal();
        }
      }, 5000);
    };
  }
}
async function connected(session) {
  ctx.session = session;
  setCSRF(session.csrf);
  $("#login").hidden = true;
  $("#shell").hidden = false;
  $("#identity").textContent = session.id + " · " + session.role;
  $("#navigation").replaceChildren(
    ...pages
      .filter(
        (p) =>
          session.role === "admin" ||
          (!["grants", "admins"].includes(p[0]) &&
            (p[0] !== "maintenance" || session.role === "operator")),
      )
      .map(([id, label]) => {
        const b = button(label, () => {
          page = id;
          location.hash = id;
          renderPage();
        });
        b.dataset.page = id;
        return b;
      }),
  );
  page = location.hash.slice(1) || "overview";
  if (!pages.some((p) => p[0] === page)) page = "overview";
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
    page = next;
    renderPage();
  }
});
api("/api/auth/session")
  .then(connected)
  .catch((error) => {
    if (error.name !== "AbortError") ctx.logoutLocal();
  });
