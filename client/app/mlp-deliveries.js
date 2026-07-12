// @ts-check
/**
 * app/mlp-deliveries.js — the Deliveries lens (S3.5, D-145–D-150,
 * S4.11 scope): the sender's list with per-target headlines and the
 * detail view — per-domain recipient/media matrices from the latest
 * snapshots and the D-149 protocol-fact timeline. Real facts,
 * plainly shown; no invented statuses.
 */

import { html } from '../lib/html.js';
import { api, ApiError } from '../store/api.js';

/**
 * @typedef {{ id: number, job_tag: string, created: string,
 *   targets: { domain: string, envelope_id: string, headline: string,
 *     recipients?: Record<string, unknown>[], media?: Record<string, unknown>[] }[] }} Delivery
 */

export class MlpDeliveries extends HTMLElement {
  constructor() {
    super();
    /** @type {Delivery[]} */
    this.rows = [];
    /** @type {number | null} */
    this.open = null;
  }

  connectedCallback() { this.load(); }

  async load() {
    try {
      const data = await api.deliveries();
      this.rows = data.deliveries ?? [];
      this.render();
    } catch (e) {
      this.innerHTML = html`<p role="alert">${e instanceof ApiError ? e.message : 'failed to load'}</p>`;
    }
  }

  render() {
    if (this.rows.length === 0) {
      this.innerHTML = '<p>Nothing sent yet.</p>';
      return;
    }
    this.innerHTML = html`<div class="delivery-list" role="list"></div>`;
    const list = /** @type {HTMLElement} */ (this.querySelector('.delivery-list'));
    for (const d of this.rows) {
      const row = document.createElement('div');
      row.className = 'thread-row';
      row.setAttribute('role', 'listitem');
      const headlines = d.targets.map((t) => html`<span class="chip">${t.domain}: ${t.headline}</span>`).join(' ');
      row.innerHTML = html`
        <span class="who">${d.job_tag || '(untagged)'}</span>
        <span class="subject"><time>${d.created}</time></span>
        <span class="chips"></span>`;
      const chips = row.querySelector('.chips');
      if (chips) chips.innerHTML = headlines;
      row.addEventListener('click', () => this.toggle(d.id));
      list.append(row);
      if (this.open === d.id) list.append(this.detailHost(d.id));
    }
  }

  /** @param {number} id */
  toggle(id) {
    this.open = this.open === id ? null : id;
    this.render();
  }

  /** @param {number} id */
  detailHost(id) {
    const host = document.createElement('div');
    host.className = 'delivery-detail';
    host.textContent = 'Loading…';
    this.detail(host, id);
    return host;
  }

  /**
   * @param {HTMLElement} host
   * @param {number} id
   */
  async detail(host, id) {
    try {
      const [detail, tl] = await Promise.all([api.delivery(id), api.timeline(id)]);
      let out = '';
      for (const t of detail.targets ?? []) {
        const media = (t.media ?? [])
          .map((/** @type {Record<string, unknown>} */ m) => html`<li>${String(m.urn ?? '').slice(9, 21)} — ${String(m.verdict ?? '')}</li>`)
          .join('');
        const recipients = (t.recipients ?? [])
          .map((/** @type {Record<string, unknown>} */ r) => html`<li>${String(r.addr ?? '')} — ${String(r.verdict ?? '')}</li>`)
          .join('');
        out += html`<h3>${t.domain}</h3><ul>` + recipients + media + '</ul>';
      }
      out += '<h3>Timeline</h3><ul>' + (tl.timeline ?? [])
        .map((/** @type {{at: string, kind: string}} */ e) => html`<li><time>${e.at}</time> ${e.kind}</li>`)
        .join('') + '</ul>';
      host.innerHTML = out;
    } catch (e) {
      host.textContent = e instanceof ApiError ? e.message : 'failed to load detail';
    }
  }
}

customElements.define('mlp-deliveries', MlpDeliveries);
