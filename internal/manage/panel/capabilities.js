import { api } from "./api.js";

const cached = new WeakMap();
export function capabilities(request = api) {
  if (!cached.has(request)) {
    const pending = request("/api/capabilities").catch((error) => {
      cached.delete(request);
      throw error;
    });
    cached.set(request, pending);
  }
  return cached.get(request);
}

export function validatePassword(value, policy) {
  const size = new TextEncoder().encode(value).length;
  if (size < policy.min || size > policy.max)
    throw new Error(
      `密码需要 ${policy.min}–${policy.max} 个 UTF-8 字节，当前为 ${size}。`,
    );
}
