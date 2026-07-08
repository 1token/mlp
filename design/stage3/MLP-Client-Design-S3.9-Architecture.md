# MLP Flagship Client Design — S3.9: Client Architecture

> **Status.** Stage 3, session 9. Judgment calls D-166–D-171 pending
> editor confirmation. Companion deliverable: the MLP Client API draft
> (separate document). §7 traces per D-38.

---

## 1. Component architecture (D-166)

Custom elements, `mlp-` prefix, light DOM by default (D-115). The
inventory:

- **Shell**: `<mlp-app>` (nav, surface host), a History-API **router of
  our own** (~50 lines: route table, params, guards) — the Go binary
  serves `index.html` for app routes, keeping deep links working with
  zero infrastructure. Routes: `/inbox`, `/deliveries`, `/media`,
  `/correspondents`, `/settings`, `/search`, `/t/{thread}`,
  `/d/{delivery}`, `/compose`.
- **Surfaces**: `<mlp-inbox>`, `<mlp-deliveries>`, `<mlp-media>`,
  `<mlp-correspondents>`, `<mlp-settings>`, `<mlp-search>`.
- **Shared**: `<mlp-bundle>`, `<mlp-item-row>`, `<mlp-thread>`,
  `<mlp-composer>`, `<mlp-files-panel>`, `<mlp-lightbox>`,
  `<mlp-chip>` (the D-128 media/deadline chips),
  `<mlp-address-chip>` (D-164 display-safety encapsulated in one
  place), `<mlp-quota-meter>` (D-159 segments),
  `<mlp-store-selector>` (D-105), and **`<mlp-body-viewer>`** — the
  one shadow-DOM family (§3).

Module layout (Stage 4's directory truth):

```
client/
  index.html          # import map, CSP meta, shell mount
  sw.js               # hand-written service worker (§4)
  app/                # one file per element
  store/              # store.js · types.js · api.js · live.js · slices/
  lib/                # vendored: blake3-wasm/ · sanitizer.js (ours)
  styles/             # tokens.css · app.css
```

## 2. Rendering discipline (D-167)

State flows one way (D-114): components render from the store and
mutate only through store methods. Rendering without a framework:

- **The `html` tagged template with mandatory auto-escaping.** Every
  interpolation is escaped; raw insertion exists only as an explicit
  `unsafe(...)` wrapper — greppable, reviewable, rare. This single
  convention is the app-chrome XSS defense; it is not optional style,
  it is the security model of frameworkless rendering.
- **Leaf components re-render wholesale** from state (small DOM,
  simplicity wins). **Container components reconcile children by
  key** via our own ~40-line utility (match by id, move/insert/remove
  — no virtual DOM, just list stitching).
- **Long lists paginate, never virtualize**: cursor pagination
  (D-132) plus an `IntersectionObserver` sentinel keeps the DOM
  bounded with zero libraries.
- Store subscription binds to lifecycle (`connectedCallback`
  subscribes, `disconnectedCallback` unsubscribes — D-114), with
  per-slice events (`inbox-changed`, `thread-changed:{id}`) so
  components hear only what they render.

## 3. The Body-viewer pipeline (D-168)

`<mlp-body-viewer>` — the §11.7 isolation boundary, shared by app and
guest page (D-151):

1. Receives the **server-derived render form** (§11.5, D-94).
2. **Re-sanitizes client-side** — the dual duty (D-31) — through
   `lib/sanitizer.js`, our JavaScript implementation of §11 whose
   correctness anchor is **passing the TV-005 corpus under tree
   equality**: the corpus is the cross-language conformance bridge
   between the Python prototype and the shipping JS.
3. Builds the shadow root, **mapping `urn:mlet:` sources to home-BS
   resolution URLs** (`/bs/o/{urn}`, `/bs/o/{urn}/thumb?w=…`) at
   render time. Stated plainly: this is DOM-ephemeral presentation,
   not artifact mutation — the stored Signed Medialet is untouched
   and forwards verbatim; D-28 stands. (It resembles the retired
   rewrite mechanism and is nothing of the kind.)
4. Upgrades Manifest-URN anchors into `<mlp-object-chip>` elements
   inside the shadow root (the D-140 inline chips), state-fed from
   the store.

**CSP posture**: the app ships the §11.7 policy; BS resolution is
**proxied through the app origin** (`/bs/…`) so `img-src 'self'`
suffices, cookies stay first-party, and the policy stays one line of
truth.

## 4. Offline posture (D-169)

**Offline-tolerant, not offline-first** — stated in-product, not
implied. Concretely:

- A **hand-written, versioned service worker** caches the app shell
  (instant load, offline shell); cache-name version bumps are a
  manual constant — the no-build discipline applied to sw lifecycle.
- **Drafts** compose offline into IndexedDB and sync (D-138).
- **Triage mutations queue** offline (done/flag/label/sweep) with
  client-generated idempotency keys, replaying on reconnect;
  conflicts resolve last-write-wins on server timestamps.
- **Reading**: recently loaded threads and render forms cache (LRU in
  IndexedDB); recently viewed thumbnails likewise.
- Full offline media ("keep offline" pins) — backlogged (S3.11).

## 5. Live updates

`store/live.js` subscribes to the SSE feed (D-132): typed events
(`inbox`, `thread:{id}`, `delivery:{id}`, `transfer:{urn}`, `quota`),
`Last-Event-ID` resume, heartbeat comments, exponential-backoff
reconnect. Transfer events drive the D-140 progress rings without
polling.

## 6. The companion draft

The **MLP Client API draft-01** ships as a separate document
(informative companion per D-68/D-86): the reference implementation's
API, the interop suggestion for independent clients, and Stage 4's
schema requirements list. Conventions and endpoint inventory there;
adopted by D-170/D-171.

## 7. Traceability (D-38)

| Element | Traces to |
|---|---|
| Light DOM + one shadow boundary | D-115; §11.7 |
| Own router, index fallback | D-113; D-166 |
| `html` escaping discipline | frameworkless XSS defense; D-167 |
| Keyed reconciler, sentinel paging | D-113/D-114; D-132; D-167 |
| JS sanitizer vs TV-005 | D-31/D-94; D-96; D-168 |
| Render-time mapping ≠ rewrite | D-28; §11.5; D-168 |
| Origin-proxied BS, one CSP | §11.7; D-168 |
| Offline-tolerant scope | D-138; D-169 |
| SSE wiring | D-132; live progress D-140 |
| Companion API draft | D-68/D-86; D-170/D-171 |

---

*Next: S3.10 — accessibility, internationalization, and the visual
language: the state-chip system as a designed vocabulary (color,
shape, motion), keyboard completeness beyond triage, screen-reader
semantics for bundles and chips, i18n architecture without a build
step, and the calm aesthetic the zero state promised.*
