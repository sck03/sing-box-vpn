import { api, getAccountOutputs } from "./api.js";
import { loadDialog } from "./dialog-session.js";
import { validationLabel } from "./validation.js";
import { el, help, button, formDialog } from "./ui.js";

// Availability is fetched for the selected account and scope. Share creation
// and direct export use the same form and retain the same selection rules.
export async function accountOutputDialog({
  accounts,
  ctx,
  title,
  multiple = false,
  defaults = {},
  fields = [],
  save,
}) {
  const request = ctx.workflowRequest || api;
  const key = multiple ? "profiles" : "profile";
  const initial = { account_id: accounts[0].id, scope: "", ...defaults };
  const load = (value, signal) =>
    getAccountOutputs(value.account_id, value.scope, signal, request);
  return loadDialog(
    ctx.signal,
    title,
    (signal) => load(initial, signal),
    (options) => {
      let pending = false,
        generation = 0,
        query;
      const available = () => options.filter((option) => option.available);
      const preferred = [
        "uri",
        "base64",
        "mihomo-provider",
        "sing-box-nodes",
      ].filter((id) => available().some((option) => option.profile === id));
      initial[key] = multiple
        ? preferred
        : preferred[0] || available()[0]?.profile || "";
      const clientField = {
        key,
        label: multiple ? "允许的订阅格式与用途（可多选）" : "订阅格式与用途",
        type: multiple ? "multiselect" : "select",
        required: true,
        options: () =>
          available().map((option) => [
            option.profile,
            `${option.label} · ${option.nodes}/${option.input_nodes} 节点${option.experimental ? " · 实验适配" : ""}`,
          ]),
      };
      const controller = formDialog(
        title,
        [
          {
            key: "account_id",
            label: "使用权益",
            type: "select",
            readonly: accounts.length === 1,
            options: accounts.map((account) => [account.id, account.name]),
          },
          {
            key: "scope",
            label: "运行实例",
            type: "select",
            options: (value) =>
              [
                ["", "所有已绑定实例"],
                ["normal", "普通"],
                ["vpngate", "VPNGate"],
              ].filter(
                ([scope]) =>
                  !scope ||
                  accounts
                    .find((account) => account.id === value.account_id)
                    ?.download_scopes?.includes(scope),
              ),
          },
          clientField,
          ...fields,
        ],
        initial,
        async (value) => {
          if (pending) throw new Error("正在检查客户端兼容性，请稍候。");
          const selected = multiple ? value.profiles : [value.profile];
          if (
            !selected?.length ||
            selected.some(
              (id) => !available().some((option) => option.profile === id),
            )
          )
            throw new Error("请选择当前权益可用的客户端格式。");
          await save(value);
        },
      );
      const summary = el("div", {
        class: "client-compatibility",
        role: "status",
      });
      controller.form.prepend(summary);
      function describe() {
        const selected = multiple
          ? controller.value().profiles
          : [controller.value().profile];
        const active = options.filter((option) =>
          selected.includes(option.profile),
        );
        const denied = options.filter((option) => !option.available);
        summary.replaceChildren(
          ...active.map((option) =>
            help(
              `${option.label}：${option.nodes} 个可用节点${option.skipped ? `，跳过 ${option.skipped} 个不兼容节点` : ""}。${option.description || ""} ${validationLabel(option.validation)}`,
            ),
          ),
          ...(denied.length
            ? [
                help(
                  `暂不可用：${denied.map((option) => option.label).join("、")}。可调整入站绑定或账户下载模板。`,
                ),
              ]
            : []),
        );
      }
      let signature = `${initial.account_id}:${initial.scope}`;
      controller.form.addEventListener("change", async () => {
        const value = controller.value(),
          next = `${value.account_id}:${value.scope}`;
        if (next === signature) {
          describe();
          return;
        }
        signature = next;
        const current = ++generation;
        const selected = multiple ? value.profiles : [value.profile];
        query?.abort();
        query = new AbortController();
        pending = true;
        options = [];
        controller.refresh();
        summary.replaceChildren(help("正在检查当前实例的客户端兼容性…"));
        try {
          const result = await load(value, query.signal);
          if (!controller.active() || generation !== current) return;
          options = result;
          controller.refresh();
          const input = controller.input(key);
          const kept = selected.filter((id) =>
            available().some((option) => option.profile === id),
          );
          if (multiple)
            for (const option of input.options)
              option.selected = kept.includes(option.value);
          else input.value = kept[0] || available()[0]?.profile || "";
          describe();
        } catch (error) {
          if (
            controller.active() &&
            generation === current &&
            error.name !== "AbortError"
          )
            summary.replaceChildren(
              help("兼容性暂不可读取：" + error.message),
              button("重试检查", () => {
                signature = "";
                controller.form.dispatchEvent(new window.Event("change"));
              }),
            );
        } finally {
          if (generation === current) pending = false;
        }
      });
      controller.dialog.addEventListener("close", () => query?.abort(), {
        once: true,
      });
      describe();
      return controller;
    },
  );
}
