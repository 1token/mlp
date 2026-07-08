# MLP Flagship Client Design — S3.10: Accessibility, i18n & Visual Language

> **Status.** Stage 3, session 10. Judgment calls D-172–D-176 pending
> editor confirmation. §7 traces per D-38.

---

## 1. Design tokens and the calm aesthetic (D-172)

`styles/tokens.css` is the single visual source of truth — CSS custom
properties, no preprocessor (D-113): semantic color tokens
(`--surface`, `--ink`, `--ink-muted`, `--accent`, per-state chip
tokens), a spacing scale, a type scale, radii, elevation, and motion
durations/easings. **Dark mode** is a second token block behind
`[data-theme]`, defaulting from `prefers-color-scheme` with a manual
override. Densities: comfortable default, compact option (Petra with
forty deliveries on screen).

**The system font stack, on principle**: the protocol forbids content
from loading external resources (D-31); the app honors the same ethos
about itself — no webfonts, no third-party assets, platform typography
with its free script coverage. The aesthetic rule everything serves:
**the chrome recedes; the chips speak.** Lifecycle information
(states, deadlines) is the product's voice (D-121/D-128); everything
else is quiet enough to let it carry.

## 2. The chip vocabulary, formalized (D-173)

Color never carries meaning alone. Each §10.3 state has a shape or
glyph; color reinforces; text appears wherever space allows:

| State | Shape/glyph | Color role | Reduced motion |
|---|---|---|---|
| `offered` | dashed outline + ↓ | informative blue | — |
| `expected` | the progress ring *is* the shape | accent | stepped percent text |
| `available` | plain solid (absence of badge = normal) | — | — |
| `pinned` | pin glyph, filled | — | — |
| tombstone | greyed + slash | desaturated | — |
| deadline | clock glyph + always-present text ("5 d") | neutral ≥7 d / amber / red (D-128) | — |
| preview | corner text label "PREVIEW" (D-158) | — | — |

**The grayscale gate**: every screen must remain fully legible
rendered in grayscale — a mechanical design test run on each surface
before it ships. Contrast floor: WCAG 2.2 AA (4.5:1 text, 3:1 UI
components), verified in both themes. `prefers-reduced-motion`
replaces all animated state (rings, sweeps, snackbar slides) with
discrete updates.

## 3. Keyboard completeness (D-174)

Beyond the triage map (D-129), the whole app is operable without a
pointer:

- **Global**: `?` shortcut overlay · `g` then `i/d/m/c/s` surface
  navigation · `/` search · `c` compose · `Esc` dismiss/close.
- **Composite lists** (inbox, library grid, matrices): the WAI-ARIA
  APG roving-tabindex pattern — arrows move within a widget, `Tab`
  moves between widgets; rows are single stops (§4).
- **Bundles**: `Enter`/`Space` toggle disclosure; `e` sweeps.
- **Thread**: `n`/`p` message navigation; object chips focusable,
  `Enter` opens the action menu.
- **Composer**: `Ctrl/Cmd+Enter` send (into the D-138 undo hold).
- **Lightbox**: arrows navigate, `p` pin, `l` label, `i` info, `Esc`
  closes.
- **Focus rules**: route changes move focus to the surface heading;
  dialogs trap and restore; a skip-link precedes the shell; the focus
  ring is a token and is never removed.

## 4. Screen-reader semantics (D-175)

- **Landmarks**: banner, navigation, main, search.
- **Bundles** are disclosures: a button with `aria-expanded` and a
  composed accessible name — "Novak Wedding, three threads, two
  unread, most urgent deadline five days" — controlling a region.
- **Rows** are single focus stops with composed names (sender,
  subject, snippet, media summary, deadline, unread state); actions
  live in a per-row menu, not as a chip-by-chip tab gauntlet.
- **Chips** verbalize fully: "final-master.mp4, 2.1 gigabytes,
  offered, expires in five days."
- **Live announcements are throttled**: transfer progress announces on
  state changes and ~10 % steps via `aria-live="polite"` — never the
  raw SSE firehose (D-132); undo snackbars are `role="status"`.
- **The profile's a11y dividend, noted**: `mlp-html/1` is
  semantic-only by law (§11/D-91) — received content *cannot* be
  div-soup; the Body viewer inherits well-formed headings, lists, and
  tables from every conformant sender. Shadow DOM is AT-transparent;
  in-shadow chips carry the same naming rules.
- **Release gates**: axe-core runs on a dev page (a checker, not a
  build step — the D-113 classification); manual NVDA and VoiceOver
  passes are on the release checklist.

## 5. i18n without a build step (D-176)

- **Runtime JSON catalogs** (`/i18n/en.json`, `/i18n/sk.json` first),
  fetched on boot and on language switch; a ~60-line `t()` module:
  key lookup, placeholder interpolation, plurals via
  `Intl.PluralRules`. No MessageFormat library — the vendored-lib
  policy (D-116) satisfied by not needing one.
- **Formatting** through platform `Intl`: dates and the time-bundle
  labels (`Intl.RelativeTimeFormat` where it fits), numbers
  localized. **Sizes**: user-facing decimal units ("2.1 GB") with
  exact byte counts in info panels; spec-facing meters keep binary
  units where the number *is* the spec's ("Envelope 118 KB /
  256 KiB", D-136).
- **RTL-ready structurally**: logical CSS properties
  (`margin-inline-start`…) from day one; `dir="auto"` on
  user-authored strings; RTL catalogs can land without layout work.
- **Pseudo-localization** built into `t()` (accented expansion in dev
  mode) — truncation bugs caught with zero tooling.
- **Errors**: the `urn:mlp:err` registry (§14) doubles as the error
  catalog keyspace — every protocol code maps to a human sentence,
  and a code without a catalog entry is a visible gap rather than a
  silent generic failure. Language negotiation: `Accept-Language`
  default, user setting override; guest-page and notification-email
  language remain sender-selected (D-153).

## 6. Voice and microcopy

The established wordings are the voice: protocol facts, named
consequences, no blame, no alarm theater — "Download from sender"
(D-141), "signature verified" never "safe" (D-164), "junk holds no
files" (D-165), tombstones that say what happened (D-143). Microcopy
rules: state the fact, name the consequence, offer the action. The
zero state stays calm (D-132) in every language.

## 7. Traceability (D-38)

| Element | Traces to |
|---|---|
| tokens.css single source, no preprocessor | D-113; D-172 |
| System fonts on principle | D-31 ethos; i18n coverage; D-172 |
| Chips as the voice, chrome recedes | D-121/D-128; D-172 |
| Redundant encoding, grayscale gate | D-128 vocabulary; D-173 |
| Reduced-motion variants | D-173 |
| Roving tabindex, focus rules | D-129 base map; D-174 |
| Composed names, throttled live regions | D-132 SSE; D-175 |
| Semantic-profile a11y dividend | §11/D-91; D-175 |
| Runtime catalogs, tiny t(), Intl | D-113/D-116; D-176 |
| Registry-as-keyspace errors | §14/D-73; D-176 |
| Voice = established wordings | D-37/D-98/D-141/D-164/D-165 |

---

*Next: S3.11 — backlog triage: everything parked across S3.1–S3.10
(Save to Medialet, snooze, download scheduler, bundling-on-the-fly,
template variables, offline media, analytics, import/export, content
search) sorted into v1 / post-v1 / MEP-gated with rationale — the
session that draws the line Stage 4 will build to. Then S3.12 closes
the stage.*
