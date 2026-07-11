// @ts-check
/**
 * app/mlp-body-viewer.js — `<mlp-body-viewer>`, the §11.7 isolation
 * boundary (D-115/D-168): the one shadow-DOM family, shared by the
 * app and the guest page (D-151).
 *
 * Pipeline (D-168): receives the server-derived render form (§11.5,
 * D-94), re-sanitizes client-side through lib/sanitizer.js — the
 * D-31 dual duty — using the PLATFORM HTML5 parser (a <template>
 * element parses in fragment context; §11.5 step 1), then builds the
 * shadow tree, mapping `urn:mlet:` sources to home-BS resolution
 * URLs at render time. DOM-ephemeral presentation, not artifact
 * mutation: the stored Signed Medialet is untouched (D-28/D-94).
 *
 * Attributes/properties:
 *   content   (property, string)   — the render-form HTML
 *   manifest  (property, string[]) — the Medialet's Manifest URNs
 *   resolveUrn (property, fn)      — urn → URL; default /bs/o/{urn}
 *
 * External links open with noopener/noreferrer semantics and carry
 * their destination in `title` (disclosure, §11.4); `urn:mlet:` links
 * dispatch a cancellable `mlp-open-urn` event — deliberate navigation
 * is consent (D-31), so the app decides what opening means. Degraded
 * bodies (§11.5 step 6) render as the derived text in a <pre>.
 *
 * Vanilla ES6, no build step (D-116); JSDoc-typed (D-117).
 */

import { sanitizeChecked, VOID } from '../lib/sanitizer.js';
import { derivedText } from '../lib/derived-text.js';

/** @typedef {import('../lib/sanitizer.js').Node} TreeNode */
/** @typedef {import('../lib/sanitizer.js').ElementNode} ElementNode */

/**
 * Platform HTML5 fragment parsing (§11.5 step 1): <template> content
 * parses in the body-fragment insertion mode without executing
 * anything, and the resulting DOM converts to the sanitizer's tree.
 * @param {string} html
 * @returns {TreeNode[]}
 */
function parsePlatform(html) {
  const t = document.createElement('template');
  t.innerHTML = html;
  return domToTree(t.content.childNodes);
}

/**
 * @param {NodeListOf<ChildNode>} nodes
 * @returns {TreeNode[]}
 */
function domToTree(nodes) {
  /** @type {TreeNode[]} */
  const out = [];
  for (const n of nodes) {
    if (n.nodeType === 3) { out.push({ t: 'tx', text: n.nodeValue ?? '' }); continue; }
    if (n.nodeType === 8) { out.push({ t: 'comment', text: n.nodeValue ?? '' }); continue; }
    if (n.nodeType !== 1) continue;
    const el = /** @type {Element} */ (n);
    /** @type {Record<string, string>} */
    const attrs = {};
    for (const a of el.attributes) attrs[a.name] = a.value;
    out.push({
      t: 'el', tag: el.tagName.toLowerCase(), attrs,
      kids: domToTree(el.childNodes),
    });
  }
  return out;
}

export class MlpBodyViewer extends HTMLElement {
  constructor() {
    super();
    /** @type {string} */
    this.content = '';
    /** @type {string[]} */
    this.manifest = [];
    /** @type {(urn: string) => string} */
    this.resolveUrn = (urn) => '/bs/o/' + encodeURIComponent(urn);
    this.attachShadow({ mode: 'open' });
  }

  connectedCallback() { this.render(); }

  render() {
    const shadow = /** @type {ShadowRoot} */ (this.shadowRoot);
    shadow.replaceChildren();
    const result = sanitizeChecked(
      parsePlatform(this.content), this.manifest, parsePlatform, derivedText);
    if (result.degraded) {
      const pre = document.createElement('pre');
      pre.textContent = result.text;
      shadow.append(pre);
      return;
    }
    shadow.append(...this.treeToDom(result.nodes));
  }

  /**
   * Builds real DOM from the sanitized tree. Attribute values are the
   * sanitizer's output verbatim except the render-time URL mapping;
   * setAttribute on a fresh tree cannot re-introduce structure.
   * @param {TreeNode[]} nodes
   * @returns {globalThis.Node[]}
   */
  treeToDom(nodes) {
    /** @type {globalThis.Node[]} */
    const out = [];
    for (const n of nodes) {
      if (n.t === 'tx') { out.push(document.createTextNode(n.text)); continue; }
      if (n.t !== 'el') continue;
      const el = document.createElement(n.tag);
      for (const [name, value] of Object.entries(n.attrs)) {
        if ((name === 'src' || name === 'poster') && value.startsWith('urn:mlet:')) {
          el.setAttribute(name, this.resolveUrn(value)); // render-time mapping ≠ rewrite (D-168)
          continue;
        }
        el.setAttribute(name, value);
      }
      if (n.tag === 'a') this.wireLink(/** @type {HTMLAnchorElement} */ (el), n);
      if (n.tag === 'video' || n.tag === 'audio') {
        // Clients MUST present playable media with controls and MUST
        // NOT autoplay (§11.2) — presentation is ours, not content's.
        el.setAttribute('controls', '');
      }
      if (!VOID.has(n.tag)) el.append(...this.treeToDom(n.kids));
      out.push(el);
    }
    return out;
  }

  /**
   * @param {HTMLAnchorElement} el
   * @param {ElementNode} node
   */
  wireLink(el, node) {
    const href = node.attrs.href ?? '';
    if (href.startsWith('urn:mlet:')) {
      el.removeAttribute('href');
      el.setAttribute('role', 'link');
      el.setAttribute('tabindex', '0');
      el.addEventListener('click', (e) => {
        e.preventDefault();
        this.dispatchEvent(new CustomEvent('mlp-open-urn', {
          detail: { urn: href }, bubbles: true, composed: true,
        }));
      });
      return;
    }
    if (/^https:\/\//i.test(href)) {
      el.setAttribute('target', '_blank');
      el.setAttribute('rel', 'noopener noreferrer');
      if (!el.hasAttribute('title')) el.setAttribute('title', href); // disclosure (§11.4)
    }
  }
}

customElements.define('mlp-body-viewer', MlpBodyViewer);
