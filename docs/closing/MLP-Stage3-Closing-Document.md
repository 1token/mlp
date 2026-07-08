# Medialet Protocol (MLP) — Stage 3 Closing Document

| | |
|---|---|
| **Document** | Stage 3 Closing Document (client-design record) |
| **Status** | Stage 3 complete pending the D-181 freeze sign-off |
| **Produces** | Eleven design documents (S3.1–S3.11) + the MLP Client API draft-01 |
| **Date** | 2026-07-06 |
| **Editor** | Igor |
| **Predecessors** | Stage 1 Closing Document (D-01–D-42) · Stage 2 Closing Document (D-43–D-108) |

## 1. What Stage 3 produced

Twelve sessions turned the frozen MLP/0.1 specification into a complete
client design: the two-surface product frame (Inbox + Sender Studio),
every surface designed to interaction depth, a technology frame with no
build step anywhere, and the client↔home-server API companion that
becomes Stage 4's schema requirements. Per the D-38 duty, every feature
traces to the beachhead persona or a frozen decision; per the D-109
finding, **not one core-spec change was required** — the D-68/D-79/D-86
scoping held under twelve sessions of product pressure, with four small
additive gaps routed to the MEP queue rather than patched silently.

## 2. Decision register, D-109–D-180

**Pre-stage (protocol/client layering).** D-109 all proposed features
client/home-server layer. D-110 `in_reply_to` sufficient; `references`
pre-identified MEP candidate. D-111 labels local; traveling tags future
MEPs, advisory-only. D-112 two-surface frame + the session plan.

**Technology frame.** D-113 no build step anywhere; import maps;
`go:embed`; `tsc --noEmit` classified checker. D-114 EventTarget store,
unidirectional. D-115 light DOM default; shadow DOM only the Body
viewer. D-116 vendored-only libraries; BLAKE3-WASM first candidate.
D-117 JSDoc discipline; `types.js` mirrors wire names.

**S3.1 — Frame & IA.** D-118 one store two lenses; six surfaces +
global Compose. D-119 topics are bundled labels — one taxonomy. D-120
Done/flag triage; "pin" exclusively retention. D-121 bundles, chips,
sweep. D-122 Deliveries matrices; Extend-as-redispatch. D-123 the
tagged-author (Job tag) pattern.

**Day-40 correction (Igor).** D-124 recipient-side resolution
precedence. D-125 attach-from-library. D-126 fulfillment-window MEP —
the leading MEP-001 candidate.

**S3.2 — Inbox.** D-127 bundling algorithm; flag hoisting; deadline
bubbling. D-128 row anatomy; the chip state vocabulary. D-129 triage
gestures/keys; transactional sweep + undo. D-130 label system;
per-label notifications; Junk rescue wiring. D-131 the Expiring-offers
system bundle. D-132 unread semantics; zero state; rollups + SSE
registered.

**S3.3 — Composer.** D-133 two modes, subject-less replies. D-134
profile-native authoring; sanitize(output)==output asserted;
logo-as-Media dedup. D-135 hash-first pipeline; preview pairing;
dispatch gates on possession. D-136 cap meters; the curated-delivery
pattern; gallery-scale MEP-queued. D-137 resolve chips; guest fallback;
Bcc honesty; window surfacing. D-138 draft lifecycle; undo-send hold;
pre-flight.

**S3.4 — Receive & accept.** D-139 auto-grant decided: ≤4 MiB /
≤32 MiB budget / manifest-order; ephemeral GC-first class; junk gets
nothing. D-140 Body viewer + inline chips + Files panel + verification
banner. D-141 "Download from sender"; accept-time store selector.
D-142 Dismiss local vs terminal deny wrapped. D-143 tombstone causes;
resend as human reply. D-144 pin surfaces, explainer, one nudge.

**S3.5 — Status & tracking.** D-145 headline ladder; outbound-promises
meter. D-146 domain-grouped attribution (the D-70 truth rendered
truthfully). D-147 guest downloads shown, opens never — the reading-
privacy line held. D-148 delegation as neutral facts; per-delivery
budget control. D-149 the protocol-fact timeline. D-150 plain-text
resend convention (the §3.2.3 interaction caught); expiry alerts.

**S3.6 — Guest & claim.** D-151 one Body viewer, two hosts; no accept
economy for guests. D-152 PIN/expiry UX; two-channel nudge. D-153 the
anti-phishing, zero-tracking notification email. D-154 claim by
re-dispatch; instant-`have` delight; link survives claim. D-155 claim
anti-abuse; redirect residual documented.

**S3.7 — Media library.** D-156 object-level cards over reference-
level storage; label write-through. D-157 facets incl. Store meters and
the Saved slot. D-158 preview honesty; pairing heuristic; `preview_of`
MEP-queued; pin-the-original nudge. D-159 segmented meters; cleanup in
GC order. D-160 routing-rules editor; free moves; server thumbnails
registered.

**S3.8 — Identity, correspondents, junk.** D-161 passkey-first;
claim-seeded recovery email; no SMS. D-162 tiers legible with reasons;
allowlist/ask-first overrides. D-163 block = rejected:policy; the
quarantine-disclosure trade surfaced with operator option. D-164
display-safety everywhere; "verified" ≠ "safe". D-165 Junk cheapness
banner; derived-text-first rendering; rescue; 30-day purge.

**S3.9 — Architecture.** D-166 component inventory; own router; module
layout. D-167 the auto-escaping `html` helper as the security model;
keyed reconciler; sentinel paging. D-168 viewer pipeline; JS sanitizer
gated on TV-005 tree equality; render-time mapping declared
artifact-safe; origin-proxied BS. D-169 offline-tolerant posture. D-170
API conventions: one JSON dialect project-wide; undo tokens;
idempotency keys; SSE. D-171 the Client API companion draft-01 adopted.

**S3.10 — A11y, i18n, visual.** D-172 tokens.css; system fonts on
principle; chrome recedes, chips speak. D-173 redundant chip encoding;
the grayscale gate; reduced motion. D-174 keyboard completeness; focus
rules. D-175 composed names; throttled live regions; the semantic-
profile a11y dividend. D-176 runtime catalogs; tiny t(); registry-as-
error-keyspace.

**S3.11 — Triage.** D-177 the v1 line; minimal Save-to-Medialet
promoted (vetoable). D-178 post-v1 roster with recorded constraints.
D-179 MEP-gated roster; file MEP-001/002 at Stage 4 start; build to
the frozen spec. D-180 brand naming flagged; operator guide registered.

## 3. The catches ledger — design pressure as quality control

Stage 3 caught, corrected, or honestly surfaced eight items — the D-41
feedback loop arriving a stage early, now twice-proven:

1. **The day-40 walkthrough corrected by the editor** → recipient
   sovereignty (D-124/D-125) and the fulfillment-window gap (D-126) —
   a genuine spec gap found through a UX walkthrough.
2. **The pin terminology collision** (Inbox-by-Google "pin" vs D-21
   retention) → the flag ruling (D-120).
3. **Petra's own gallery breaks the frozen Manifest cap** → cap
   meters, the curated-delivery pattern, an MEP-queued analysis
   (D-136).
4. **Acceptance attribution is per-domain** (a D-70 consequence) →
   domain-grouped matrices instead of invented precision (D-146).
5. **The resend template nearly violated §3.2.3** → the plain-text
   convention (D-150).
6. **Preview pairing doesn't travel** → heuristic + marker + nudge +
   MEP-002 candidate (D-158).
7. **The quarantine verdict is classifier feedback** — an inherent
   frozen trade, surfaced and given operator latitude (D-163).
8. **The reading-privacy line tested where it's cheapest to break**
   (guest opens) — and held (D-147).

## 4. Artifact index

S3.1 Product Frame & IA · S3.2 Inbox · S3.3 Composer · S3.4 Receive &
Accept · S3.5 Status & Tracking · S3.6 Guest & Claim · S3.7 Media
Library · S3.8 Identity, Correspondents & Junk · S3.9 Architecture ·
S3.10 A11y, i18n & Visual · S3.11 Backlog Triage · **MLP Client API
draft-01** (informative companion, D-171).

**The MEP queue, final Stage 3 state**: MEP-001 fulfillment-window
override (D-126, file at Stage 4 start) · MEP-002 `preview_of` (D-158,
file at Stage 4 start) · candidates awaiting implementation evidence:
`references` (D-110), gallery-scale/cap analysis (D-136).

## 5. Stage 4 handoff

**Build order** (proposed session plan):

1. **S4.0** — repo scaffolding: layout (`spec/ server/ client/
   conformance/ docs/`), Go module, CI (tests + `tsc --noEmit` +
   conformance runner), all frozen artifacts committed; **file MEP-001
   and MEP-002** (D-179); commit the TV-002–004 generators (the
   Stage 2 debt, Closing Doc §5.2).
2. **S4.1** — the crypto/serialization core in Go (JCS, multiformats,
   kid, the three document signatures) — **green against TV-001
   before anything else exists**.
3. **S4.2** — SQLite schema v1 from the Client API draft §3 plus the
   federation-side tables (dispatch records, Delivery Records,
   references, reservations).
4. **S4.3–S4.6** — the federation server, vector by vector: Discovery
   + Domain Document (hardened fetch) → `/dispatch` + verdicts
   (TV-002) → tus transfer + verification (TV-003) → forwarding +
   delegation (TV-004).
5. **S4.7** — the Client API layer + SSE (draft-01 realized).
6. **S4.8–S4.11** — the client, foundation-first: store + tokens +
   shell/router → **Body viewer + the JS sanitizer gated on TV-005**
   → Inbox → composer → Deliveries → Media → identity/junk.
7. **S4.12** — guest pages + claim.
8. **S4.13** — two-domain deployment; **the minimum credible demo
   executed and recorded** (definition of done below).
9. **S4.14** — conformance hardening toward the every-MUST-a-test bar
   (D-104); operator guide (D-180); NLnet application finalized with
   D1–D5 now real (D-42).

**Conformance anchors**: TV-001–005 (Go core, negotiation, transfer,
delegation, and the JS sanitizer's tree-equality gate), the D-134
compose-time assert, the Client API draft, and the grayscale/a11y
release gates (D-173/D-175).

**Definition of done — the minimum credible demo** (D-41, extended by
Stage 3): two real domains; a delivery composed on one with job tag
and store routing; Tier-2 deferral with auto-granted previews; a large
object accepted, the transfer killed mid-flight, resumed to
completion with zero redundant bytes; `have` answering a resend; a
reply threading back into a topic bundle and swept to zero; and the
guest delivery → claim → instant-`have` funnel, end to end, on
camera.

---

*On D-181 confirmation, Stage 3 is closed and frozen. Three stages
down: 180 decisions, one frozen specification, five verified vectors,
twelve design artifacts. What remains is the stage the other three
were for.*
