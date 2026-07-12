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
 *   held: boolean, pinned: boolean, preview_of?: string, states: Record<string, number>,
 *   deliveries: number }} MediaCard
 */

export class MlpMedia extends HTMLElement {
  constructor() {
    super();
    /** @type {MediaCard[]} */
    this.cards = [];
  }

  /**
   * Fold preview cards into their master (MEP-002): a card whose
   * preview_of names another card in view is attached to that master
   * rather than shown as an independent asset.
   * @param {MediaCard[]} cards
   * @returns {{ master: MediaCard, preview: MediaCard | null }[]}
   */
  fold(cards) {
    const byURN = new Map(cards.map((c) => [c.urn, c]));
    /** @type {Set<string>} */
    const folded = new Set();
    /** @type {{ master: MediaCard, preview: MediaCard | null }[]} */
    const out = [];
    for (const c of cards) {
      if (c.preview_of && byURN.has(c.preview_of)) { folded.add(c.urn); }
    }
    for (const c of cards) {
      if (folded.has(c.urn)) continue;
      const preview = cards.find((p) => p.preview_of === c.urn) ?? null;
      out.push({ master: c, preview });
    }
    return out;
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
    for (const { master: c, preview } of this.fold(this.cards)) {
      const row = document.createElement('div');
      row.className = 'thread-row';
      row.setAttribute('role', 'listitem');
      const states = Object.entries(c.states)
        .map(([st, n]) => html`<span class="chip">${n} ${st}</span>`).join(' ');
      const previewNote = preview
        ? html`<span class="chip">preview ${preview.size} B</span>` : '';
      row.innerHTML = html`
        <span class="who">${c.name || c.urn.slice(9, 21)}</span>
        <span class="subject">${c.size} B · ${c.deliveries} deliveries ${c.held ? '· held' : ''}</span>
        <span class="chips"></span>
        <span class="actions"></span>`;
      const chipHost = row.querySelector('.chips');
      if (chipHost && previewNote) chipHost.insertAdjacentHTML('afterbegin', previewNote);
      const chips = row.querySelector('.chips');
      if (chips) chips.insertAdjacentHTML('beforeend', states);
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
