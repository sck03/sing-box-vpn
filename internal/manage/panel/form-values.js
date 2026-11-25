// DNS resolver fields accept a tag or sing-box's structured resolver options.
export function resolverValue(text) {
  const value = text.trim();
  if (!value.startsWith("{")) return value;
  let resolver;
  try {
    resolver = JSON.parse(value);
  } catch {
    throw new Error("解析器高级配置需要有效的 JSON 对象");
  }
  if (
    !resolver ||
    typeof resolver.server !== "string" ||
    !resolver.server.trim()
  )
    throw new Error("解析器高级配置需要非空的 server DNS tag");
  return resolver;
}
