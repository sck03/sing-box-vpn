import { api, mutate } from "./api.js";

/** @param {{q: string, offset: number, limit: number}} params
 * @param {AbortSignal} [signal] @param {typeof api} [request]
 * @returns {Promise<import('./contracts.js').GrantPage>} */
export const grantPage = (params, signal, request = api) =>
  request("/api/grants?" + new URLSearchParams({ q: params.q, offset: String(params.offset), limit: String(params.limit) }), undefined, { signal });

/** @param {string} id @param {typeof api} [request]
 * @returns {Promise<import('./contracts.js').ActionResult>} */
export const revokeGrant = (id, request = api) => mutate("/api/grants/revoke", { id }, undefined, request);
