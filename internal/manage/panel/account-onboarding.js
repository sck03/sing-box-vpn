import { api, getAccount } from "./api.js";
import { openTask } from "./tasks.js";
import { createAccountShare } from "./sharing.js";
import { dialogSession } from "./dialog-session.js";
import { el, help, row, button } from "./ui.js";

export function accountCreated(account, bind, failure = "") {
  const session = dialogSession();
  session.replace(
    el("h2", {}, "权益已创建"),
    help(failure || "接下来绑定可用入站，再创建分享地址。"),
    row(button("绑定入站", () => bind(account, undefined), "primary"),
      button("稍后设置", session.close)),
  );
}

// Show sharing only after the queued binding is confirmed. The account is
// persisted before queuing; retrying a failed binding never creates it again.
export async function openAccountBindingTask(ctx, account, taskID, bind) {
  const workflow = Object.create(ctx);
  workflow.signal = undefined;
  return openTask(taskID, undefined, (task, signal) => {
    if (task.state !== "succeeded") return [button("重新绑定", () => bind(account, undefined))];
    return [button("创建分享", async () => {
      const current = await getAccount(account.id, signal, ctx.workflowRequest || api);
      if (signal.aborted) return;
      await createAccountShare([current], workflow);
    }, "primary")];
  });
}
