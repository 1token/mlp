// @ts-check
/**
 * lib/sanitizer.js — the mlp-html/1 sanitization pipeline (spec §11,
 * D-91–D-96). Conformance anchor: the TV-005 corpus under parsed-tree
 * equality (D-94), which is this module's cross-language bridge to
 * the Python prototype sanitizer.
 *
 * The module is deliberately DOM-free: it operates on a plain tree
 * (see {@link Node}) so the same logic runs in the browser (fed by
 * the platform HTML5 parser — §11.5 step 1 requires spec-compliant
 * HTML5 tree construction, which in a browser is the native parser)
 * and under the node test harness (fed by a test-only parser). The
 * pipeline itself — walk, filter, caps, fixpoint, degradation — is
 * identical in both.
 *
 * No frameworks, no build step, ES6 module (D-116/D-117).
 */

/**
 * @typedef {{ t: 'el', tag: string, attrs: Record<string, string>, kids: Node[] }} ElementNode
 * @typedef {{ t: 'tx', text: string }} TextNode
 * @typedef {{ t: 'comment', text: string }} CommentNode
 * @typedef {ElementNode | TextNode | CommentNode} Node
 */

// ---- §11.2 element tables --------------------------------------------

/**
 * Permitted elements → their element-specific attributes. `ol` and
 * `time` appear in the table's attribute column only; they are
 * permitted members of their groups (D-218 reading).
 * @type {Record<string, string[]>}
 */
const PERMITTED = {
  p: [], br: [], hr: [], h1: [], h2: [], h3: [], h4: [], h5: [], h6: [],
  blockquote: [], pre: [], code: [], div: [],
  ul: [], ol: ['start'], li: [], dl: [], dt: [], dd: [],
  em: [], strong: [], b: [], i: [], u: [], s: [], sub: [], sup: [],
  mark: [], small: [], q: [], abbr: [], dfn: [], kbd: [], samp: [],
  var: [], del: [], ins: [], wbr: [], span: [], time: ['datetime'],
  table: [], caption: [], thead: [], tbody: [], tfoot: [], tr: [],
  th: ['colspan', 'rowspan', 'scope'], td: ['colspan', 'rowspan'],
  a: ['href'],
  img: ['src', 'alt', 'width', 'height'],
  video: ['src', 'poster', 'width', 'height'],
  audio: ['src'], source: ['src', 'type'],
  figure: [], figcaption: [],
};

/** Drop-list: removed with their entire subtree (D-91). */
const DROP = new Set([
  'script', 'style', 'iframe', 'frame', 'frameset', 'object', 'embed',
  'applet', 'form', 'input', 'button', 'textarea', 'select', 'option',
  'optgroup', 'label', 'fieldset', 'legend', 'template', 'svg', 'math',
  'link', 'meta', 'base', 'noscript', 'slot', 'canvas', 'dialog',
  'map', 'area', 'marquee',
]);

/** Elements whose src must be a Manifest urn or the element is removed. */
const EMBEDS = new Set(['img', 'video', 'audio', 'source']);

/** HTML void elements (serialization, §11.5 step 3). */
export const VOID = new Set([
  'area', 'base', 'br', 'col', 'embed', 'hr', 'img', 'input', 'link',
  'meta', 'source', 'track', 'wbr',
]);

const INT_ATTRS = new Set(['width', 'height', 'colspan', 'rowspan', 'start']);
const RE_INT = /^[0-9]{1,6}$/;
const RE_ID = /^[A-Za-z][A-Za-z0-9_-]*$/;
const RE_CLASS = /^[\w\s-]{1,256}$/;
const RE_LANG = /^[A-Za-z0-9-]{1,35}$/;
const RE_FRAGMENT = /^#[A-Za-z][A-Za-z0-9_-]*$/;

// ---- §11.3 style ------------------------------------------------------

const STYLE_PROPS = new Set([
  'color', 'background-color', 'font-size', 'font-weight', 'font-style',
  'font-family', 'text-align', 'text-decoration', 'line-height',
  'letter-spacing',
  'margin', 'margin-top', 'margin-right', 'margin-bottom', 'margin-left',
  'padding', 'padding-top', 'padding-right', 'padding-bottom', 'padding-left',
  'border', 'border-top', 'border-right', 'border-bottom', 'border-left',
  'border-width', 'border-style', 'border-color', 'border-radius',
  'width', 'max-width', 'height', 'max-height',
  'display', 'flex-direction', 'justify-content', 'align-items', 'gap',
  'vertical-align', 'white-space', 'overflow-wrap', 'word-break',
  'list-style-type', 'object-fit',
]);

/** The load-bearing value grammar: no functional notation, no
 * parentheses, quotes, slashes, or `!` (D-93). */
const RE_STYLE_VALUE = /^[A-Za-z0-9#%.,\s-]+$/;
const DISPLAY_VALUES = new Set(['block', 'inline', 'inline-block', 'flex']);
const STYLE_MAX = 2048;

/**
 * Filters one style attribute value; returns the surviving
 * serialization or null when nothing (or too much) survives.
 * @param {string} value
 * @returns {string | null}
 */
export function filterStyle(value) {
  if (value.length > STYLE_MAX) return null; // dropped whole (§11.3)
  /** @type {string[]} */
  const out = [];
  for (const decl of value.split(';')) {
    const colon = decl.indexOf(':');
    if (colon < 0) continue;
    const prop = decl.slice(0, colon).trim().toLowerCase();
    const val = decl.slice(colon + 1).trim();
    if (!STYLE_PROPS.has(prop) || val === '' || !RE_STYLE_VALUE.test(val)) continue;
    if (prop === 'display' && !DISPLAY_VALUES.has(val)) continue;
    out.push(prop + ':' + val);
  }
  return out.length ? out.join(';') : null;
}

// ---- §11.4 URLs -------------------------------------------------------

/**
 * @param {string} href
 * @param {Set<string>} manifest
 */
function hrefAllowed(href, manifest) {
  if (/^https:\/\//i.test(href)) return true;
  if (/^mailto:/i.test(href)) return true;
  if (href.startsWith('urn:mlet:')) return manifest.has(href);
  return RE_FRAGMENT.test(href);
}

/**
 * @param {string} src
 * @param {Set<string>} manifest
 */
function srcAllowed(src, manifest) {
  return src.startsWith('urn:mlet:') && manifest.has(src);
}

// ---- §11.5 the pipeline ----------------------------------------------

export const MAX_DEPTH = 64;
export const MAX_NODES = 16384;

/**
 * Counts nodes and maximum depth of a fragment.
 * @param {Node[]} nodes
 * @returns {{ count: number, depth: number }}
 */
export function measure(nodes) {
  let count = 0, depth = 0;
  /** @param {Node[]} ns @param {number} d */
  const walk = (ns, d) => {
    if (d > depth) depth = d;
    for (const n of ns) {
      count++;
      if (n.t === 'el') walk(n.kids, d + 1);
    }
  };
  walk(nodes, 1);
  return { count, depth };
}

/**
 * §11.5 step 2: the depth-first filter. Pure tree→tree; never throws.
 * @param {Node[]} nodes
 * @param {Set<string>} manifest Manifest urn:mlet: set (§11.4).
 * @returns {Node[]}
 */
export function sanitizeTree(nodes, manifest) {
  /** @type {Node[]} */
  const out = [];
  for (const n of nodes) {
    if (n.t === 'tx') { out.push({ t: 'tx', text: n.text }); continue; }
    if (n.t !== 'el') continue; // comments, PIs: removed
    const tag = n.tag.toLowerCase();
    if (DROP.has(tag)) continue; // subtree decomposes (D-91)
    const kids = sanitizeTree(n.kids, manifest);
    if (!(tag in PERMITTED)) { out.push(...kids); continue; } // unwrap
    const attrs = filterAttrs(tag, n.attrs, manifest);
    if (attrs === null) continue; // embed with invalid/absent src (§11.4)
    out.push({ t: 'el', tag, attrs, kids });
  }
  return out;
}

/**
 * Attribute filtering for one permitted element (§§11.2–11.4).
 * Returns null when the element itself must be removed.
 * @param {string} tag
 * @param {Record<string, string>} attrs
 * @param {Set<string>} manifest
 * @returns {Record<string, string> | null}
 */
function filterAttrs(tag, attrs, manifest) {
  const specific = PERMITTED[tag];
  /** @type {Record<string, string>} */
  const out = {};
  for (const [rawName, value] of Object.entries(attrs)) {
    const name = rawName.toLowerCase();
    if (name === 'name') continue; // permitted nowhere (DOM clobbering)
    const isSpecific = specific.includes(name);
    switch (name) {
      case 'title': if (value !== '') out.title = value; break;
      case 'dir': if (value === 'ltr' || value === 'rtl' || value === 'auto') out.dir = value; break;
      case 'lang': if (RE_LANG.test(value)) out.lang = value; break;
      case 'class': if (RE_CLASS.test(value)) out.class = value; break;
      case 'id': if (RE_ID.test(value)) out.id = value; break;
      case 'style': {
        const s = filterStyle(value);
        if (s !== null) out.style = s;
        break;
      }
      case 'href':
        if (isSpecific && hrefAllowed(value, manifest)) out.href = value;
        break; // invalid href: attribute lost, text survives (§11.4)
      case 'src':
      case 'poster':
        if (isSpecific && srcAllowed(value, manifest)) out[name] = value;
        break;
      default:
        if (!isSpecific) break;
        if (INT_ATTRS.has(name)) {
          if (RE_INT.test(value)) out[name] = value;
        } else {
          out[name] = value; // alt, type, scope, datetime
        }
    }
  }
  // Embeds without a valid Manifest src are removed entirely (§11.4).
  if (EMBEDS.has(tag) && !('src' in out)) return null;
  if (tag === 'img' && !('alt' in out)) out.alt = ''; // §11.4
  return out;
}

// ---- serialization (§11.5 step 3) --------------------------------------

/** @param {string} s */
const escText = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
/** @param {string} s */
const escAttr = (s) => s.replace(/&/g, '&amp;').replace(/"/g, '&quot;');

/**
 * HTML5 fragment serialization of a tree.
 * @param {Node[]} nodes
 * @returns {string}
 */
export function serializeTree(nodes) {
  let out = '';
  for (const n of nodes) {
    if (n.t === 'tx') { out += escText(n.text); continue; }
    if (n.t !== 'el') continue;
    out += '<' + n.tag;
    for (const [k, v] of Object.entries(n.attrs)) out += ' ' + k + '="' + escAttr(v) + '"';
    out += '>';
    if (VOID.has(n.tag)) continue;
    out += serializeTree(n.kids) + '</' + n.tag + '>';
  }
  return out;
}

// ---- tree equality (D-94: never byte equality) --------------------------

/**
 * Parsed-tree equality: tags and text exact, attribute maps compared
 * unordered, children in order.
 * @param {Node[]} a
 * @param {Node[]} b
 * @returns {boolean}
 */
export function treesEqual(a, b) {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) {
    const x = a[i], y = b[i];
    if (x.t !== y.t) return false;
    if (x.t === 'tx') { if (x.text !== /** @type {TextNode} */ (y).text) return false; continue; }
    if (x.t === 'el') {
      const ye = /** @type {ElementNode} */ (y);
      if (x.tag !== ye.tag) return false;
      const ka = Object.keys(x.attrs), kb = Object.keys(ye.attrs);
      if (ka.length !== kb.length) return false;
      for (const k of ka) if (x.attrs[k] !== ye.attrs[k]) return false;
      if (!treesEqual(x.kids, ye.kids)) return false;
    }
  }
  return true;
}

// ---- the full checked pipeline (§11.5 steps 2–6) ------------------------

/**
 * @typedef {{ degraded: false, nodes: Node[] } |
 *           { degraded: true, text: string }} SanitizeResult
 */

/**
 * Runs the normative pipeline over an already-parsed fragment:
 * caps (step 5), filter (step 2), REQUIRED idempotence fixpoint
 * (step 4) via the caller's parser, degradation to derived text
 * (step 6). The parse callback closes the loop with the same parser
 * that produced `nodes` — the platform parser in the browser, the
 * harness parser under node.
 *
 * @param {Node[]} nodes parsed body-fragment tree
 * @param {string[]} manifest Manifest urn list
 * @param {(html: string) => Node[]} parseFragment
 * @param {(nodes: Node[]) => string} derivedText §11.6 projection
 * @returns {SanitizeResult}
 */
export function sanitizeChecked(nodes, manifest, parseFragment, derivedText) {
  const { count, depth } = measure(nodes);
  if (count > MAX_NODES || depth > MAX_DEPTH) {
    // Step 6: a hostile Body earns a boring one — text from the
    // parsed input (a pure text projection, safe by construction).
    return { degraded: true, text: derivedText(nodes) };
  }
  const set = new Set(manifest);
  const clean = sanitizeTree(nodes, set);
  // Step 4: verify the fixpoint; a serializer/parser disagreement is
  // the mutation-XSS seam, and the response is retreat, not hope.
  const reparsed = parseFragment(serializeTree(clean));
  const again = sanitizeTree(reparsed, set);
  if (!treesEqual(clean, again) || !treesEqual(clean, reparsed)) {
    return { degraded: true, text: derivedText(nodes) };
  }
  return { degraded: false, nodes: clean };
}
