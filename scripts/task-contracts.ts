import type {
  TaskCommand,
  RefreshResult,
  DeliveryResult,
  SubscriptionStatusResponse,
  Grant,
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

// @ts-expect-error scheduling metadata is internal
const automatic: TaskCommand = {
  operation: "subscription",
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
// @ts-expect-error credential tasks cannot carry download/export options
const exported: TaskCommand = {
  operation: "account",
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
// @ts-expect-error delivery acknowledgement persistence is explicit
const incompleteDelivery: DeliveryResult = {
  ...delivery,
  items: [{ id: "device", status: "delivered" }],
};
// @ts-expect-error runtime status does not expose destination credentials
const privateDelivery: Pick<SubscriptionStatusResponse, "deliveries"> = {
  deliveries: [{ ...activity.deliveries[0], token: "secret" }],
};
void [delivery, activity, incompleteDelivery, privateDelivery];
