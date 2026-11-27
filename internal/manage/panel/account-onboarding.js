import { api, getAccount } from "./api.js";
import { openTask } from "./tasks.js";
import { createAccountShare } from "./sharing.js";
import { dialogSession } from "./dialog-session.js";
import { retainDialogResult } from "./workflow-results.js";
import { $, el, help, row, button } from "./ui.js";

export async function presentAccountResult(
  ctx,
  account,
  display,
  active,
  signal,
) {
  signal.throwIfAborted();
  const result = retainDialogResult(
    `${account.name || account.id}：权益操作结果`,
    display,
    signal,
    "查看权益结果",
  );
  if (active() && !ctx.signal?.aborted) {
    const shown = result.show();
    ctx.render();
    await shown;
  }
}

export function accountCreated(account, bind, failure = "", clear = () => {}) {
  const session = dialogSession();
  session.replace(
    el("h2", {}, "权益已创建"),
    help(failure || "接下来绑定可用入站，再创建分享地址。"),
    row(
      button(
        "绑定入站",
        () => {
          clear();
          return bind(account, undefined);
        },
        "primary",
      ),
      button("稍后设置", () => {
        clear();
        session.close();
      }),
    ),
  );
}

// Show sharing only after the queued binding is confirmed. The account is
// persisted before queuing; retrying a failed binding never creates it again.
export async function openAccountBindingTask(
  ctx,
  account,
  taskID,
  bind,
  clear = () => {},
) {
  const workflow = Object.create(ctx);
  workflow.signal = undefined;
  return openTask(taskID, undefined, (task, signal) => {
    if (task.state !== "succeeded")
      return [
        button("重新绑定", () => {
          clear();
          return bind(account, undefined);
        }),
      ];
    return [
      button(
        "创建分享",
        async () => {
          const current = await getAccount(
            account.id,
            signal,
            ctx.workflowRequest || api,
          );
          if (signal.aborted) return;
          clear();
          await createAccountShare([current], workflow);
        },
        "primary",
      ),
      button("完成", () => {
        clear();
        $("#dialog").close();
      }),
    ];
  });
}
