import { api } from './api.js';

/** @param {import('./contracts.js').AccountSave} value
 * @param {typeof api} [request] @returns {Promise<{ok: boolean}>} */
export const saveAccount = (value, request = api) => request('/api/accounts', value);
/** @param {import('./contracts.js').AccountArchive} value
 * @param {typeof api} [request] @returns {Promise<{ok: boolean}>} */
export const archiveAccount = (value, request = api) => request('/api/accounts/archive', value);
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
export const createShare = (value, request = api) => request('/api/grants', value);
