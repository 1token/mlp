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
import './mlp-thread.js';
import './mlp-composer.js';

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
    if (openThread === 'compose') {
      this.innerHTML = html`<main><p><button id="back">← Inbox</button></p><h1>Compose</h1><mlp-composer></mlp-composer></main>`;
    } else if (openThread === null) {
      this.innerHTML = html`<main><h1>Inbox <button id="compose">Compose</button></h1><mlp-inbox></mlp-inbox></main>`;
    } else {
      this.innerHTML = html`<main><p><button id="back">← Inbox</button></p><mlp-thread thread-id="${openThread}"></mlp-thread></main>`;
    }
    this.querySelector('#back')?.addEventListener('click', () =>
      store.update('nav', { openThread: null }));
    this.querySelector('#compose')?.addEventListener('click', () =>
      store.update('nav', { openThread: 'compose' }));
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
