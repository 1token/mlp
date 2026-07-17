// @ts-check
/**
 * app/mlp-search.js — the S4.21 results view over the S4.19 endpoint.
 * A pure renderer of the `search` slice: the shell owns the input and
 * the (debounced) fetching; this component only draws what the store
 * holds — grouped per message, newest first, one row per result with
 * its match doors (`message` body/subject vs `media` with the file
 * name) and bracket-marked snippets rendered as <mark>.
 */

import { html, unsafe, snippetHtml } from '../lib/html.js';
import { reconcile } from '../lib/html.js';
import { store } from '../store/store.js';

/** @typedef {import('../store/store.js').SearchResult} SearchResult */

export class MlpSearch extends HTMLElement {
  connectedCallback() {
    this.unsub = store.subscribe('search', () => this.render());
    this.render();
  }

  disconnectedCallback() { this.unsub?.(); }

  render() {
    if (!this.list) {
      this.innerHTML = '<p class="search-status" role="status"></p><div class="search-results" role="list"></div>';
      this.status = /** @type {HTMLElement} */ (this.querySelector('.search-status'));
      this.list = /** @type {HTMLElement} */ (this.querySelector('.search-results'));
    }
    const { q, results, loading } = store.state.search;
    const status = /** @type {HTMLElement} */ (this.status);
    status.textContent = loading ? 'Searching…'
      : results.length === 0 ? (q ? `Nothing matches “${q}”.` : '')
      : `${results.length} result${results.length === 1 ? '' : 's'} for “${q}”`;
    reconcile(/** @type {HTMLElement} */ (this.list), results,
      (r) => String(r.message_id),
      (r) => this.createRow(r),
      (el, r) => this.updateRow(el, r));
  }

  /** @param {SearchResult} r */
  createRow(r) {
    const el = document.createElement('div');
    el.className = 'search-row';
    el.setAttribute('role', 'listitem');
    el.addEventListener('click', () =>
      store.update('nav', { openThread: r.thread_id }));
    this.updateRow(el, r);
    return el;
  }

  /** @param {HTMLElement} el @param {SearchResult} r */
  updateRow(el, r) {
    const date = (r.received_at ?? '').slice(0, 10);
    const matches = r.matches.map((m) => html`<span class="search-hit">
        <span class="chip">${m.via === 'media' ? 'media: ' + (m.name ?? '') : 'message'}</span>
        ${unsafe(snippetHtml(m.snippet))}
      </span>`).join('');
    // The one raw insertion: snippetHtml escapes every character it
    // emits (the html`` discipline's greppable unsafe, D-167).
    el.innerHTML = html`
      <div class="search-head"><strong>${r.subject || '(no subject)'}</strong>
        <time datetime="${r.received_at}">${date}</time></div>
      ${unsafe(matches)}`;
  }
}

customElements.define('mlp-search', MlpSearch);
