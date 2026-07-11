// @ts-check
/**
 * app/mlp-thread.js — the open thread (S3.2/S3.4, S4.9 scope):
 * messages rendered through <mlp-body-viewer> (the TV-005-gated
 * pipeline — the server hands the verbatim body, the client is the
 * sanitization boundary), per-URN reference chips with accept on
 * offered media (D-141), read-on-open (S3.2 §5).
 */

import { html } from '../lib/html.js';
import { store } from '../store/store.js';
import { api, ApiError } from '../store/api.js';
import { MlpBodyViewer } from './mlp-body-viewer.js';

/**
 * @typedef {{ id: number, author?: string, subject?: string,
 *   created?: string, received_at: string, read: boolean,
 *   body?: { profile: string, content: string },
 *   manifest?: { urn: string }[],
 *   refs: Record<string, { state: string, name?: string, size: number }> }} Message
 */

export class MlpThread extends HTMLElement {
  connectedCallback() { this.load(); }

  get threadId() { return Number(this.getAttribute('thread-id')); }

  async load() {
    try {
      const data = await api.thread(this.threadId);
      /** @type {Message[]} */
      this.messages = data.messages ?? [];
      this.render();
      // Read-on-open (S3.2 §5): fire-and-forget; the undo bar is for
      // deliberate triage, not for exposure marking.
      if ((this.messages ?? []).some((m) => !m.read)) api.triage(this.threadId, 'read').catch(() => {});
    } catch (e) {
      this.innerHTML = html`<p role="alert">${e instanceof ApiError ? e.message : 'failed to load'}</p>`;
    }
  }

  render() {
    this.innerHTML = '';
    for (const m of this.messages ?? []) {
      const section = document.createElement('section');
      section.className = 'msg';
      section.innerHTML = html`
        <header><strong>${m.author ?? ''}</strong> · ${m.subject ?? ''} · <time>${m.created ?? m.received_at}</time></header>
        <div class="body-slot"></div>
        <footer class="refs"></footer>`;
      const viewer = /** @type {MlpBodyViewer} */ (document.createElement('mlp-body-viewer'));
      viewer.content = m.body?.content ?? '';
      viewer.manifest = (m.manifest ?? []).map((e) => e.urn);
      section.querySelector('.body-slot')?.append(viewer);
      this.renderRefs(/** @type {HTMLElement} */ (section.querySelector('.refs')), m);
      this.append(section);
    }
  }

  /**
   * @param {HTMLElement} host
   * @param {Message} m
   */
  renderRefs(host, m) {
    host.innerHTML = '';
    for (const [urn, ref] of Object.entries(m.refs ?? {})) {
      const chip = document.createElement('span');
      chip.className = 'chip';
      chip.innerHTML = html`${ref.name ?? urn.slice(9, 21)} · ${ref.state} `;
      if (ref.state === 'offered') {
        const btn = document.createElement('button');
        btn.textContent = 'Accept';
        btn.addEventListener('click', async () => {
          btn.disabled = true;
          try {
            await api.accept(urn);
            this.load();
          } catch (e) {
            btn.disabled = false;
            chip.append(' — ' + (e instanceof ApiError ? e.message : 'failed'));
          }
        });
        chip.append(btn);
      }
      host.append(chip, ' ');
    }
  }
}

customElements.define('mlp-thread', MlpThread);
