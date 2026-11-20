export function validationLabel(value) {
  if (!value?.state) return "尚未验证";
  if (value.state === "pending") return "兼容性已分析 · 下载时校验配置";
  if (value.state === "failed") return "验证失败";
  if (value.state === "unavailable") return "校验器未安装";
  if (value.state === "passed" && value.scope === "core-config")
    return `${value.engine === "mihomo" ? "Mihomo" : "sing-box"} 格式通过`;
  if (value.state === "passed" && value.scope === "parser")
    return "格式往返检查通过";
  return "已生成 · 应用导入待验证";
}
