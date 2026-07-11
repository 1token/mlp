// @ts-check
/**
 * app/mlp-inbox.js — the S3.2 thread list, S4.9 scope: the flat
 * rolled-up rows (subject, sender, unread weight, media chips) with
 * keyed reconciliation (D-167) and the triage trio feeding the undo
 * bar (D-129). Bundles, sections, hoisting, and sweep are the S4.11
 * refinement.
 */

import { html } from '../lib/html.js';
import { reconcile } from '../lib/html.js';
import { store } from '../store/store.js';
import { api } from '../store/api.js';

/** @typedef {import('../store/store.js').ThreadRow} ThreadRow */

export class MlpInbox extends HTMLElement {
  connectedCallback() {
    this.unsub = store.subscribe('inbox', () => this.render());
    this.render();
  }

  disconnectedCallback() { this.unsub?.(); }

  render() {
    if (!this.list) {
      this.innerHTML = '<div class="thread-list" role="list"></div>';
      this.list = /** @type {HTMLElement} */ (this.querySelector('.thread-list'));
    }
    const rows = store.state.inbox.threads;
    reconcile(this.list, rows,
      (t) => String(t.id),
      (t) => this.createRow(t),
      (el, t) => this.updateRow(el, t));
    if (rows.length === 0) this.list.innerHTML = '<p>Nothing here — a calm inbox.</p>';
  }

  /** @param {ThreadRow} t */
  createRow(t) {
    const el = document.createElement('div');
    el.className = 'thread-row';
    el.setAttribute('role', 'listitem');
    el.addEventListener('click', (e) => {
      if (/** @type {HTMLElement} */ (e.target).tagName === 'BUTTON') return;
      store.update('nav', { openThread: t.id });
    });
    this.updateRow(el, t);
    return el;
  }

  /**
   * Leaf rows re-render wholesale from state (S3.9 §2).
   * @param {HTMLElement} el
   * @param {ThreadRow} t
   */
  updateRow(el, t) {
    el.classList.toggle('unread', t.unread > 0);
    const chips = Object.entries(t.media ?? {})
      .map(([state, n]) => html`<span class="chip">${n} ${state}</span>`)
      .join(' ');
    el.innerHTML = html`
      <span class="who">${t.rollup?.last_author ?? ''}</span>
      <span class="subject">${t.rollup?.subject ?? '(no subject)'}</span>
      <span class="chips"></span>
      <button data-op="flag" title="Flag">${t.flagged ? '★' : '☆'}</button>
      <button data-op="done" title="Done">✓</button>`;
    const chipHost = el.querySelector('.chips');
    if (chipHost) chipHost.innerHTML = chips;
    for (const btn of el.querySelectorAll('button[data-op]')) {
      btn.addEventListener('click', () => this.triage(t, /** @type {'flag'|'done'} */ (btn.getAttribute('data-op'))));
    }
  }

  /**
   * @param {ThreadRow} t
   * @param {'flag' | 'done'} op
   */
  async triage(t, op) {
    const value = op === 'flag' ? !t.flagged : true;
    const res = await api.triage(t.id, op, value);
    store.update('undo', {
      undo: { token: res.undo_token, label: op === 'done' ? 'Marked done' : (value ? 'Flagged' : 'Unflagged') },
    });
    const data = await api.threads(store.state.inbox.view);
    store.update('inbox', { threads: data.threads ?? [] });
  }
}

customElements.define('mlp-inbox', MlpInbox);
