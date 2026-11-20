export function validationLabel(value) {
  if (!value?.state) return "尚未验证";
  if (value.state === "pending") return "兼容性已分析 · 下载时校验配置";
  if (value.state === "failed") return "验证失败";
  if (value.state === "unavailable") return "sing-box 配置检查不可用";
  if (value.state === "passed" && value.scope === "core-config")
    return "sing-box 配置检查通过";
  if (value.state === "passed" && value.scope === "parser")
    return "节点格式检查通过 · 客户端导入待验证";
  return "已生成 · 应用导入待验证";
}
