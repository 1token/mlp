# MLP — Stage 4 Continuity Brief

> Purpose: resume implementation in a fresh working session with zero
> context loss. Read this first; everything else is referenced from it.
> Updated at the S4.11 → S4.12 boundary (2026-07-11).

## 1. Project in one paragraph

Medialet (MLP): an open federated protocol for asynchronous
point-to-point delivery of heavy media — "email for heavy media."
Signaling cheap and optimistic; storage expensive and pessimistic
("junk weighs kilobytes"). Apache-2.0 code / CC-BY-4.0 spec;
medialet.org; NLnet NGI Zero Commons Fund is the funding target.
Beachhead persona: Petra, independent photographer. Sole editor: Igor;
every judgment call is confirmed explicitly and logged with a
sequential D-number; changes to frozen artifacts travel only as MEPs.

## 2. Where everything lives (this repository)

- `spec/MLP-Core-Specification-0.1-draft-01.md` — **frozen** (D-108).
- `spec/meps/` — MEP-001 (fulfillment-window) and MEP-002
  (`preview_of`), both **Draft — awaiting Igor's editor decision**
  (open action). Build to the frozen spec until accepted (D-179).
- `conformance/` — TV-001–005 + generators; **byte-reproducible,
  CI-enforced** (D-186).
- `design/stage3/` — the frozen client design (D-181) + Client API
  draft-01 (Stage 4's requirements list).
- `docs/closing/` — the full decision registers: Stage 1 (D-01–42),
  Stage 2 (D-43–108), Stage 3 (D-109–181).
- `server/` — Go module `medialet.org/mlp`; built so far: `core/`
  (S4.1), `store/` (S4.2), `discovery/` (S4.3), `sn/` (S4.4 + S4.6
  forwarding/delegation), `bs/` (S4.5), `clientapi/` (S4.7).
  `client/` — S4.8: `lib/sanitizer.js` + `lib/derived-text.js`
  (TV-005-gated §11 pipeline), `app/mlp-body-viewer.js`, `test/`.
  S4.9: `index.html` (CSP shell), `lib/html.js` (escaping tag +
  keyed reconciler), `store/` (store·api·live), `app/mlp-app.js`,
  `app/mlp-inbox.js`, `app/mlp-thread.js`, `styles/`. S4.10:
  `app/mlp-composer.js`; server `sn/compose.go`,
  `clientapi/drafts.go`, `bs` LocalPatch/LocalHead.

## 3. Stage 4 state

| Session | Delivered | Proof |
|---|---|---|
| S4.0 | Repo scaffold, MEP-001/002 filed, TV-002–004 generators reconstructed | all five vectors regenerate **byte-identically**; CI gate |
| S4.1 | `core/`: JCS (D-43 dialect, own RFC 8785 writers), multiformats, kid self-verify, SignDoc/VerifyDoc with label context match | `go test` recomputes **every** TV-001 value incl. deterministic sigs and UUIDv7s |
| S4.2 | `store/`: 0001 migration (~30 tables), runner (`user_version`), D-87 state machine **enforced by trigger** | legal walk green; 6 forbidden transitions abort; replay-unique; reservation terminal |
| S4.3 | Generator-debt repair (D-197); `discovery/`: Domain Document parsing (§5.2/§6.1–6.3), hardened fetch (§5.4), Resolver with 24 h ceiling + unknown-kid re-fetch + negative cache (§5.5) | TV-001 `domain_document` fixture is the parsing anchor; 21 tests green incl. dial-time SSRF wiring proof; all five vectors regenerate byte-identically **for real** now |
| S4.4 | `sn/`: §3.4.4 validation sequence, §7.3 `/dispatch` with D-74 retry idempotency, §7.4 verdict generation + verification, §7.5 reservations, §7.6 `/verdict` updates with the transition table, §7.7 default tiers, RFC 9457 problems | dispatching the TV-001 envelope reproduces TV-002 **verdict 1 byte-identically** (708 B, exact sig); recipient-accept reproduces **verdict 2** (923 B) and mints the reservation; failure matrix maps every §3.4.4 item to its §7.8 code; deny→grant refused as `invalid-transition` |
| S4.6 | Migration 0002 (D-209 Delivery-Record repair); `sn/forward.go` + `sn/delegation.go`: §3.4.2 chain append with §9.2 duties + D-51 loop prevention, §9.4 `delegation/1` requests + `/fulfill`, §9.5 source-side validation with the D-83 budget and dedup, §9.3 requester loop | the TV-004 **forwarded Envelope reproduces byte-identically** (1,669 B; the appended attestation IS TV-001's hop sig verbatim); the **delegation request reproduces** (1,095 B) minting the reservation on the requester's BS (D-82); the §9.5 walk answers the exact unsigned response, dedups replays without budget, alarms `medialet-mismatch` on splice; 9-case failure suite |
| S4.7 | `clientapi/`: the D-170 conventions (dialect JSON, problem+json with client codes, HttpOnly session + X-MLP-Client CSRF, Idempotency-Key journal), password fallback (PBKDF2, RFC 7914 vectors), sessions CRUD, the D-132 SSE feed with Last-Event-ID resume, and the machinery-backed endpoints: `/o/{urn}/accept` (direct upgrade + §9.3 delegation), `/deliveries` + D-149 `/timeline`, `/objects/have`, `/quota`, `/settings` | one API call to accept a direct TV-001 delivery emits **TV-002 verdict 2 byte-identically** toward the origin; a forwarded TV-004 delivery triggers the delegation flow; SSE resumes exactly from the journal; idempotent replay re-executes nothing |
| S4.8 | `client/lib/sanitizer.js` (§11 pipeline: two-tier removal, attribute/style/URL filtering, caps, REQUIRED idempotence fixpoint, degradation to derived text), `lib/derived-text.js` (§11.6 reference), `app/mlp-body-viewer.js` (the §11.7 shadow boundary with render-time urn→BS mapping), node harness, CI client-checks job activated | **all 14 TV-005 cases green under tree equality + idempotence on the first run**; caps degrade to text; tsc --noEmit clean over the shipping client code (D-117) |
| S4.9 | Ingest materialization (`sn/materialize.go`: messages/threads per D-110, offered refs per §10.3, rollups); `clientapi/threads.go` (inbox+junk views, full thread, triage trio with D-129 undo, `/undo`); accept authorization closed + refs offered→expected; client shell/store/inbox/thread over the S4.7 API and SSE | TV-001 ingest materializes thread+message+refs; replies join parent threads, orphans root their own, re-deliveries dedup; quarantine lands in junk; undo restores exactly and expires at 30 s; non-recipient accept 404s; html`` escaping suite + TV-005 gate + tsc all green |
| S4.10 | `sn/compose.go` (D-138 pre-flight, author/1 via `keyWithRole`, per-domain fan-out, dispatch + synchronous verdict recording, deliveries/refs-promised/sender-copy/timeline materialization); `clientapi/drafts.go` (drafts CRUD, hash-first `/uploads` declare + intra-domain PATCH over the shared `bs` core, `/drafts/{id}/send`); `app/mlp-composer.js` (autosave, attach-by-reference, the 10 s undo hold) | **composing Petra's draft reproduces the TV-001 Signed Medialet AND Signed Envelope byte-identically**, dispatches to a live target over HTTP, and records **TV-002 verdict 1 byte-identically** on return; the upload door resumes at the checkpoint and refuses corrupt digests; send gates on possession (409); two-domain fan-out = one delivery, two envelopes, both recipients materialized |
| S4.11 | `render/`: the Go §11 pipeline over x/net/html (spec-compliant HTML5 parsing) — sanitizer, §11.6 derived text, D-132 snippet; migration 0003 (`render_degraded`); derivation at ingest + send, rollup snippet, render-form thread payloads, the D-21 classifier hook; media library (cards, pin/unpin, owner delete, hardened object serving, raw-medialet endpoint), the `OnVerified` seam flipping expected→available; junk release/block with the correspondents ledger; client deliveries lens, media cards, nav tabs, junk actions | **the Go sanitizer passes all 14 TV-005 cases first run** — the third implementation through the corpus; the classifier demotes on derived text and a released sender outranks it; the media lifecycle walks §10.3 end-to-end offered→…→unavailable(deleted) |
| S4.5 | `bs/`: §6.6 RFC 9421 profile, the §8.2–8.4 upload resource with the D-77 transactional pipeline, D-27 BLAKE3 checkpoints, §8.5 failure taxonomy, §8.7 pusher loop under D-72 | all three TV-003 signature bases reproduce **byte-identically** and vector signatures verify; the transcript replays header-for-header (204/20, HEAD 200 with the exact `Upload-Expires`, 204/36 `verified`); digest-mismatch rolls back, hash-mismatch resets to 0 and recovers, restart re-derives the checkpoint; the pusher survives a lost 204 with PATCH offsets exactly [0, 20] |

Register tail since the Stage 3 closing doc: **D-182–D-204** —
D-182 repo/CI · D-183 MEP template · D-184/185 MEP-001/002 filed ·
D-186 generator debt paid · D-187 module + zeebo/blake3 (mandated by
§6.4) · D-188 JCS approach (dialect violations are errors) · D-189
core API surface · D-190 TV-001 green acceptance · D-191 driver
posture (ship modernc.org/sqlite; validate with mattn in sandbox;
code driver-agnostic) · D-192 schema conventions (RFC3339 TEXT;
JSON-as-TEXT; minted secrets stored as `*_hash`, presented tokens
plaintext) · D-193 refs trigger = D-87 verbatim · D-194 federation
records (dispatches = §9.5 credential store; full verdict history for
the D-149 timeline; `hasher_state` per D-27) · D-195 schema accepted ·
D-196 continuity working practice (public repo as carrier; clone,
read this brief, **verify inherited state before building**) ·
D-197 generator-debt repair: tv-001.py was still the pre-S2.4
provisional generator (stdout-only, raw-key kid) so the CI
reproducibility gate passed vacuously for TV-001, and tv-005.py wrote
to a sandbox path absent in CI; both fixed, all five vectors verified
byte-identical (vector *content* was never wrong — core go tests
independently recompute TV-001) · D-198 Domain Document parsing
semantics: D-43 dialect parser; document-level hard failures (missing
required member, domain-binding mismatch, empty version intersection,
non-https `sn`, >64 entries, >65,536 bytes) vs §6.2 entry-level
ignores (kid self-verify failure, alg/multicodec mismatch, malformed
entry or window, duplicate kid) with a `Rejected` count; empty key
set allowed (spec-literal); `VerificationKey(kid, role, at)` bundles
the §6.3 role + window + decode checks for S4.4 · D-199 hardened
fetch posture: address filter runs in the dialer `Control` hook on
the literal address at connect(2) time — check and use coincide, the
§5.4 pinning requirement; forbidden set = spec list + IANA extras
(192.0.0.0/24, TEST-NETs, 198.18/15, 240/4 incl. broadcast,
2001:db8::/32, NAT64 64:ff9b::/96, v4-mapped unmapped first);
https+443 enforced on the initial URL and every redirect hop; cap
applies to decoded body bytes with abort-on-overrun; connect 5 s /
total 10 s; no proxy, no cookies, no bodies · D-200 caching
semantics: TTL = min(Cache-Control max-age, 24 h), absent freshness
info defaults to the ceiling itself, no-store/no-cache/max-age=0
stores the row already-stale (document used once, never reused);
unknown kid forces exactly one re-fetch and only when the miss came
from cache (a fresh document is already authoritative); negative
cache in-memory, 5 min; cached documents re-validated on every load
(cache is data, not authority) · D-201 S4.4 scope & surface: `sn/`
implements §7 (`/dispatch`, verdicts, `/verdict` updates, RFC 9457
problems); §3.4.4 items 1–5 in `ParseEnvelope`, 6–7 in
`ProcessDispatch`; locality and structural-cap violations map to
`malformed` (no closer §7.8 code exists); `discovery` exports
`ErrUnknownKID` so consumers map unknown-kid → `signature-invalid`
vs resolution failure → `discovery-failed` · D-202 policy posture:
tier matching compares mailbox keys against the *author* identity
(the value the Medialet attests); `tier_override='block'` →
quarantined with reason `policy`; same-domain author → Tier 1; quota
headroom stubbed permissive until S4.5 object accounting; Tier-2
possession answered as plain `defer` (no disclosure — the D-29
masked-grant machinery arrives with the BS in S4.5); quarantined-only
Envelopes get `defer`/`policy` · D-203 persistence: `medialets.raw`
stores the JCS canonical Signed Medialet (identity-preserving; equals
received bytes for JCS emitters per §2.4 SHOULD); an (author, id)
pair bound to *different* content is refused as `replay` (D-46);
reservations minted hash-only (D-192); full verdict history with
per-URN rows (D-71/D-149) · D-204 §7.6 update semantics:
unchanged-state entries in snapshots are no-ops (terminal have/deny
are not "altered" by restatement; grant→grant with a Reservation is
the explicit refresh); state *changes* are confined to the table,
any violation discards the whole update as `invalid-transition`;
supersession by (`created`, `verdict_id`) — stale snapshots are
stored for the D-149 history but never applied; duplicate
`verdict_id` re-POSTs are idempotent 204s; the issuer must equal the
dispatch's `target_domain` else `unknown-envelope`; an update
arriving before the recorded synchronous response establishes the
baseline · D-205 checkpoint persistence strategy: zeebo/blake3 has no
hasher-state serialization, so D-27 checkpoints are transactional
in-memory `Clone()`s, and restart durability comes from
**re-derivation** — the quarantined partial, truncated exactly at the
durable offset, fully determines the hasher state; `hasher_state`
stays NULL, reserved for a future serializable hasher (the D-27
invariant holds: HEAD never lies, restarts recover, cost is one
O(offset) re-hash) · D-206 RFC 9421 implementation posture: strict
profile-only parsing (label `mlp`, covered components equal to the
D-66 sets *as ordered*, created+keyid required, alg only ed25519);
the base's `@signature-params` line reuses the verbatim
Signature-Input serialization for byte fidelity; `@target-uri`
reconstructed as configured `PublicBase` + request URI · D-207
transfer failure-code judgments: a body exceeding the exact declared
size aborts as **`hash-mismatch`** with reset-to-zero (the bytes
cannot match the URN — object-level wrongness, not transport);
consumed tokens answer 410 `reservation-invalid`, unknown tokens 401;
Tus-Resumable mismatch → 412; segment verification (D-78 SHOULD) and
the slow-loris throughput floor (§8.6 MAY) deferred as
quality-of-implementation — the completion check is the conformance
floor · D-208 pusher posture: `discovery.ForbiddenAddr` exported for
D-72 reuse; the hardened default client refuses http, all redirects,
and forbidden addresses at dial time; no overall timeout (PATCH
bodies are legitimately long-lived — cancellation is the context's);
digest/offset mismatches realign via HEAD bounded by the attempt
budget; `reservation-expired`/`-invalid` surface as a typed
renegotiation error for the §7.6 grant→grant path; a lost *final* 204
resolves at the negotiation layer (re-HEAD meets the consumed token,
renegotiation answers `have`) · D-209 Delivery-Record repair
(migration 0002): §9.3 step 2 / §3.4.2 need the received Envelope's
`created` and Hop Signature *value* to construct its attestation —
D-53 promised "everything needed" but the 0001 schema kept only the
kid; `envelopes_in` gains `envelope_created` and `hop_sig_value`,
populated at ingest; pre-0002 rows backfill NULL and refuse to seed
attestations with an explicit error · D-210 forwarding posture:
`Forward(origin, envelope_id, to, forwarded_by, mode, automatic)` —
Delegated carries received sources through with the root origin
guaranteed present (the §9.2 minimum), Custody lists self first with
received sources as fallback; D-51 loop prevention keys on the
`automatic` flag (deliberate user forwards proceed); the forward is
recorded in `dispatches`, making this domain a §9.5-capable source ·
D-211 requester posture: reservations minted at request build
(refused entries leave pending rows that expire into quarantine GC);
credential selection per §9.3 step 2 incl. the own-origin
construction; candidates = received sources ∩ chain members in
order, default `[origin]`; refusal or transport failure falls
through; exhaustion surfaces typed `ErrUnavailable` (the "request a
resend" state) · D-212 source-side judgments: `medialet-mismatch` →
**409** (an alarm, not a miss); attestation matching compares sig
(normative) plus kid and created defensively against our records;
reservation `max_size` ≠ Manifest size → request-level 400
`malformed`; URN absent from the Manifest → per-entry refused
`not-available`; budget = accepted `delegations` rows per (envelope,
urn), constant default 10 (the per-mailbox `delegation_budget`
override and the expiry refund sweep deferred); dedup on (requester,
request_id) reconstructs the prior response from stored rows ·
D-213 S4.7 scope: the API *backbone* plus endpoints whose machinery
is green — inbox rollups, composer/send, library, junk, WebAuthn and
`/undo` deferred to S4.8–S4.11 per the frozen order (the undo journal
table waits for its first triage consumer); client-side problem
codes add `csrf-required`; accept's mailbox-authorization check
waits for the messages materialization (single-user posture, noted
in code) · D-214 auth posture: password fallback hashes with
PBKDF2-HMAC-SHA256 (RFC 8018, stdlib-only, verified against the
RFC 7914 §11 vectors, default 210k iterations) — argon2id preferred
once a dependency is acceptable, WebAuthn lands in S4.11; session
tokens are 32-byte random, blake3-hashed at rest (D-192), HttpOnly
SameSite=Lax; unknown address and wrong password answer identically ·
D-215 SSE architecture: the `events` table is the source of truth —
every Emit journals first, then fans out in-process; Last-Event-ID
replays from the journal, making resume exact and slow consumers
safe (dropped live frames recover on reconnect) · D-216 accept
semantics: direct vs forwarded chosen by the Delivery Record's hops;
direct issues the §7.6 upgrade snapshot and POSTs it to the
§5-discovered origin `/verdict` (hookable `PostVerdict`); forwarded
runs `RequestFulfillment`; the origin-side snapshot recorder now
writes `timeline_events` (D-149) for delivery-linked dispatches ·
D-217 S4.8 harness posture: the sanitizer core is DOM-free over a
plain tree; production parsing is the PLATFORM HTML5 parser
(<template> fragment context — §11.5 step 1's spec-compliant tree
construction); the node harness feeds the same pipeline through a
clearly-labeled test-only fragment parser sufficient for the fixed
corpus — the seam is documented, not hidden; conformance comparison
is the D-94 tree equality (attrs unordered, text exact) · D-218
profile-reading judgments: `ol` and `time` are permitted (they
appear in §11.2's attribute column; the element column omission is
editorial); `th scope` values pass unconstrained (spec silence);
a style attribute whose every declaration is filtered drops
entirely; embeds with an *absent* src are removed like invalid ones;
degradation's derived text is computed from the parsed input tree
(a pure text projection, safe on any tree) · D-219 body-viewer
posture: dual sanitization at render through the same module
(D-31); urn→`/bs/o/{urn}` mapping happens on the fresh DOM build —
presentation, never artifact mutation (D-168); urn links dispatch a
cancellable `mlp-open-urn` event (navigation is consent, D-31);
external links get noopener/noreferrer + title disclosure; degraded
bodies render as <pre> text; controls forced, autoplay never (§11.2)
· D-220 CI: the client-checks job activates as reserved — the TV-005
node gate runs before tsc; the tsc --noEmit JSDoc check covers
shipping code (lib/ + app/) only, the harness's gate being its own
execution · D-221 S4.9 scope: the flat rolled-up list with the
triage trio and both views (inbox filters done=0; junk lists
quarantined threads) — bundles, sections, hoisting, and sweep are
the S4.11 triage refinement; read-on-open is fire-and-forget without
an undo bar (exposure marking is not deliberate triage) · D-222
threading (D-110 realized): a message joins the thread of the
message its in_reply_to names; an unknown parent roots its own
thread (the tree root is unknowable without the parent — a wrong
guess is worse than a short thread); re-delivered Medialets dedup on
UNIQUE(mailbox, medialet); new activity resets done=0, resurfacing
the thread; quarantined recipients materialize with junk=1, rejected
recipients materialize nothing · D-223 rollup and body posture: the
rollup derives from Medialet fields alone (subject, author, media
count); the server-side render-form derivation and derived-text
snippet are deferred to S4.11 WITH their first consumers (D-165
derived-text-first junk payloads, the D-21 classifier) — until then
`/threads/{id}` serves the verbatim body and the TV-005-proven
client pipeline is the sanitization boundary; accept now requires
the mailbox's messages row (closing the D-213 note) and flips the
accepted ref offered→expected under the §10.3 trigger · D-224 client
posture: one-way store with per-slice lifecycle subscription
(D-114); api.js mints an Idempotency-Key per mutation (D-169);
EventSource's native Last-Event-ID reconnection is the whole resume
story (the S4.7 journal replays; zero client bookkeeping); the CSP
grants style-src 'unsafe-inline' for sanitizer-permitted style
*attributes* only — no <style> element survives §11 and app chrome
escapes through html`` — everything else is 'none'/'self'; the undo
bar's 30 s client timer mirrors the server TTL · D-225 S4.10 send
posture: `Send()`'s first clock read stamps the Medialet, later
reads stamp the dispatch (the TV-001 two-timestamp shape falls out
naturally); pre-flight = recipient grammar + manifest caps +
possession (D-135/D-84); the §11 body-conformance assert joins the
deferred server render-form work; a failed target does not unsend
the others (per-target outcomes; `dispatch.failed` timeline events);
author/1 keys come from own_keys via the generalized `keyWithRole`
(D-13: signing is the SN's act) · D-226 origin materialization: the
sender's own copy is a messages row with `envelope_in` NULL (the
0001 schema anticipated it), read=1, threaded by D-110 so replies
land home; outbound refs are `promised` (§10.5, the CHECK demands
it); one deliveries row per send job, dispatches linked by
delivery_id per domain · D-227 intra-domain upload door: the same
`bs` transactional core behind `LocalPatch`/`LocalHead` (one code
path, the S3.3 Stage 4 note realized) with session auth replacing
RFC 9421 (D-79); chunks self-digest (TLS+session carries transport
integrity; the URN completion check stays absolute) and a
client-supplied Content-Digest must match or 422; client-side
BLAKE3 (file drag-in) waits for the vendored WASM in S4.11 — until
then the composer attaches by reference only, and the upload lane
is exercised by tests and non-browser clients · D-228 the 10 s undo
hold is purely client-side (D-138: cheap, expected, the last moment
before a signature); the server signs the instant it is told · D-229
the Go render pipeline: parsing is x/net/html (spec-compliant HTML5
tree construction, §11.5 step 1) fetched via the GitHub-mirror
replace directive; TV-005 under tree equality is the
third-implementation bridge (Python generator, JS client, Go
server); the stored render form serializes deterministically
(sorted attributes — comparison stays tree equality, bytes stay
reproducible); migration 0003 adds only `render_degraded` — 0001
had already provisioned render_form/derived_text (the collision was
caught by the migration failing loudly; checked, shrunk); pre-0003
rows backfill lazily on first read · D-230 derivation wiring: at
ingest BEFORE recipient evaluation, so the D-21 classifier judges
the derived text — it demotes tier-2 strangers only (correspondents
and same-domain are never classifier-demoted); at send; the rollup
gains the D-132 snippet; `/threads/{id}` serves the render form —
the D-31 dual duty is now real in both directions (server derives,
the TV-005-proven client re-sanitizes); degraded bodies ship the
derived text, flagged · D-231 media judgments: the `OnVerified` seam
keeps `bs` mailbox-agnostic while the API layer flips
expected→available (§10.3); owner delete transitions pinned rows too
(D-88: pin protects from GC, never from the owner) and removes the
bytes; object serving hardens with nosniff + `CSP: sandbox` +
immutable caching (content-addressed); Range serving is QoI; the raw
Signed Medialet endpoint answers recipients only (D-28 fidelity
source) · D-232 junk semantics (D-165): release = junk 0 + the
correspondent `allow` override — the strongest signal, and it
outranks the classifier (tested); block = `block` override + done,
the thread staying in junk for the record · D-233 deferrals:
WebAuthn → S4.12 with the guest/claim auth surfaces; the blake3-wasm
file door → S4.12–13 demo preparation; bundles/sections/sweep remain
the parked S3.11 backlog.

## 4. Environment recipe (sandbox)

```
apt-get install -y golang-go gcc          # Go 1.22 via apt
export GOPROXY=direct GOSUMDB=off         # module proxy unreachable;
                                          # github.com is reachable
cd server && go vet ./... && go test ./...
python3 conformance/generators/tv-00N.py  # needs: pip blake3 rfc8785
                                          # cryptography beautifulsoup4 html5lib
```
`mattn/go-sqlite3` (cgo) validates here; `modernc.org/sqlite` is the
shipping driver (one import swap) — D-191.

## 5. Next: the remaining build order (Stage 3 Closing §5)

**S4.3 — done** (see table). Discovery + Domain Document: hardened fetch profile
(§5.4/D-59: redirect/size/timeout limits, SSRF address filtering),
`domain` binding check, kid self-verification wired on key-set load,
24 h cache ceiling into `domain_docs`/`domain_keys` (D-33); TV-001's
Domain Document fixture as the parsing anchor.
**S4.4–S4.11 — done** (see table); every §11 duty is now
implemented on both sides of the wire and the D-223 deferral is
repaid. **S4.12 (next)**: guest + claim (S3.6, D-151–D-155) — the
guest delivery page (the second `<mlp-body-viewer>` consumer),
capability links, the claim ceremony into a mailbox; WebAuthn joins
here per D-233 (the auth surfaces travel together); blake3-wasm
vendoring for the composer's file door. Then S4.13 two-domain demo
(Stage 3 Closing §5 definition of done) · S4.14 hardening + operator
guide + NLnet
gated on TV-005 tree equality FIRST, then Inbox → composer →
Deliveries → Media → identity/junk) · S4.12 guest+claim · S4.13
two-domain demo (definition of done in Stage 3 Closing §5) · S4.14
conformance hardening + operator guide + NLnet.

## 6. The working ritual (unchanged)

Per session: design/implementation presented with lettered judgment
calls → Igor confirms explicitly → decisions frozen with sequential
D-numbers (next free: **D-234**) → artifacts delivered as local
commits emitted as a `git format-patch` series against `origin/main`
for Igor's review, `git am`, and push (D-196) → next-session pointer. Honesty rules: caught problems are
surfaced, never patched silently; spec gaps go to the MEP queue;
conformance claims are machine-verified.

## 7. Open editor actions

1. Decide MEP-001 and MEP-002 (accept/reject/amend).
2. Product brand name before launch (D-180).
3. ~~Publish the repository~~ — done: github.com/1token/mlp is live
   and is the continuation carrier (cloned directly in S4.3; D-40,
   D-196).
