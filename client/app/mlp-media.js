// @ts-check
/**
 * app/mlp-media.js — the Media library (S3.7, D-156–D-160, S4.11
 * scope): one card per URN aggregating its references — held/pinned,
 * per-state counts, tombstones visible as unavailable counts (a
 * tombstone is a fact, not an apology). Pin protects from GC, never
 * from the owner (D-88); delete is the owner's and says so.
 */

import { html } from '../lib/html.js';
import { api, ApiError } from '../store/api.js';

/**
 * @typedef {{ urn: string, name: string, size: number, type: string,
 *   held: boolean, pinned: boolean, states: Record<string, number>,
 *   deliveries: number }} MediaCard
 */

export class MlpMedia extends HTMLElement {
  constructor() {
    super();
    /** @type {MediaCard[]} */
    this.cards = [];
  }

  connectedCallback() { this.load(); }

  async load() {
    try {
      const data = await api.media();
      this.cards = data.media ?? [];
      this.render();
    } catch (e) {
      this.innerHTML = html`<p role="alert">${e instanceof ApiError ? e.message : 'failed to load'}</p>`;
    }
  }

  render() {
    if (this.cards.length === 0) {
      this.innerHTML = '<p>No media references yet.</p>';
      return;
    }
    this.innerHTML = html`<div class="media-list" role="list"></div>`;
    const list = /** @type {HTMLElement} */ (this.querySelector('.media-list'));
    for (const c of this.cards) {
      const row = document.createElement('div');
      row.className = 'thread-row';
      row.setAttribute('role', 'listitem');
      const states = Object.entries(c.states)
        .map(([st, n]) => html`<span class="chip">${n} ${st}</span>`).join(' ');
      row.innerHTML = html`
        <span class="who">${c.name || c.urn.slice(9, 21)}</span>
        <span class="subject">${c.size} B · ${c.deliveries} deliveries ${c.held ? '· held' : ''}</span>
        <span class="chips"></span>
        <span class="actions"></span>`;
      const chips = row.querySelector('.chips');
      if (chips) chips.innerHTML = states;
      const actions = /** @type {HTMLElement} */ (row.querySelector('.actions'));
      if (c.states.available || c.states.pinned) {
        const pin = document.createElement('button');
        pin.textContent = c.pinned ? 'Unpin' : 'Pin';
        pin.addEventListener('click', async () => {
          try { await api.pin(c.urn, !c.pinned); this.load(); } catch { /* transition refused */ }
        });
        actions.append(pin);
      }
      if (c.held) {
        const del = document.createElement('button');
        del.textContent = 'Delete';
        del.title = 'The owner may destroy what GC may not (D-88)';
        del.addEventListener('click', async () => {
          try { await api.objectDelete(c.urn); this.load(); } catch { /* refused */ }
        });
        actions.append(del);
      }
      list.append(row);
    }
  }
}

customElements.define('mlp-media', MlpMedia);
