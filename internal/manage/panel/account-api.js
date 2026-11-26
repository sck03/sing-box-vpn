import { api, mutate } from './api.js';

/** @param {import('./contracts.js').AccountSave} value
 * @param {typeof api} [request] @returns {Promise<{ok: boolean}>} */
export const saveAccount = (value, request = api) => mutate('/api/accounts', value, undefined, request);
/** @param {import('./contracts.js').AccountArchive} value
 * @param {typeof api} [request] @returns {Promise<{ok: boolean}>} */
export const archiveAccount = (value, request = api) => mutate('/api/accounts/archive', value, undefined, request);
/** @param {string} scope @param {AbortSignal} [signal]
 * @param {typeof api} [request] @returns {Promise<import('./contracts.js').Option[]>} */
export const inboundOptions = (scope, signal, request = api) => request('/api/inbound-options?scope=' + encodeURIComponent(scope), undefined, { signal });
/** @param {AbortSignal} [signal] @param {typeof api} [request]
 * @returns {Promise<import('./contracts.js').Option[]>} */
export const accountOptions = (signal, request = api) => request('/api/account-options', undefined, { signal });
/** @param {string} id @param {string} scope @param {AbortSignal} [signal]
 * @param {typeof api} [request] @returns {Promise<import('./contracts.js').CredentialSummary[]>} */
export const accountCredentials = (id, scope, signal, request = api) => request('/api/credentials?' + new URLSearchParams({ id, scope }), undefined, { signal });
/** @param {import('./contracts.js').Grant} value @param {typeof api} [request]
 * @returns {Promise<import('./contracts.js').ShareCreated>} */
export const createShare = (value, request = api) => mutate('/api/grants', value, undefined, request);
