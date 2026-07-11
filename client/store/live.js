// @ts-check
/**
 * store/live.js — the SSE feed (D-132). EventSource reconnects
 * itself and replays exactly: the browser sends Last-Event-ID on
 * reconnection and the server replays from the journal (S4.7
 * D-215) — no client bookkeeping required for resume correctness.
 */

import { store } from './store.js';
import { api } from './api.js';

/** @type {EventSource | null} */
let source = null;

export function connectLive() {
  if (source) return;
  source = new EventSource('/api/v1/events');
  source.addEventListener('thread.changed', refreshInbox);
  source.addEventListener('media.accepted', refreshInbox);
  source.onerror = () => { /* EventSource retries with Last-Event-ID */ };
}

export function disconnectLive() {
  source?.close();
  source = null;
}

async function refreshInbox() {
  try {
    const view = store.state.inbox.view;
    const data = await api.threads(view);
    store.update('inbox', { threads: data.threads ?? [], view });
  } catch { /* transient; the next event or navigation retries */ }
}
