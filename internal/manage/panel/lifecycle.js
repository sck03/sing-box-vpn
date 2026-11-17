import { api } from "./api.js";

// Reads belong to one render; user workflows survive refreshes but not logout.
export function pageContext(context, signal) {
  const view = Object.create(context);
  view.signal = signal;
  view.request = (path, body) => api(path, body, { signal });
  view.workflowRequest = api;
  return view;
}
