// @ts-check
/**
 * app/mlp-app.js — the shell: login gate, inbox ↔ thread navigation,
 * the D-129 undo bar. Light DOM (the one shadow boundary is
 * <mlp-body-viewer>, D-115); renders from the store, mutates through
 * store methods (D-114).
 */

import { html, unsafe, escapeHtml } from '../lib/html.js';
import { store } from '../store/store.js';
import { api, ApiError } from '../store/api.js';
import { connectLive } from '../store/live.js';
import './mlp-inbox.js';
import './mlp-search.js';
import './mlp-thread.js';
import './mlp-composer.js';
import './mlp-deliveries.js';
import './mlp-media.js';

export class MlpApp extends HTMLElement {
  connectedCallback() {
    this.unsubs = [
      store.subscribe('auth', () => this.render()),
      store.subscribe('nav', () => this.render()),
      store.subscribe('undo', () => this.renderUndo()),
    ];
    this.render();
    this.bootstrap();
  }

  disconnectedCallback() { (this.unsubs ?? []).forEach((u) => u()); }

  /** Probe the session: a 401 shows the login gate. */
  async bootstrap() {
    try {
      const data = await api.threads('inbox');
      store.update('inbox', { threads: data.threads ?? [] });
      store.update('auth', { authenticated: true });
      connectLive();
    } catch (e) {
      if (e instanceof ApiError && e.code === 'auth-required') {
        store.update('auth', { authenticated: false });
      }
    }
  }

  render() {
    const { authenticated, openThread } = store.state;
    if (!authenticated) {
      this.innerHTML = html`
        <main>
          <h1>Medialet</h1>
          <p><label>Address <input id="addr" autocomplete="username"></label></p>
          <p><label>Password <input id="pw" type="password" autocomplete="current-password"></label></p>
          <p><button id="login">Sign in</button> <span id="err" role="alert"></span></p>
        </main>`;
      this.querySelector('#login')?.addEventListener('click', () => this.login());
      return;
    }
    const tab = store.state.tab;
    const nav = html`<nav>
      <button data-tab="inbox">Inbox</button>
      <button data-tab="junk">Junk</button>
      <button data-tab="deliveries">Deliveries</button>
      <button data-tab="media">Media</button>
      <button id="compose">Compose</button>
      <input id="q" type="search" placeholder="Search" aria-label="Search messages"
        autocomplete="off" value="${store.state.search.q}">
    </nav>`;
    if (openThread === 'compose') {
      this.innerHTML = html`<main><p><button id="back">← Back</button></p><h1>Compose</h1><mlp-composer></mlp-composer></main>`;
    } else if (openThread !== null) {
      this.innerHTML = html`<main><p><button id="back">← Back</button></p><mlp-thread thread-id="${openThread}"></mlp-thread></main>`;
    } else {
      const body = store.state.searching ? '<mlp-search></mlp-search>'
        : tab === 'deliveries' ? '<mlp-deliveries></mlp-deliveries>'
        : tab === 'media' ? '<mlp-media></mlp-media>'
        : '<mlp-inbox></mlp-inbox>';
      const title = store.state.searching ? 'Search' : tab[0].toUpperCase() + tab.slice(1);
      this.innerHTML = '<main>' + nav + `<h1>${title}</h1>` + body + '</main>';
      this.bindSearch();
    }
    this.querySelector('#back')?.addEventListener('click', () =>
      store.update('nav', { openThread: null }));
    this.querySelector('#compose')?.addEventListener('click', () =>
      store.update('nav', { openThread: 'compose' }));
    for (const btn of this.querySelectorAll('button[data-tab]')) {
      btn.addEventListener('click', async () => {
        const t = /** @type {'inbox'|'junk'|'deliveries'|'media'} */ (btn.getAttribute('data-tab'));
        store.update('nav', { tab: t, openThread: null });
        if (t === 'inbox' || t === 'junk') {
          try {
            const data = await api.threads(t);
            store.update('inbox', { threads: data.threads ?? [], view: t });
          } catch { /* transient */ }
        }
      });
    }
    this.renderUndo();
  }

  async login() {
    const addr = /** @type {HTMLInputElement | null} */ (this.querySelector('#addr'))?.value ?? '';
    const pw = /** @type {HTMLInputElement | null} */ (this.querySelector('#pw'))?.value ?? '';
    const err = this.querySelector('#err');
    try {
      const res = await api.login(addr, pw);
      store.update('auth', { authenticated: true, mailboxId: res.mailbox_id });
      this.bootstrap();
    } catch (e) {
      if (err) err.textContent = e instanceof ApiError ? e.message : 'sign-in failed';
    }
  }

  /**
   * S4.21 search wiring. Keystrokes touch only the `search` slice
   * (the shell does not subscribe to it, so the input keeps focus);
   * the empty <-> active transition alone goes through `nav`, which
   * swaps the body once — then we restore focus into the fresh input.
   */
  bindSearch() {
    const input = /** @type {HTMLInputElement | null} */ (this.querySelector('#q'));
    if (!input) return;
    if (this.refocusSearch) {
      input.focus();
      input.setSelectionRange(input.value.length, input.value.length);
      this.refocusSearch = false;
    }
    input.addEventListener('input', () => this.onSearchInput(input.value));
    input.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') { input.value = ''; this.onSearchInput('', true); }
      if (e.key === 'Enter') this.onSearchInput(input.value, true);
    });
  }

  /** @param {string} q @param {boolean} [now] skip the debounce */
  onSearchInput(q, now) {
    clearTimeout(this.searchTimer);
    const run = () => this.runSearch(q);
    if (now) run(); else this.searchTimer = setTimeout(run, 250);
  }

  /** @param {string} q */
  async runSearch(q) {
    const active = q.trim() !== '';
    store.update('search', active ? { q, loading: true } : { q: '', results: [], loading: false });
    if (active !== store.state.searching) {
      this.refocusSearch = active; // the swap recreates the input
      store.update('nav', { searching: active, openThread: null });
    }
    if (!active) return;
    try {
      const data = await api.search(q);
      if (store.state.search.q !== q) return; // a newer query superseded this one
      store.update('search', { results: data.results ?? [], loading: false });
    } catch (e) {
      if (store.state.search.q === q) store.update('search', { results: [], loading: false });
    }
  }

  /** The undo bar rides outside the view swap (D-129: ~30 s). */
  renderUndo() {
    this.querySelector('.undo-bar')?.remove();
    const undo = store.state.undo;
    if (!undo) return;
    const bar = document.createElement('div');
    bar.className = 'undo-bar';
    bar.innerHTML = html`${undo.label} <button>${unsafe(escapeHtml('Undo'))}</button>`;
    bar.querySelector('button')?.addEventListener('click', async () => {
      try { await api.undo(undo.token); } catch { /* expired: the bar goes anyway */ }
      store.update('undo', { undo: null });
    });
    this.append(bar);
    setTimeout(() => {
      if (store.state.undo?.token === undo.token) store.update('undo', { undo: null });
    }, 30_000);
  }
}

customElements.define('mlp-app', MlpApp);
