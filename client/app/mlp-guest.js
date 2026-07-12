// @ts-check
/**
 * app/mlp-guest.js — `<mlp-guest-page>`, the guest delivery surface
 * (S3.6, D-151–D-155): the second host of the one Body viewer. The
 * capability token is the URL path; the optional PIN arrives from
 * the guest's second channel (D-152) and travels only in the
 * X-MLP-Guest-PIN header. Views are never reported to anyone
 * (D-147 — this page simply has no such wire). The claim (D-154)
 * turns possession of link + PIN into a mailbox: the delivery
 * re-dispatches server-side, a session cookie lands, and the page
 * offers passkey registration (D-161) before handing over to the
 * app shell.
 *
 * Vanilla ES6, no build step (D-116); JSDoc-typed (D-117).
 */

import { html } from '../lib/html.js';
import '../app/mlp-body-viewer.js';

/**
 * @typedef {{ urn: string, name?: string, size: number, type: string,
 *   preview_of?: string }} GuestFile
 * @typedef {{ subject?: string, author: string, created: string,
 *   body: { profile: string, content: string, degraded?: boolean },
 *   derived_text?: string, files: GuestFile[], domain: string,
 *   claimed_as?: string }} GuestPayload
 */

const token = location.pathname.replace(/^\/g\//, '');
const api = `/api/v1/guest/${encodeURIComponent(token)}`;

class MlpGuestPage extends HTMLElement {
  constructor() {
    super();
    /** @type {string} */ this.pin = '';
    /** @type {GuestPayload | null} */ this.payload = null;
    /** @type {string} */ this.notice = '';
  }

  connectedCallback() { this.load(); }

  /** @param {string} path @param {RequestInit} [init] */
  async fetchGuest(path, init) {
    const headers = new Headers(init && init.headers || {});
    if (this.pin) headers.set('X-MLP-Guest-PIN', this.pin);
    if (init && init.method && init.method !== 'GET') headers.set('X-MLP-Client', 'guest-page');
    return fetch(api + path, { ...init, headers });
  }

  async load() {
    const resp = await this.fetchGuest('');
    if (resp.status === 401) {
      const prob = await resp.json().catch(() => ({}));
      this.renderPINPrompt(prob.title === 'pin-invalid' ? 'Wrong PIN — check with the sender.' : '');
      return;
    }
    if (!resp.ok) {
      const prob = await resp.json().catch(() => ({}));
      this.renderProblem(resp.status, prob);
      return;
    }
    this.payload = /** @type {GuestPayload} */ (await resp.json());
    this.render();
  }

  /** @param {string} error */
  renderPINPrompt(error) {
    this.innerHTML = html`
      <main class="guest">
        <h1>This delivery is PIN-protected</h1>
        <p>The sender conveyed the PIN to you separately.</p>
        <p class="error">${error}</p>
        <input type="password" inputmode="numeric" autocomplete="one-time-code"
               maxlength="6" aria-label="PIN">
        <button class="primary">Open</button>
      </main>`;
    const input = /** @type {HTMLInputElement} */ (this.querySelector('input'));
    const open = () => { this.pin = input.value.trim(); this.load(); };
    this.querySelector('button')?.addEventListener('click', open);
    input.addEventListener('keydown', (e) => { if (e.key === 'Enter') open(); });
    input.focus();
  }

  /** @param {number} status @param {{ detail?: string }} prob */
  renderProblem(status, prob) {
    const gone = status === 410;
    const locked = status === 423;
    this.innerHTML = html`
      <main class="guest">
        <h1>${gone ? 'This link is no longer active' : locked ? 'Too many PIN attempts' : 'Something went wrong'}</h1>
        <p>${prob.detail || (gone ? 'Ask the sender for a fresh link.' : '')}</p>
      </main>`;
  }

  render() {
    const p = this.payload;
    if (!p) return;
    const kb = (/** @type {number} */ n) => n >= 1048576
      ? (n / 1048576).toFixed(1) + ' MiB' : Math.max(1, Math.round(n / 1024)) + ' KiB';
    this.innerHTML = html`
      <main class="guest">
        <header>
          <h1>${p.subject || '(no subject)'}</h1>
          <p class="meta">from <strong>${p.author}</strong> · ${p.created}
            · delivered by ${p.domain}</p>
        </header>
        <section class="body-host"></section>
        <section class="files" aria-label="Files"></section>
        <section class="claim"></section>
      </main>`;

    const viewer = /** @type {any} */ (document.createElement('mlp-body-viewer'));
    viewer.manifest = p.files.map((f) => f.urn);
    viewer.resolveUrn = (/** @type {string} */ urn) =>
      api + '/o/' + encodeURIComponent(urn);
    viewer.content = p.body.degraded ? '' : p.body.content;
    if (p.body.degraded && p.derived_text) viewer.degraded = p.derived_text;
    this.querySelector('.body-host')?.append(viewer);

    const files = /** @type {HTMLElement} */ (this.querySelector('.files'));
    const masters = p.files.filter((f) => !f.preview_of ||
      !p.files.some((m) => m.urn === f.preview_of));
    for (const f of masters) {
      const row = document.createElement('div');
      row.className = 'file-row';
      row.innerHTML = html`
        <span class="name">${f.name || f.urn.slice(9, 21)}</span>
        <span class="size">${kb(f.size)}</span>
        <a class="download" href="${api + '/o/' + encodeURIComponent(f.urn)}"
           download>Download</a>`;
      // The PIN header cannot ride an <a download>: fetch + blob.
      row.querySelector('a')?.addEventListener('click', async (e) => {
        if (!this.pin) return; // plain navigation works PIN-less
        e.preventDefault();
        const resp = await this.fetchGuest('/o/' + encodeURIComponent(f.urn));
        if (!resp.ok) return;
        const url = URL.createObjectURL(await resp.blob());
        const a = document.createElement('a');
        a.href = url; a.download = f.name || 'download';
        a.click();
        URL.revokeObjectURL(url);
      });
      files.append(row);
    }

    this.renderClaim();
  }

  renderClaim() {
    const host = /** @type {HTMLElement} */ (this.querySelector('.claim'));
    const p = /** @type {GuestPayload} */ (this.payload);
    if (p.claimed_as) {
      host.innerHTML = html`
        <p>This delivery was claimed as <strong>${p.claimed_as}</strong>.
          The link keeps working. <a href="/">Open your inbox</a></p>`;
      return;
    }
    host.innerHTML = html`
      <h2>Keep this — and everything sent to you next</h2>
      <p>Claim a free address at ${p.domain}. Your files are already
        here, so they are yours the moment you do.</p>
      <p class="error"></p>
      <input type="text" aria-label="pick a name" placeholder="yourname"
             pattern="[a-z0-9][a-z0-9._-]*">
      <button class="primary">Claim @${p.domain}</button>`;
    this.querySelector('.claim button')?.addEventListener('click', () => this.claim());
  }

  async claim() {
    const input = /** @type {HTMLInputElement} */ (this.querySelector('.claim input'));
    const errEl = /** @type {HTMLElement} */ (this.querySelector('.claim .error'));
    const resp = await this.fetchGuest('/claim', {
      method: 'POST',
      body: JSON.stringify({ local_part: input.value.trim().toLowerCase() }),
    });
    const body = await resp.json().catch(() => ({}));
    if (!resp.ok) {
      errEl.textContent = body.detail || 'That did not work.';
      return;
    }
    // The session cookie is set; offer a passkey (D-161), then the app.
    const host = /** @type {HTMLElement} */ (this.querySelector('.claim'));
    host.innerHTML = html`
      <h2>Welcome, ${body.address}</h2>
      <p>Your delivery is in your inbox — the files were already here.</p>
      <button class="primary">Add a passkey</button>
      <a href="/">Skip for now → inbox</a>
      <p class="error"></p>`;
    host.querySelector('button')?.addEventListener('click', () => this.registerPasskey());
  }

  async registerPasskey() {
    const errEl = /** @type {HTMLElement} */ (this.querySelector('.claim .error'));
    try {
      const begin = await (await fetch('/api/v1/webauthn/register/begin', {
        method: 'POST', headers: { 'X-MLP-Client': 'guest-page' }, body: '{}',
      })).json();
      const b64 = (/** @type {string} */ s) =>
        Uint8Array.from(atob(s.replace(/-/g, '+').replace(/_/g, '/')), (c) => c.charCodeAt(0));
      const toB64url = (/** @type {ArrayBuffer} */ buf) =>
        btoa(String.fromCharCode(...new Uint8Array(buf)))
          .replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
      const cred = /** @type {any} */ (await navigator.credentials.create({
        publicKey: {
          challenge: b64(begin.challenge),
          rp: begin.rp,
          user: { ...begin.user, id: b64(begin.user.id) },
          pubKeyCredParams: begin.pubKeyCredParams,
          attestation: 'none',
        },
      }));
      const resp = await fetch('/api/v1/webauthn/register/finish', {
        method: 'POST', headers: { 'X-MLP-Client': 'guest-page' },
        body: JSON.stringify({
          challenge: begin.challenge,
          client_data_json: toB64url(cred.response.clientDataJSON),
          attestation_object: toB64url(cred.response.attestationObject),
        }),
      });
      if (!resp.ok) throw new Error('registration refused');
      location.href = '/';
    } catch (err) {
      errEl.textContent = 'Passkey setup failed — you can add one later in Settings.';
    }
  }
}

customElements.define('mlp-guest-page', MlpGuestPage);
