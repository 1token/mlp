// @ts-check
/**
 * app/mlp-composer.js — the composer (S3.3, S4.10 scope): recipients,
 * subject, profile-native body, attach-by-reference through the
 * hash-first declare (D-135 — the client computes/knows the urn; the
 * have-check answers instantly), draft autosave (D-138), and the
 * 10-second undo hold between "Send" and the signature moment — the
 * last instant anything can be unsaid (D-02). File drag-in with
 * client-side BLAKE3 joins in S4.11 with the vendored WASM (D-116).
 */

import { html } from '../lib/html.js';
import { store } from '../store/store.js';
import { api, ApiError } from '../store/api.js';
import { urnMletOfBlob } from '../lib/mlet-urn.js';

const HOLD_MS = 10_000;

export class MlpComposer extends HTMLElement {
  constructor() {
    super();
    /** @type {string | null} */
    this.draftId = null;
    /** @type {{ urn: string, size: number, name: string, type?: string }[]} */
    this.attached = [];
    /** @type {ReturnType<typeof setTimeout> | null} */
    this.holdTimer = null;
  }

  connectedCallback() { this.render(); }

  disconnectedCallback() {
    if (this.holdTimer !== null) clearTimeout(this.holdTimer);
  }

  render() {
    this.innerHTML = html`
      <section class="composer">
        <p><label>To <input id="to" placeholder="a@x.example, b@y.example"></label></p>
        <p><label>Subject <input id="subject"></label></p>
        <p><textarea id="body" rows="8" placeholder="<p>…</p>"></textarea></p>
        <p><label>Attach files <input id="attach-file" type="file" multiple></label></p>
        <p><label>Attach by reference (urn:mlet:…) <input id="attach-urn"></label>
           <input id="attach-size" type="number" placeholder="size" min="1">
           <button id="attach">Attach</button></p>
        <p class="attachments"></p>
        <p><button id="send">Send</button> <span id="status" role="status"></span></p>
      </section>`;
    this.querySelector('#attach')?.addEventListener('click', () => this.attach());
    this.querySelector('#attach-file')?.addEventListener('change', async (e) => {
      const input = /** @type {HTMLInputElement} */ (e.target);
      for (const file of input.files ?? []) await this.attachFile(file);
      input.value = '';
    });
    this.querySelector('#send')?.addEventListener('click', () => this.armSend());
    for (const id of ['to', 'subject', 'body']) {
      this.querySelector('#' + id)?.addEventListener('change', () => this.autosave());
    }
  }

  /** @param {string} id */
  fieldValue(id) {
    return /** @type {HTMLInputElement | HTMLTextAreaElement | null} */ (this.querySelector('#' + id))?.value ?? '';
  }

  draftDoc() {
    return {
      subject: this.fieldValue('subject'),
      body_content: this.fieldValue('body'),
      recipients: this.fieldValue('to').split(',').map((s) => s.trim()).filter(Boolean),
      manifest: this.attached.map((a) => ({
        urn: a.urn, size: a.size, type: a.type || 'application/octet-stream', name: a.name,
        available_until: new Date(Date.now() + 7 * 86400_000).toISOString().replace(/\.\d{3}Z$/, 'Z'),
      })),
    };
  }

  async autosave() {
    const doc = this.draftDoc();
    try {
      if (this.draftId === null) {
        const res = await api.draftCreate(doc);
        this.draftId = res.id;
      } else {
        await api.draftSave(this.draftId, doc);
      }
    } catch { /* offline: S3.9 queue posture; retried on next change */ }
  }

  /** The D-135 door, reference half: declare and attach when held. */
  async attach() {
    const urn = this.fieldValue('attach-urn').trim();
    const size = Number(this.fieldValue('attach-size'));
    const status = this.querySelector('#status');
    if (!urn.startsWith('urn:mlet:') || !Number.isFinite(size) || size < 1) {
      if (status) status.textContent = 'a urn:mlet: address and its size are required';
      return;
    }
    try {
      const res = await api.uploadDeclare(urn, size);
      if (res.have) {
        this.attached.push({ urn, size, name: urn.slice(9, 21) });
        this.renderAttachments();
        if (status) status.textContent = 'attached by reference — already in your store';
        this.autosave();
      } else if (status) {
        status.textContent = 'not in your store yet — pick the file itself to upload it';
      }
    } catch (e) {
      if (status) status.textContent = e instanceof ApiError ? e.message : 'attach failed';
    }
  }

  /**
   * The D-135 door, byte half: hash FIRST (the address is the
   * question), have-check, then stream chunks from the server's
   * confirmed offset — a reload resumes, never re-sends (D-244).
   * @param {File} file
   */
  async attachFile(file) {
    const status = this.querySelector('#status');
    const say = (/** @type {string} */ t) => { if (status) status.textContent = t; };
    try {
      say('hashing ' + file.name + '…');
      const urn = await urnMletOfBlob(file, (done, total) => {
        say(`hashing ${file.name}: ${Math.round((100 * done) / total)}%`);
      });
      const declared = await api.uploadDeclare(urn, file.size);
      if (!declared.have) {
        let offset = await api.uploadHead(declared.upload);
        const step = 4 * 1024 * 1024;
        while (offset < file.size) {
          const chunk = new Uint8Array(
            await file.slice(offset, offset + step).arrayBuffer());
          say(`uploading ${file.name}: ${Math.round((100 * offset) / file.size)}%`);
          offset = await api.uploadPatch(declared.upload, offset, chunk);
        }
      }
      this.attached.push({
        urn, size: file.size, name: file.name,
        type: file.type || 'application/octet-stream',
      });
      this.renderAttachments();
      say(declared.have
        ? 'attached — already in your store, nothing uploaded'
        : 'uploaded, verified by address, attached');
      this.autosave();
    } catch (e) {
      say(e instanceof ApiError ? e.message : 'upload failed — pick the file again to resume');
    }
  }

  renderAttachments() {
    const host = this.querySelector('.attachments');
    if (!host) return;
    host.innerHTML = this.attached.map((a) => html`<span class="chip">${a.name} · ${a.size} B</span> `).join('');
  }

  /** Send arms the 10 s hold (D-138); Undo disarms it. */
  async armSend() {
    await this.autosave();
    if (this.draftId === null) return;
    const send = /** @type {HTMLButtonElement | null} */ (this.querySelector('#send'));
    const status = this.querySelector('#status');
    if (send) send.disabled = true;
    if (status) status.textContent = 'Sending in 10 s…';
    const undo = document.createElement('button');
    undo.textContent = 'Undo';
    status?.append(' ', undo);
    const draftId = this.draftId;
    this.holdTimer = setTimeout(async () => {
      undo.remove();
      try {
        const result = await api.draftSend(draftId);
        if (status) status.textContent =
          `Sent — ${result.targets.map((/** @type {{domain: string, message: string}} */ t) => `${t.domain}: ${t.message}`).join(', ')}`;
        this.draftId = null;
        store.update('nav', { openThread: null });
      } catch (e) {
        if (status) status.textContent = e instanceof ApiError ? e.message : 'send failed';
        if (send) send.disabled = false;
      }
    }, HOLD_MS);
    undo.addEventListener('click', () => {
      if (this.holdTimer !== null) clearTimeout(this.holdTimer);
      this.holdTimer = null;
      undo.remove();
      if (status) status.textContent = 'Held — still a draft.';
      if (send) send.disabled = false;
    });
  }
}

customElements.define('mlp-composer', MlpComposer);
