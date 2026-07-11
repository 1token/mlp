// @ts-check
/**
 * test/mini-html5-parser.js — TEST HARNESS ONLY.
 *
 * A small HTML fragment parser sufficient for the TV-005 corpus and
 * the sanitizer's own serializer output. It is NOT the production
 * parsing path and is NOT a spec-complete HTML5 parser: §11.5 step 1
 * requires spec-compliant HTML5 tree construction, which in the
 * shipping client is the platform parser (see app/mlp-body-viewer.js).
 * This module exists so the pipeline logic can be exercised under
 * node, where no DOM exists, against a fixed and well-formed corpus.
 * Handles: elements with quoted/unquoted attributes, void elements,
 * self-closing syntax, comments (emitted as nodes — the sanitizer
 * removes them), raw-text elements (script/style), and basic
 * character references.
 */

/** @typedef {import('../lib/sanitizer.js').Node} Node */

const VOID = new Set([
  'area', 'base', 'br', 'col', 'embed', 'hr', 'img', 'input', 'link',
  'meta', 'source', 'track', 'wbr',
]);
const RAWTEXT = new Set(['script', 'style']);

/** @param {string} s */
function decodeEntities(s) {
  return s.replace(/&(#x?[0-9A-Fa-f]+|[A-Za-z]+);/g, (m, body) => {
    if (body[0] === '#') {
      const code = body[1] === 'x' || body[1] === 'X'
        ? parseInt(body.slice(2), 16) : parseInt(body.slice(1), 10);
      return Number.isFinite(code) ? String.fromCodePoint(code) : m;
    }
    const named = { amp: '&', lt: '<', gt: '>', quot: '"', apos: "'", nbsp: '\u00a0' };
    return body in named ? named[/** @type {keyof typeof named} */ (body)] : m;
  });
}

/**
 * Parses an HTML fragment into the sanitizer tree model.
 * @param {string} html
 * @returns {Node[]}
 */
export function parseFragment(html) {
  let i = 0;
  /** @type {Node[]} */
  const roots = [];
  /** @type {import('../lib/sanitizer.js').ElementNode[]} */
  const stack = [];
  /** @param {Node} n */
  const append = (n) => (stack.length ? stack[stack.length - 1].kids : roots).push(n);

  while (i < html.length) {
    if (html[i] !== '<') {
      let j = html.indexOf('<', i);
      if (j < 0) j = html.length;
      append({ t: 'tx', text: decodeEntities(html.slice(i, j)) });
      i = j;
      continue;
    }
    if (html.startsWith('<!--', i)) {
      let j = html.indexOf('-->', i + 4);
      if (j < 0) j = html.length - 3;
      append({ t: 'comment', text: html.slice(i + 4, j) });
      i = j + 3;
      continue;
    }
    if (html[i + 1] === '/') {
      const j = html.indexOf('>', i);
      const tag = html.slice(i + 2, j < 0 ? html.length : j).trim().toLowerCase();
      for (let k = stack.length - 1; k >= 0; k--) {
        if (stack[k].tag === tag) { stack.length = k; break; }
      }
      i = (j < 0 ? html.length : j + 1);
      continue;
    }
    // start tag
    const m = /^<([A-Za-z][A-Za-z0-9-]*)/.exec(html.slice(i));
    if (!m) { append({ t: 'tx', text: '<' }); i++; continue; }
    const tag = m[1].toLowerCase();
    i += m[0].length;
    /** @type {Record<string, string>} */
    const attrs = {};
    let selfClosing = false;
    for (;;) {
      while (i < html.length && /\s/.test(html[i])) i++;
      if (html.startsWith('/>', i)) { selfClosing = true; i += 2; break; }
      if (html[i] === '>') { i++; break; }
      const am = /^([^\s=/>]+)/.exec(html.slice(i));
      if (!am) { i++; continue; }
      const name = am[1].toLowerCase();
      i += am[0].length;
      while (i < html.length && /\s/.test(html[i])) i++;
      let value = '';
      if (html[i] === '=') {
        i++;
        while (i < html.length && /\s/.test(html[i])) i++;
        const q = html[i];
        if (q === '"' || q === "'") {
          const j = html.indexOf(q, i + 1);
          value = html.slice(i + 1, j < 0 ? html.length : j);
          i = (j < 0 ? html.length : j + 1);
        } else {
          const vm = /^[^\s>]*/.exec(html.slice(i));
          value = vm ? vm[0] : '';
          i += value.length;
        }
      }
      attrs[name] = decodeEntities(value);
    }
    /** @type {import('../lib/sanitizer.js').ElementNode} */
    const el = { t: 'el', tag, attrs, kids: [] };
    append(el);
    if (VOID.has(tag) || selfClosing) continue;
    if (RAWTEXT.has(tag)) {
      const close = '</' + tag;
      let j = html.toLowerCase().indexOf(close, i);
      if (j < 0) j = html.length;
      if (j > i) el.kids.push({ t: 'tx', text: html.slice(i, j) });
      const gt = html.indexOf('>', j);
      i = gt < 0 ? html.length : gt + 1;
      continue;
    }
    stack.push(el);
  }
  return roots;
}
