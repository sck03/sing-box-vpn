import { api, getProgram } from "./api.js";
import { dialogSession } from "./dialog-session.js";
import { card, help, row, button, table, el, badge, formDialog } from "./ui.js";

const componentTable = (components) =>
  table(
    [
      { key: "name", label: "组件" },
      {
        key: "version",
        label: "当前版本",
        value: (item) => item.version || "无法读取",
      },
      { key: "expected", label: "本发行版验证版本" },
      {
        label: "配套状态",
        render: (item) =>
          badge(
            item.compatible ? "匹配" : "请检查安装",
            item.compatible ? "good" : "warn",
          ),
      },
    ],
    components,
    undefined,
    { controls: false },
  );

export async function programCard(ctx) {
  const current = await (ctx.request || api)("/api/program");
  const check = async () => {
    const session = dialogSession(ctx.signal);
    session.replace(
      el("h2", {}, "检查程序更新"),
      help("正在查询项目正式发行包…"),
    );
    try {
      const result = await getProgram(true, session.signal);
      if (!session.active()) return;
      session.replace(
        el("h2", {}, "程序更新"),
        help(
          "当前：" +
            result.current +
            (result.latest ? " · 最新正式版：" + result.latest : ""),
        ),
        componentTable(result.components),
        result.check_error
          ? help("检查未完成：" + result.check_error)
          : help(
              result.update_available
                ? "有可用更新。升级前将验证发行包摘要、组件版本和当前配置；组件一起切换，启动失败自动恢复。"
                : "没有更新的可验证正式发行包。",
            ),
        help(
          "证书签发组件随程序更新；证书本身在证书管理中申请 / 续期。升级期间连接会短暂中断。",
        ),
        result.release_url
          ? el(
              "a",
              {
                href: result.release_url,
                target: "_blank",
                rel: "noopener noreferrer",
              },
              "查看发行说明",
            )
          : null,
        row(
          button("关闭", session.close),
          ...(result.update_available && !result.check_error
            ? [
                button(
                  "更新到 " + result.latest,
                  async () => {
                    await ctx.task({
                      operation: "program-upgrade",
                      value: result.latest,
                    });
                    session.close();
                  },
                  "primary",
                ),
              ]
            : []),
        ),
      );
    } catch (error) {
      if (session.active()) session.fail(error);
    }
  };
  return card(
    "程序与组件更新",
    componentTable(current.components),
    help(
      "受管核心、官方配置校验器和证书签发组件由同一发行包管理。版本匹配后还需通过当前配置检查；客户端实际连接兼容性以验收记录为准。",
    ),
    row(
      button("检查更新", check, "primary"),
      button(
        "回滚受管运行时",
        async () => {
          if (confirm("验证并切换到上一份受管运行时？原活动实例会重启。"))
            await ctx.task({ operation: "runtime-rollback" });
        },
        "danger",
      ),
    ),
    el(
      "details",
      { class: "advanced" },
      el("summary", {}, "高级：指定发行版本"),
      help(
        "用于重新安装或选择明确的正式发行标签。仍会执行摘要、组件与当前配置校验，数据格式不匹配时拒绝更新。",
      ),
      button("指定发行版本", () =>
        formDialog(
          "指定程序发行版本",
          [{ key: "value", label: "发行标签（例如 v5.1.0）", required: true }],
          { value: "" },
          (value) =>
            ctx.task({
              operation: "program-upgrade",
              value: value.value.trim(),
            }),
        ),
      ),
    ),
    help(
      "回滚运行时仍会校验当前配置。完整程序更新失败时，面板、运行时和校验器将一起恢复。",
    ),
  );
}
