import { api, exportAccount, getAccountPorts } from "./api.js";
import { accountOutputDialog } from "./account-outputs.js";
import { showExport } from "./export-results.js";

// Only bound, applied TUIC entries offer overrides. Scope changes retain edits
// but exclude hidden entries from the command sent to the server.
export function exportAccountDialog(account, ctx) {
  const request = ctx.workflowRequest || api;
  let ports = [];
  return accountOutputDialog({
    accounts: [account], ctx, title: "账户专属导出",
    loadFields: async (signal) => {
      ports = (await getAccountPorts(account.id, signal, request))
        .filter((option) => option.udp_ports?.length);
      return ports.map((option, index) => ({
        key: "port_" + index,
        label: `${option.runtime_id === "normal" ? "普通" : "VPNGate"} · ${option.inbound_id} 的 TUIC 端口`,
        type: "number", min: 1, max: 65535, required: true, advanced: true,
        default: option.port,
        visible: (value) => !value.scope || value.scope === option.runtime_id,
        help: `主端口 ${option.port}；可选范围 ${option.udp_ports.join("、")}`,
      }));
    },
    save: async (value) => {
      const port_overrides = {};
      ports.forEach((option, index) => {
        const port = value["port_" + index];
        if ((!value.scope || value.scope === option.runtime_id) && port !== option.port)
          port_overrides[option.runtime_id + "/" + option.inbound_id] = port;
      });
      const result = await exportAccount({
        id: account.id, scope: value.scope, profile: value.profile, port_overrides,
      }, request);
      showExport(result, value.profile);
    },
  });
}
