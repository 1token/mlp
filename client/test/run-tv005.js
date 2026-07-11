// @ts-check
/**
 * test/run-tv005.js — the S4.8 gate: the JS sanitizer against the
 * TV-005 corpus under parsed-tree equality (D-94), plus the REQUIRED
 * idempotence fixpoint per case. Plain node, no dependencies:
 *
 *     node client/test/run-tv005.js
 *
 * Exits non-zero on any failure; CI runs this before the client is
 * allowed to grow (S4.8 gating order).
 */

import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { dirname, join } from 'node:path';
import {
  sanitizeTree, sanitizeChecked, serializeTree, treesEqual,
} from '../lib/sanitizer.js';
import { derivedText } from '../lib/derived-text.js';
import { parseFragment } from './mini-html5-parser.js';

const here = dirname(fileURLToPath(import.meta.url));
const vectorPath = join(here, '..', '..', 'conformance', 'vectors', 'mlp-tv-005.json');

/** @type {{ manifest: string[], foreign_urn_used: string,
 *   cases: { name: string, input: string, sanitized: string, notes: string }[] }} */
const vector = JSON.parse(readFileSync(vectorPath, 'utf8'));

let failures = 0;
/** @param {string} name @param {string} what @param {string} detail */
function fail(name, what, detail) {
  failures++;
  console.error(`FAIL ${name}: ${what}\n  ${detail}`);
}

for (const c of vector.cases) {
  const manifest = new Set(vector.manifest);
  const got = sanitizeTree(parseFragment(c.input), manifest);
  const want = parseFragment(c.sanitized);

  // The corpus promise: expected outputs are already fixpoints, so
  // parsing them yields the comparison tree directly (D-94 tree
  // equality, never byte equality).
  if (!treesEqual(got, want)) {
    fail(c.name, 'tree mismatch',
      `got:  ${serializeTree(got)}\n  want: ${serializeTree(want)}`);
    continue;
  }

  // §11.5 step 4: sanitize(sanitize(B)) equals sanitize(B) as trees.
  const again = sanitizeTree(parseFragment(serializeTree(got)), manifest);
  if (!treesEqual(got, again)) {
    fail(c.name, 'idempotence violated',
      `first:  ${serializeTree(got)}\n  second: ${serializeTree(again)}`);
    continue;
  }

  // The checked pipeline must agree and must not degrade the corpus.
  const checked = sanitizeChecked(parseFragment(c.input), vector.manifest, parseFragment, derivedText);
  if (checked.degraded || !treesEqual(checked.nodes, want)) {
    fail(c.name, 'sanitizeChecked disagrees', JSON.stringify(checked).slice(0, 200));
    continue;
  }
  console.log(`ok   ${c.name}`);
}

// The foreign URN really is outside the manifest — corpus sanity.
if (vector.manifest.includes(vector.foreign_urn_used)) {
  fail('corpus', 'foreign_urn_used unexpectedly in manifest', vector.foreign_urn_used);
}

// Cap degradation (§11.5 steps 5–6): a 20,000-node body degrades to
// its derived text, never to a rejection.
{
  const flood = '<p>x</p>'.repeat((16384 >> 1) + 64);
  const res = sanitizeChecked(parseFragment(flood), vector.manifest, parseFragment, derivedText);
  if (!res.degraded || !res.text.startsWith('x')) {
    fail('caps', 'node-count violation must degrade to derived text', JSON.stringify(res).slice(0, 120));
  } else {
    console.log('ok   caps_degrade_to_text');
  }
}

// §11.6 spot checks over a sanitized tree.
{
  const tree = sanitizeTree(parseFragment(
    '<h1>T</h1><ol start="3"><li>a</li><li>b</li></ol>' +
    '<p><a href="https://x.example/y">link</a></p>' +
    `<p><img src="${vector.manifest[0]}" alt="cat"></p>` +
    '<table><tr><td>1</td><td>2</td></tr></table>'), new Set(vector.manifest));
  const text = derivedText(tree);
  for (const needle of ['3. a', '4. b', 'link <https://x.example/y>', '[image: cat]', '1\t2']) {
    if (!text.includes(needle)) fail('derived_text', `missing ${JSON.stringify(needle)}`, text);
  }
  if (failures === 0) console.log('ok   derived_text_reference');
}

if (failures > 0) {
  console.error(`\nTV-005: ${failures} failure(s)`);
  process.exit(1);
}
console.log(`\nTV-005: all ${vector.cases.length} cases green (tree equality + idempotence)`);
