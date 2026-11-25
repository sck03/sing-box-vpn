import type {
  TaskCommand,
  RefreshResult,
  DeliveryResult,
  SubscriptionStatusResponse,
  Grant,
  App,
  ConfigurationPreview,
  OutputPreview,
} from "../internal/manage/panel/contracts.js";

// Compile-time wire examples. These never send requests or ship in the panel.
const commands: TaskCommand[] = [
  {
    operation: "account",
    account: { operation: "bind", id: "alice", scope: "normal" },
  },
  {
    operation: "account",
    account: {
      operation: "bind",
      account_ids: ["alice", "bob"],
      scope: "normal",
    },
  },
  {
    operation: "account",
    account: { operation: "rotate", id: "credential", generation: 1 },
  },
  { operation: "service", service: "sb-core", value: "start" },
  { operation: "gateway", config: { op: "swap" } },
  {
    operation: "subscription",
    subscription: { operation: "check", id: "output" },
  },
];
void commands;

const automatic: TaskCommand = {
  operation: "subscription",
  // @ts-expect-error scheduling metadata is internal
  automatic: true,
  subscription: { operation: "refresh" },
};
// @ts-expect-error service commands cannot carry account selectors
const unrelated: TaskCommand = {
  operation: "service",
  service: "sb-core",
  value: "stop",
  id: "ignored",
};
const exported: TaskCommand = {
  operation: "account",
  // @ts-expect-error credential tasks cannot carry download/export options
  account: { operation: "bind", scope: "normal", client: "uri" },
};
// @ts-expect-error operation-specific values must agree
const invalid: TaskCommand = { operation: "bbr", value: "disable" };
void [automatic, unrelated, exported, invalid];

// Stateful results and grant limits must remain explicit in generated contracts.
const refresh: RefreshResult = {
  outcome: "partial",
  checked: 4,
  updated: 3,
  outputs: 1,
  expected_outputs: 1,
  failures: [],
  published: true,
  deferred: 2,
  interrupted: false,
};
const limits: Pick<Grant, "expires_at" | "max_downloads"> = {
  expires_at: null,
  max_downloads: 3,
};
// @ts-expect-error refresh counts are numeric
const badCount: RefreshResult = { ...refresh, checked: "4" };
// @ts-expect-error publication state is a required boolean
const badPublished: RefreshResult = { ...refresh, published: "yes" };
// @ts-expect-error grant download counts are numeric
const badLimit: Pick<Grant, "max_downloads"> = { max_downloads: "3" };
void [refresh, limits, badCount, badPublished, badLimit];

const delivery: DeliveryResult = {
  outcome: "partial",
  selected: 2,
  interrupted: true,
  items: [{ id: "device", status: "delivered", persisted: true }],
};
const activity: Pick<SubscriptionStatusResponse, "deliveries"> = {
  deliveries: [
    {
      id: "device",
      kind: "http-put",
      state: "uncertain",
      failures: 1,
      output: "provider",
      automatic: true,
    },
  ],
};
const incompleteDelivery: DeliveryResult = {
  ...delivery,
  // @ts-expect-error delivery acknowledgement persistence is explicit
  items: [{ id: "device", status: "delivered" }],
};
const privateDelivery: Pick<SubscriptionStatusResponse, "deliveries"> = {
  // @ts-expect-error runtime status does not expose destination credentials
  deliveries: [{ ...activity.deliveries[0], token: "secret" }],
};
void [delivery, activity, incompleteDelivery, privateDelivery];

const preview: ConfigurationPreview = {
  valid: true,
  ordinary_server: {},
  outputs: [],
  diagnostics: {},
};
const output: OutputPreview = {
  id: "account",
  name: "Account",
  mime: "text/plain",
  nodes: 2,
  diagnostics: [],
  preview: { content: "redacted", truncated: false, decoded: false },
};
// @ts-expect-error preview node counts must stay numeric
const badPreview: OutputPreview = { ...output, nodes: "2" };
declare const app: App;
// @ts-expect-error tunnel state belongs to PlatformConfig, never App
app.argo = {};
// @ts-expect-error routing has one representation in app.advanced
app.routing = {};
// @ts-expect-error SOCKS endpoints are explicit outbound resources
app.socks_port = 40000;
void [preview, output, badPreview];
