import { api } from "./api.js";

/** @param {AbortSignal} [signal] @param {typeof api} [request]
 * @returns {Promise<import('./contracts.js').OutboundOptions>} */
export const outboundOptions = (signal, request = api) =>
  request("/api/outbound-options", undefined, { signal });

/** @param {AbortSignal} [signal] @param {typeof api} [request]
 * @returns {Promise<import('./contracts.js').SubscriptionActionOptions>} */
export const subscriptionActions = (signal, request = api) =>
  request("/api/subscriptions/actions", undefined, { signal });

/** @param {AbortSignal} [signal] @param {typeof api} [request]
 * @returns {Promise<import('./contracts.js').GrantOptions>} */
export const grantOptions = (signal, request = api) =>
  request("/api/grant-options", undefined, { signal });
