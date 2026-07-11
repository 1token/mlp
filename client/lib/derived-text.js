// @ts-check
/**
 * lib/derived-text.js — the §11.6 deterministic reference algorithm
 * (D-95): the cap-violation fallback, the accessibility surface, the
 * preview material, and the quarantine classifier's input (D-21).
 * Operates on the sanitizer's tree model; safe on any tree — it is a
 * pure text projection.
 */

/** @typedef {import('./sanitizer.js').Node} Node */

const BLOCKS = new Set([
  'p', 'div', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'blockquote', 'pre',
  'ul', 'ol', 'dl', 'dt', 'dd', 'table', 'caption', 'thead', 'tbody',
  'tfoot', 'figure', 'figcaption', 'hr',
]);

/**
 * @param {Node[]} nodes
 * @returns {string}
 */
export function derivedText(nodes) {
  const out = render(nodes, { ol: null }).replace(/\n{3,}/g, '\n\n');
  return out.replace(/^\n+|\n+$/g, '');
}

/**
 * @param {Node[]} nodes
 * @param {{ ol: number | null }} ctx list numbering context
 * @returns {string}
 */
function render(nodes, ctx) {
  let out = '';
  for (const n of nodes) {
    if (n.t === 'tx') { out += n.text; continue; }
    if (n.t !== 'el') continue;
    switch (n.tag) {
      case 'br': out += '\n'; break;
      case 'img': out += `[image: ${n.attrs.alt ?? ''}]`; break;
      case 'a': {
        const text = render(n.kids, ctx);
        const href = n.attrs.href ?? '';
        // External destinations disclosed; URN and fragment links are
        // text alone (§11.6).
        out += href && !href.startsWith('urn:mlet:') && !href.startsWith('#')
          ? `${text} <${href}>` : text;
        break;
      }
      case 'ul': out += '\n' + render(n.kids, { ol: null }) + '\n'; break;
      case 'ol': {
        const start = /^[0-9]{1,6}$/.test(n.attrs.start ?? '') ? parseInt(n.attrs.start, 10) : 1;
        const sub = { ol: start };
        out += '\n' + render(n.kids, sub) + '\n';
        break;
      }
      case 'li': {
        const prefix = ctx.ol === null ? '- ' : `${ctx.ol}. `;
        if (ctx.ol !== null) ctx.ol++;
        out += prefix + render(n.kids, { ol: null }).trim() + '\n';
        break;
      }
      case 'tr': {
        const cells = n.kids
          .filter((k) => k.t === 'el' && (k.tag === 'td' || k.tag === 'th'))
          .map((k) => render(/** @type {import('./sanitizer.js').ElementNode} */(k).kids, { ol: null }).trim());
        out += cells.join('\t') + '\n';
        break;
      }
      default: {
        const inner = render(n.kids, ctx);
        out += BLOCKS.has(n.tag) ? '\n' + inner + '\n' : inner;
      }
    }
  }
  return out;
}
