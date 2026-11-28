import { accountFields } from "./account-fields.js";
import { capabilities } from "./capabilities.js";
import { api, sessionSignal } from "./api.js";
import { saveAccount, inboundOptions } from "./account-api.js";
import {
  accountCreated,
  openAccountBindingTask,
  presentAccountResult,
} from "./account-onboarding.js";
import { loadDialog } from "./dialog-session.js";
import { formDialog, notice } from "./ui.js";

// Editing and onboarding share workflow lifetime; list reads belong to the page.
export function accountEditor(ctx) {
  const edit = async (item) =>
    loadDialog(
      ctx.signal,
      item.revision ? "编辑权益" : "新建权益",
      (signal) =>
        Promise.all([
          (ctx.workflowRequest || api)("/api/subscribers", undefined, {
            signal,
          }),
          item.revision ? [] : bindingFields(signal),
          capabilities(ctx.workflowRequest || api),
        ]),
      ([subscribers, bindings, limits]) =>
        formDialog(
          item.revision ? "编辑权益" : "新建权益",
          [
            ...accountFields(!!item.revision, subscribers, limits.account_name),
            ...(!item.revision
              ? [
                  {
                    key: "bind_now",
                    label: "创建后自动绑定节点",
                    type: "bool",
                    section: "可连接的节点",
                    readonly: !bindings[0]?.options.length,
                    help: bindings[0]?.options.length
                      ? "绑定成功后可直接创建分享链接。"
                      : "尚无已应用的节点，可先创建权益，稍后再绑定。",
                  },
                  ...bindings.map((field) => ({
                    ...field,
                    visible: (v) => v.bind_now,
                  })),
                ]
              : []),
          ],
          {
            ...item,
            bind_now: !!bindings[0]?.options.length,
            scope: bindings[0]?.options[0]?.[0] || "normal",
          },
          async (v, editor) => {
            const signal = sessionSignal();
            /** @type {import('./contracts.js').AccountSave['account']} */
            const account = { id: v.id, name: v.name };
            for (const key of [
              "id",
              "name",
              "subscriber_id",
              "quota_bytes",
              "expires_at",
              "disabled",
              "start_mode",
              "starts_at",
              "duration_seconds",
              "period",
              "revision",
              "created_at",
            ])
              if (v[key] !== undefined) account[key] = v[key];
            await saveAccount({ account }, ctx.workflowRequest || api);
            signal.throwIfAborted();
            notice("权益已保存，相关订阅会自动更新");
            let display = (clear) => accountCreated(account, bind, "", clear);
            if (!item.revision) {
              if (v.bind_now) {
                try {
                  const id = await ctx.task({
                    operation: "account",
                    account: {
                      operation: "bind",
                      id: account.id,
                      scope: v.scope,
                      inbound_ids: v.inbound_ids,
                    },
                  });
                  display = (clear) =>
                    openAccountBindingTask(ctx, account, id, bind, clear);
                } catch (error) {
                  signal.throwIfAborted();
                  display = (clear) =>
                    accountCreated(
                      account,
                      bind,
                      "权益已经保存，绑定未排队：" + error.message,
                      clear,
                    );
                }
              }
              await presentAccountResult(
                ctx,
                account,
                display,
                editor.active,
                signal,
              );
            } else if (editor.active() && !ctx.signal?.aborted) {
              ctx.render();
            }
          },
        ),
    );
  const bindingFields = async (signal) => {
    const scopes = ["normal", "vpngate"];
    const choices = await Promise.all(
      scopes.map((scope) =>
        inboundOptions(scope, signal, ctx.workflowRequest || api),
      ),
    );
    return [
      {
        key: "scope",
        label: "运行实例",
        type: "select",
        options: [
          ["normal", "普通节点"],
          ["vpngate", "VPNGate"],
        ].filter((_, index) => choices[index].length),
      },
      {
        key: "inbound_ids",
        label: "入站入口（可多选）",
        type: "multiselect",
        wide: true,
        options: (value) =>
          (choices[scopes.indexOf(value.scope)] || []).map((entry) => [
            entry.id,
            entry.name,
          ]),
        required: true,
        initial: (value) =>
          (choices[scopes.indexOf(value.scope)] || []).map((entry) => entry.id),
        resetOn: ["scope"],
        help: "默认勾选当前启用入口，可搜索后调整。至少选择一个；排队期间新增的入口不会自动加入。绑定会重载受影响入站，该入站下所有用户需重连。",
      },
    ];
  };
  const bind = async (item, signal) =>
    loadDialog(signal, "绑定入站", bindingFields, (fields) =>
      formDialog(
        "绑定入站",
        [
          ...fields,
          { key: "uuid", label: "自定义 UUID（留空随机）", advanced: true },
          {
            key: "password",
            label: "自定义密码（16–128 字节，留空随机）",
            type: "password",
            advanced: true,
          },
        ],
        { scope: fields[0].options[0]?.[0] || "" },
        async (v, editor) => {
          const workflow = sessionSignal();
          const id = await ctx.task({
            operation: "account",
            account: { operation: "bind", id: item.id, ...v },
          });
          workflow.throwIfAborted();
          await presentAccountResult(
            ctx,
            item,
            (clear) => openAccountBindingTask(ctx, item, id, bind, clear),
            editor.active,
            workflow,
          );
        },
      ),
    );
  return { edit, bind, bindingFields };
}
