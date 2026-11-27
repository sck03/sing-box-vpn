import type {
  TaskCommand,
  AccountExportRequest,
  RefreshResult,
  DeliveryResult,
  SubscriptionStatusResponse,
  Grant,
  App,
  ConfigurationPreview,
  OutputPreview,
  InboundPreparation,
} from "../internal/manage/panel/contracts.js";

// Compile-time wire examples. These never send requests or ship in the panel.
const commands: TaskCommand[] = [
  {
    operation: "account",
    account: { operation: "bind", id: "alice", scope: "normal", inbound_ids: ["in"] },
  },
  {
    operation: "account",
    account: {
      operation: "bind",
      account_ids: ["alice", "bob"],
      inbound_ids: ["in"],
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
    subscription: { operation: "check", id: "output", probe_url: "https://example.test" },
  },
];
void commands;

// @ts-expect-error a preset must reserve ports from the current draft
const missingCandidate: InboundPreparation = { id: "entry", protocol: "vless" };
void missingCandidate;

const emptyBinding: TaskCommand = {
  operation: "account",
  // @ts-expect-error explicit selection must contain at least one inbound
  account: { operation: "bind", id: "alice", scope: "normal", inbound_ids: [] },
};
const implicitBinding: TaskCommand = {
  operation: "account",
  // @ts-expect-error implicit all-inbound binding is no longer supported
  account: { operation: "bind", id: "alice", scope: "normal" },
};
void [emptyBinding, implicitBinding];

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
const exportOptions: AccountExportRequest = {
  id: "alice", profile: "uri", port_overrides: { "normal/in": 62001 },
};
const bindingWithExportOptions = {
  ...exportOptions, operation: "bind" as const, scope: "normal",
  inbound_ids: ["in"] as [string, ...string[]],
};
const exported: TaskCommand = {
  operation: "account",
  // @ts-expect-error credential tasks cannot carry download/export options
  account: bindingWithExportOptions,
};
// @ts-expect-error operation-specific values must agree
const invalid: TaskCommand = { operation: "bbr", value: "disable" };
void [automatic, unrelated, exported, invalid];

const scopedRefresh: TaskCommand = {
  operation: "subscription",
  // @ts-expect-error refresh is global; a silently ignored ID is invalid
  subscription: { operation: "refresh", id: "source" },
};
const missingOutput: TaskCommand = {
  operation: "subscription",
  // @ts-expect-error a check must identify its output
  subscription: { operation: "check" },
};
const deliveryProbe: TaskCommand = {
  operation: "subscription",
  // @ts-expect-error delivery cannot carry probe options
  subscription: { operation: "deliver", probe_url: "https://example.test" },
};
void [scopedRefresh, missingOutput, deliveryProbe];

const missingDeliveryTarget: TaskCommand = {
  operation: "subscription",
  // @ts-expect-error manual delivery requires a destination
  subscription: { operation: "deliver" },
};
const missingProbeURL: TaskCommand = {
  operation: "subscription",
  // @ts-expect-error a check requires its endpoint before entering the task queue
  subscription: { operation: "check", id: "output" },
};
void [missingDeliveryTarget, missingProbeURL];

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
  deployment: { changes: [], runtime_observed: false, applied_revision: 1 },
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
// @ts-expect-error program versions belong to component status, never App
app.version = "obsolete";
void [preview, output, badPreview];

// Endpoint names select both the write DTO and response type.
import { query, mutate } from "../internal/manage/panel/api.js";
async function endpointContracts() {
 const certificates = await query("/api/certificates");
 const available: boolean = certificates[0].can_renew;
 void available;
 await mutate("/api/tasks", { operation: "certificate-renew", id: "live" });
 // @ts-expect-error certificate preparation requires its contact and domain
 await mutate("/api/certificates/prepare", { id: "draft" });
 // @ts-expect-error task input cannot be sent as a configuration save
 await mutate("/api/config", { operation: "certificate-renew", id: "live" });
 // @ts-expect-error unknown endpoint has no response contract
 await query("/api/nonexistent");
 const settings = await query("/api/panel/settings");
 await mutate("/api/panel/settings", { options: settings.options, revision: settings.revision });
 const enrollment = await mutate("/api/mfa/enroll", { password: "test-password" });
 const secret: string = enrollment.secret;
 void secret;
 // @ts-expect-error panel settings use a string revision, not a configuration revision
 await mutate("/api/panel/settings", { options: settings.options, revision: 3 });
 // @ts-expect-error subscriber state is boolean
 await mutate("/api/subscribers", { id: "test", name: "Test", disabled: "false" });
 // @ts-expect-error MFA confirmation codes are strings
 await mutate("/api/mfa/confirm", { code: 123456 });
 await mutate("/api/grants/revoke", { id: "link" });
 // @ts-expect-error grant revocation requires a stable string ID
 await mutate("/api/grants/revoke", { id: 123 });
}
void endpointContracts;
