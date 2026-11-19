import type { TaskCommand } from "../internal/manage/panel/contracts.js";

// Compile-time wire examples. These never send requests or ship in the panel.
const commands: TaskCommand[] = [
  { operation: "account", account: { operation: "bind", id: "alice", scope: "normal" } },
  { operation: "account", account: { operation: "bind", account_ids: ["alice", "bob"], scope: "normal" } },
  { operation: "account", account: { operation: "rotate", id: "credential", generation: 1 } },
  { operation: "service", service: "sb-core", value: "start" },
  { operation: "gateway", config: { op: "swap" } },
  { operation: "subscription", subscription: { operation: "check", id: "output" } },
];
void commands;

// @ts-expect-error scheduling metadata is internal
const automatic: TaskCommand = { operation: "subscription", automatic: true, subscription: { operation: "refresh" } };
// @ts-expect-error service commands cannot carry account selectors
const unrelated: TaskCommand = { operation: "service", service: "sb-core", value: "stop", id: "ignored" };
// @ts-expect-error credential tasks cannot carry download/export options
const exported: TaskCommand = { operation: "account", account: { operation: "bind", scope: "normal", client: "uri" } };
// @ts-expect-error operation-specific values must agree
const invalid: TaskCommand = { operation: "bbr", value: "disable" };
void [automatic, unrelated, exported, invalid];
