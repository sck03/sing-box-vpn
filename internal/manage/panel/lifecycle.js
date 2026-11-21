import { api } from "./api.js";

// Reads belong to one render; user workflows survive refreshes but not logout.
export function pageContext(context, signal) {
  const view = Object.create(context);
  // Never inherit the parent's dispatcher: it delegates back to this view.
  view.updateOverview = null;
  view.signal = signal;
  view.request = (path, body, options = {}) => api(path, body, {
    ...options,
    signal: options.signal ? AbortSignal.any([signal, options.signal]) : signal,
  });
  view.workflowRequest = api;
  return view;
}
