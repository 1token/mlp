// @ts-check
/**
 * lib/html.js — frameworkless rendering discipline (S3.9 §2, D-167).
 *
 * The `html` tagged template escapes EVERY interpolation; raw
 * insertion exists only through the explicit {@link unsafe} wrapper —
 * greppable, reviewable, rare. This convention is the app-chrome XSS
 * defense (Body content never passes through here at all: it goes
 * through lib/sanitizer.js inside <mlp-body-viewer>).
 *
 * `reconcile` is the ~40-line keyed child reconciler: match by key,
 * move/insert/remove — no virtual DOM, just list stitching.
 */

/** Marks a string as already-safe HTML. Use sparingly. */
export class Unsafe {
  /** @param {string} value */
  constructor(value) { this.value = value; }
}

/** @param {string} value */
export const unsafe = (value) => new Unsafe(value);

/** @param {string} s */
export function escapeHtml(s) {
  return s
    .replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

/**
 * @param {TemplateStringsArray} strings
 * @param {...unknown} values
 * @returns {string}
 */
export function html(strings, ...values) {
  let out = strings[0];
  for (let i = 0; i < values.length; i++) {
    const v = values[i];
    if (v instanceof Unsafe) out += v.value;
    else if (Array.isArray(v)) out += v.map((x) => x instanceof Unsafe ? x.value : escapeHtml(String(x ?? ''))).join('');
    else out += escapeHtml(String(v ?? ''));
    out += strings[i + 1];
  }
  return out;
}

/**
 * Keyed list reconciliation (D-167): container children carry
 * data-key; items map through key/create/update. Creates are
 * appended in order, keeps are moved into position, leftovers are
 * removed.
 *
 * @template T
 * @param {HTMLElement} container
 * @param {T[]} items
 * @param {(item: T) => string} keyOf
 * @param {(item: T) => HTMLElement} create
 * @param {(el: HTMLElement, item: T) => void} update
 */
export function reconcile(container, items, keyOf, create, update) {
  /** @type {Map<string, HTMLElement>} */
  const existing = new Map();
  for (const child of Array.from(container.children)) {
    const el = /** @type {HTMLElement} */ (child);
    const k = el.dataset.key;
    if (k !== undefined) existing.set(k, el);
  }
  /** @type {globalThis.Node | null} */
  let cursor = container.firstChild;
  for (const item of items) {
    const key = keyOf(item);
    let el = existing.get(key);
    if (el) {
      existing.delete(key);
      update(el, item);
    } else {
      el = create(item);
      el.dataset.key = key;
    }
    if (el !== cursor) container.insertBefore(el, cursor);
    else cursor = cursor.nextSibling;
    if (el === cursor) cursor = el.nextSibling;
  }
  for (const leftover of existing.values()) leftover.remove();
}
