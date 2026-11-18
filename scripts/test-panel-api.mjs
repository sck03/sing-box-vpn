import assert from "node:assert/strict";

const base = process.env.PANEL_TEST_URL;
if (
  !base ||
  !["127.0.0.1", "localhost", "[::1]"].includes(new URL(base).hostname)
)
  throw new Error("PANEL_TEST_URL must point at a disposable loopback panel");
const password = process.env.PANEL_TEST_PASSWORD;
if (!password) throw new Error("PANEL_TEST_PASSWORD is required");
let cookie, csrf;
async function call(path, body, extra = {}) {
  const response = await fetch(base + path, {
    method: body === undefined ? "GET" : "POST",
    headers: {
      ...(cookie ? { Cookie: cookie, "X-CSRF-Token": csrf } : {}),
      "Content-Type": "application/json",
      ...extra,
    },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(120000),
  });
  const text = await response.text();
  let value;
  try {
    value = JSON.parse(text);
  } catch {
    value = text;
  }
  return { status: response.status, value, headers: response.headers };
}
function ok(result) {
  assert.equal(
    result.status,
    200,
    typeof result.value === "string"
      ? result.value
      : JSON.stringify(result.value),
  );
  return result.value;
}
const login = await call("/api/auth/login", {
  username: process.env.PANEL_TEST_USERNAME || "reviewadmin",
  password,
});
csrf = ok(login).csrf;
cookie = login.headers.get("set-cookie").split(";")[0];
const overview = ok(await call("/api/overview"));
assert(
  overview.services.every((s) => s.state === "unavailable"),
  "Only --no-system test installations are supported",
);
assert.equal(
  ok(await call("/api/capabilities")).gateway.limits.max_connections.max,
  1024,
);
let state = ok(await call("/api/config"));
const config = state.config;
assert.equal(config.app.schema, 2);
assert.equal(config.subscriptions.version, 2);
const invalid = structuredClone(config);
invalid.gateway = { connect_timeout: 240 };
assert.equal((await call("/api/config/preview", invalid)).status, 400);
const old = structuredClone(config);
old.app.warp = {};
assert.equal((await call("/api/config/preview", old)).status, 400);

const suffix = Date.now().toString(36),
  person = "demo-" + suffix,
  account = "monthly-" + suffix;
ok(
  await call("/api/subscribers", {
    id: person,
    name: "演示订阅者",
    disabled: false,
    revision: 0,
  }),
);
ok(
  await call("/api/accounts", {
    account: {
      id: account,
      name: "月度权益",
      subscriber_id: person,
      start_mode: "created",
      period: { kind: "monthly", day: 1, timezone: "Asia/Shanghai" },
      quota_bytes: 100 * 1073741824,
      revision: 0,
    },
  }),
);
const nodes = ok(await call("/api/inbound-options?scope=normal"));
const inbound = nodes.find((n) => n.id.includes("vmess")) || nodes[0];
const queued = ok(
  await call("/api/tasks", {
    operation: "account",
    account: {
      operation: "bind",
      id: account,
      scope: "normal",
      inbound_ids: [inbound.id],
    },
  }),
);
async function finished(id) {
  const deadline = Date.now() + 90000;
  while (Date.now() < deadline) {
    const task = ok(await call("/api/tasks/detail?id=" + id));
    if (!["queued", "running"].includes(task.state)) {
      assert.equal(task.state, "succeeded", JSON.stringify(task));
      return task;
    }
    await new Promise((resolve) => setTimeout(resolve, 300));
  }
  throw new Error("Test task did not finish");
}
await finished(queued.task_id);
assert.equal(
  (
    await call("/api/grants", {
      id: "local-" + suffix,
      account_id: account,
      clients: ["uri"],
    })
  ).status,
  400,
  "loopback shares must be rejected",
);
// This synthetic origin is used only in the disposable no-system installation.
config.listener = {
  enabled: true,
  listen: "127.0.0.1",
  port: 2097,
  public_base_url: "https://subscriptions.example.test",
};
await finished(
  ok(await call("/api/config", { revision: state.revision, config })).task_id,
);
state = ok(await call("/api/config"));
assert(
  ok(
    await call("/api/accounts/export", {
      id: account,
      scope: "normal",
      client: "uri",
    }),
  ).content.includes("://"),
);
const grant = ok(
  await call("/api/grants", {
    id: "share-" + suffix,
    account_id: account,
    scope: "normal",
    clients: ["uri"],
    max_downloads: 1,
  }),
);
assert.match(grant.user_url, /^https:\/\/subscriptions\.example\.test\/user#/);
ok(
  await call("/user", undefined, {
    "Sec-Fetch-Site": "cross-site",
    Origin: "https://external.example",
  }),
);
ok(await call("/u/" + grant.token + "/usage"));
ok(await call("/u/" + grant.token + "/uri"));
assert.equal((await call("/u/" + grant.token + "/uri")).status, 403);
assert.equal(ok(await call("/u/" + grant.token + "/usage")).downloads, 1);
assert.equal(
  (await call("/api/state", undefined, { "Sec-Fetch-Site": "cross-site" }))
    .status,
  403,
);

config.subscriptions.sources = [
  {
    id: "account-source",
    kind: "account",
    account_id: account,
    scope: "normal",
    required: true,
  },
];
config.subscriptions.collections = [
  { id: "collection", sources: ["account-source"], processor: {} },
];
config.subscriptions.templates = [
  { id: "template", client: "uri", version: 1, mode: "nodes" },
];
config.subscriptions.outputs = [
  {
    id: "output",
    name: "账户订阅",
    collection: "collection",
    template: "template",
  },
];
assert.equal(
  ok(await call("/api/config/preview", config)).outputs[0].available,
  true,
);
const apply = ok(
  await call("/api/config", { revision: state.revision, config }),
);
const result = await finished(apply.task_id);
assert.equal(result.result.outcome, "passed");
assert.equal(result.result.outputs, 1);
const saved = ok(await call("/api/config"));
assert.equal(saved.revision, saved.applied_revision);

const options = ok(await call("/api/grant-options"));
assert.equal(options.pending, false);
assert(options.outputs.some((o) => o.id === "output"));
assert.equal(
  ok(await call("/u/" + grant.token + "/usage")).download_denial,
  "download_limit",
);
const limited = {
  id: "limited-" + suffix,
  name: "限额权益",
  quota_bytes: null,
  duration_seconds: 86400,
};
ok(await call("/api/accounts", { account: limited }));
const findAccount = async (archived = false) =>
  ok(await call(`/api/accounts?q=${limited.id}&archived=${archived}`)).items[0];
let entitlement = await findAccount();
assert.equal(entitlement.expires_at, null);
assert(entitlement.effective_expires_at);
assert.equal(entitlement.download_denial, "no_bound_inbound");
assert.equal(
  (
    await call("/api/grants", {
      id: "unbound-" + suffix,
      account_id: limited.id,
      clients: ["uri"],
    })
  ).status,
  400,
);
await finished(
  ok(
    await call("/api/tasks", {
      operation: "account",
      account: {
        operation: "bind",
        id: limited.id,
        scope: "normal",
        inbound_ids: [inbound.id],
      },
    }),
  ).task_id,
);
const denied = ok(
  await call("/api/grants", {
    id: "limited-share-" + suffix,
    account_id: limited.id,
    clients: ["uri"],
  }),
);
const credentials = ok(
  await call(`/api/credentials?id=${limited.id}&scope=normal`),
);
await finished(
  ok(
    await call("/api/tasks", {
      operation: "account",
      account: {
        operation: "unbind",
        id: credentials[0].id,
        generation: credentials[0].generation,
        scope: "normal",
      },
    }),
  ).task_id,
);
assert.equal(
  ok(await call(`/u/${denied.token}/usage`)).download_denial,
  "no_bound_inbound",
);
assert.equal((await call(`/u/${denied.token}/uri`)).status, 403);
limited.quota_bytes = 0;
entitlement = await findAccount();
ok(
  await call("/api/accounts", {
    account: { ...limited, name: "改名", revision: entitlement.revision },
  }),
);
assert.equal((await findAccount()).expires_at, null);
assert.equal(
  ok(await call(`/u/${denied.token}/usage`)).download_denial,
  "quota_exhausted",
);
assert.equal((await call(`/u/${denied.token}/uri`)).status, 403);
assert.equal(
  (
    await call("/api/accounts/archive", {
      id: limited.id,
      revision: entitlement.revision,
    })
  ).status,
  409,
);
entitlement = await findAccount();
ok(
  await call("/api/accounts/archive", {
    id: limited.id,
    revision: entitlement.revision,
  }),
);
assert.equal(await findAccount(), undefined);
assert.equal((await findAccount(true)).state, "archived");
assert(
  !ok(await call("/api/account-options")).some((o) => o.id === limited.id),
);
assert.equal((await call(`/u/${denied.token}/usage`)).status, 404);

console.log(
  "Live panel API passed: shared constraints, current schemas, account binding/export, cross-site sharing, download limits, candidate preview and durable apply.",
);
