import { api, mutate } from "./api.js";

/** @param {import('./contracts.js').InboundPreparation} input
 * @param {AbortSignal} [signal] @param {typeof api} [request] */
export const prepareInbound = (input, signal, request = api) =>
  mutate("/api/preset", input, signal, request);

/** @param {import('./contracts.js').CountryPreparation} input
 * @param {AbortSignal} [signal] @param {typeof api} [request] */
export const prepareCountry = (input, signal, request = api) =>
  mutate("/api/country", input, signal, request);

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
