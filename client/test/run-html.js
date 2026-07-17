// @ts-check
/**
 * test/run-html.js — the html`` escaping discipline (D-167): every
 * interpolation escapes; unsafe() is the only raw path; arrays join
 * with per-item escaping. Pure node, no DOM (the reconciler's gate
 * is the browser).
 */
import { html, unsafe, escapeHtml } from '../lib/html.js';

let failures = 0;
/** @param {string} name @param {string} got @param {string} want */
function check(name, got, want) {
  if (got !== want) { failures++; console.error(`FAIL ${name}\n  got:  ${got}\n  want: ${want}`); }
  else console.log(`ok   ${name}`);
}

check('escapes_interpolation',
  html`<p>${'<img src=x onerror=alert(1)>'}</p>`,
  '<p>&lt;img src=x onerror=alert(1)&gt;</p>');
check('escapes_quotes_in_attrs',
  html`<div title="${'a" onmouseover="x'}"></div>`,
  '<div title="a&quot; onmouseover=&quot;x"></div>');
check('unsafe_passthrough',
  html`<div>${unsafe('<b>ok</b>')}</div>`,
  '<div><b>ok</b></div>');
check('arrays_escape_per_item',
  html`<ul>${['<li>', unsafe('<li>x</li>')]}</ul>`,
  '<ul>&lt;li&gt;<li>x</li></ul>');
check('null_undefined_empty',
  html`<p>${null}${undefined}</p>`, '<p></p>');
check('escapeHtml_covers_five',
  escapeHtml(`&<>"'`), '&amp;&lt;&gt;&quot;&#39;');


// S4.21: snippetHtml — the D-267 bracket marks, safely.
import { snippetHtml } from '../lib/html.js';
check('snippet_marks',
  snippetHtml('fotky zo [svadby] v meste'),
  'fotky zo <mark>svadby</mark> v meste');
check('snippet_escapes_inside_and_outside_marks',
  snippetHtml('<b>x</b> [<i>y</i>] z'),
  '&lt;b&gt;x&lt;/b&gt; <mark>&lt;i&gt;y&lt;/i&gt;</mark> z');
check('snippet_stray_brackets_stay_literal',
  snippetHtml('a ] b [ c'),
  'a ] b [ c');
check('snippet_empty_mark',
  snippetHtml('x [] y'),
  'x <mark></mark> y');

if (failures) { console.error(`\nhtml.js: ${failures} failure(s)`); process.exit(1); }
console.log('\nhtml.js: escaping discipline green');
