# MLP — Stage 4 Continuity Brief

> Purpose: resume implementation in a fresh working session with zero
> context loss. Read this first; everything else is referenced from it.
> Updated at the S4.19 close (2026-07-16) — node-local search; MEP-003/MEP-004 filed as Drafts.

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

- `spec/MLP-Core-Specification-0.1-draft-02.md` — **frozen** (D-108).
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
| S4.15 | The documentation pack (editor-requested): `docs/README.md` (the map, normative/product/architecture/interfaces/operations); `docs/product/USER-STORIES.md` (five personas — author, reader, guest, operator, implementer — every story citing its decision and its certifying test); `docs/product/PRD.md` (functional + non-functional requirements, decision-traceable, status-honest incl. PARTIAL/PLANNED); `docs/architecture/ARCHITECTURE.md` (system context, server + client module diagrams, deployment, the three background loops), `DOMAIN-MODEL.md` (the entities and their loyalties), `DATA-MODEL.md` (GENERATED from the migrations by `gen-er.py` — 39 tables, 5 subsystems, drift-gated in CI), `SEQUENCES.md` (five flows, each bound to its test); `api/openapi.yaml` (OpenAPI 3.0.3 for /api/v1 — 38 paths, 45 operations matching the route enumeration exactly, 18 schemas, validated in CI) | **every diagram block is machine-checked**: all 15 PlantUML blocks pass plantuml.jar syntax validation (one alias/package collision caught and fixed); the OpenAPI validates under openapi-spec-validator; the ER document regenerates byte-identically; the operation count reconciles with the grep-enumerated route surface |
| S4.16 | The scenario suite (editor-requested, TestTwoDomainDemo mold): `cmd/mlpd/scenario_harness_test.go` (N-domain `world` on one controllable clock — `config.Clock` wired through SN, BS, Client API AND the pusher), `scenarios_basic_test.go` (6: same-domain instant accept, conversation lifecycle, attach-by-reference, tier lifecycle, multi-recipient fan-out with the D-04 envelope-privacy proof on the origin's dispatch records, idempotent send), `scenarios_advanced_test.go` (6: delegated forwarding across three domains, **custody surviving the origin's death** with the MEP-001 window honored from a killed socket, D-51 loop prevention at the chain-member origin, resend-after-deletion honesty, guest lock + expiry under the moving clock, ephemeral GC vs. the pinned master); `demo/SCENARIOS.md` catalog | **all 12 scenarios green over real TCP sockets, deterministic across 3 consecutive full runs**; the suite surfaced and root-caused the §7.6 frozen-clock supersession tie (world-owned time: +1 s per protocol operation) and the unwired pusher clock |
| S4.17 | The working-group exploder (editor-requested): `cmd/mlpd/scenarios_wg_test.go` — an IETF-style WG mailing list as an APPLICATION on MLP primitives (roster app-level, like mailman on SMTP; every action pure protocol), five domains: the list/moderator, three members, a cross-subscribed archive mirror. Seven phases: post → fan-out (per-domain Forward, D-04 preserved; authorship + CA byte-identical at every subscriber) → **heavy media never touches the list** (§9.3 delegation, the 5 MiB draft point-to-point from the author, `objectLive(lists)==false` asserted) → threading across the exploder (D-110: the reply joins the same topic everywhere because the CA is the identity) → moderation as the tier system (blocked troll `quarantined`, never inboxed, never exploded; release = approval) → the cross-subscription loop dying at one revolution (the boomerang provably arrives — two Delivery Records — and the automatic re-explosion refuses with `ErrForwardLoop`, D-51); every member exactly one copy | the scenario passes over real sockets, deterministic across 3 consecutive full-suite runs; the exploder posture is `automatic=true` — an exploder IS automation, and that honesty is what lets D-51 protect the federation |
| S4.18 | Windows portability fix, editor-reported from a real Windows run: every upload's FINAL chunk answered 500 because `finalize` renamed the quarantine file while the handler still held its handle — routine on POSIX, a sharing violation on Windows. Fix in `bs.go`: close-before-promote (the bytes are already durable — the §8.4 checkpoint `Sync()` precedes it; the deferred Close double-closes harmlessly); the promote is now idempotent when the destination already exists (racing pushes of one URN: both verified against the URN's BLAKE3, identical by construction, the loser's partial is removed); `resetToZero`'s open-handle `os.Remove` documented as tidiness-not-correctness (both call sites `Truncate(0)` first). Audit of all five rename/remove sites: the other three are handle-free | full Linux suite green (9 pkgs; demo + 13 scenarios ×2); `CGO_ENABLED=0 GOOS=windows go build` of bs/sn/clientapi/core passes; the Windows test run itself is the editor's verification |
| S4.19 | Node-local search (D-261): the tenth package `search/` — pluggable stdlib-only `Extractor` registry (DOCX/XLSX via zip+xml; minimal in-house PDF: Flate + Tj/TJ, kerning-gap word boundaries, printability filter dropping undecodable subset-font runs; plain text; 32 MiB in / 512 KiB out, D-264/D-267), `Indexer` with the per-URN `object_text` cache + FTS4 `search_fts` (unicode61 remove_diacritics — 'zilina' finds 'Žilina'; FTS4 because the pinned mattn build ships it tag-free where FTS5 needs a build tag, D-263); migration 0006; extraction at the `OnVerified` moment in `buildNode` + query-time `SyncMedialets`/`SyncObjects` self-heal (covers the sender-side gap: uploads verify before the send creates its promise rows) + `Reindex` (D-266); `GET /api/v1/search` — mailbox-scoped through the refs/messages joins, grouped per message with `via: message\|media`, bracket snippets, newest-first, paging, hostile queries sanitized to bare lowercased terms (D-265/D-267); OpenAPI 0.1-draft-02 + Client API draft-02 (one additive section, D-268); ER group + regeneration; the fourteenth scenario `TestScenarioSearchFindsTheShoot` | Milan finds the shoot by a word that exists ONLY inside the delivered PDF; 'nahlady' finds 'náhľady'; Petra's own sent copy self-heals into her index at first query; a miss is an honest empty page; **10 packages green**, three client gates + tsc (CI-pinned 5.5), MUST audit + all 7 vectors byte-identical, OpenAPI valid, ER regenerates byte-identically |
| S4.14 | Conformance hardening + operations + funding: the D-104 audit as a two-script instrument (`audit-musts.py` extracts the 69-line MUST corpus, `audit-annotate.py` refuses unannotated entries and emits `MUST-AUDIT.md`; CI regenerates both and fails on drift, so a spec edit forces an audit decision) — **50 of 64 testable requirements COVERED**, 2 partial, 7 open-client + 5 open-deferred, each gap decision-tied; new failing-input tests (`sn/must_test.go`: JSON-dialect refusals incl. floats/nulls/epoch/duplicate urns, unknown-member tolerance as a positive case, the §4.1 grammar battery, §9.2 interloper-source never contacted); a genuine grammar gap found and fixed at the root (`validRun` admitted `_` in domain labels against IDNA2008 LDH — underscore now rides `extra` for local atoms only, plus label hyphen-position rules); the D-139 GC-first class on `refs.ephemeral` (0001 had the column; auto-granted refs now carry it) with `SN.CollectGarbage` honoring the §10.5 invariants (pinned retains absolutely; atomic tombstone flip; only-unavailable immediately collectable; standard class untouched) wired hourly into mlpd; `docs/OPERATOR.md` (D-180) and `docs/NLNET-APPLICATION.md` (D-42, D1–D5 each mapped to committed artifacts, €38k shaped against the audit's own open items) | **the audit is the deliverable that makes every other claim checkable**: TestGCInvariants passes first run; TestNonChainSourceNeverContacted proves the §9.2 filter; the grammar battery caught a real hole; nine packages + three client gates + seven vectors + the audit reconcile green |
| S4.13 | The two-domain demo (Stage 3 Closing §5) + the composer's file door: `cmd/mlpd` (one binary per domain — SN + BS + Client API + static client + guest page + push loop; demo mode via `-peer` through `discovery.NewDemoFetcher`, loudly logged); `TestTwoDomainDemo` walks every §5 bullet over real TCP sockets; D-139 auto-grant implemented as the `sn.AutoGrant` recipient-policy knob (spec default stays defer-all — TV-002 reproduces); Send now writes the correspondents ledger (`first_outbound_at`), unlocking §7.5 `have` disclosure; vendored pure-JS blake3 (`@noble/hashes`, no wasm, CSP intact) + `lib/mlet-urn.js` gated on the TV-001 media address (run-urn.js in CI); the composer's hash-first file door with tus resume; `demo/run.sh` + `demo/DEMO.md` (the on-camera script) | **all seven definition-of-done bullets pass programmatically**: strangers' preview auto-grants and renders alive while the master defers; the push killed after one 2 MiB chunk sits at `pushing`/2097152 and resumes to a byte-verified object; the correspondent's resend answers `have` and accepts instantly; the reply threads and sweeps; the guest claims and instantly has; three genuine gaps surfaced and fixed at the root (auto-grant unimplemented, correspondents never written, StripPrefix breaking @target-uri) |
| S4.12 | Guest + claim (S3.6) and passkeys (S3.8/D-233): migration 0005 (pin_failures + WebAuthn tables over the 0001-provisioned guest_links/guest_downloads); guest links minted at Send for explicitly named guests (hash-stored tokens, per-draft 6-digit PINs for the sender's second channel, the D-153 notifier hook carrying the link only, 30-day expiry); sessionless guest endpoints with the D-155 five-failure lock; payload = the render form (one viewer, two hosts — views never recorded, downloads recorded per D-147); the claim (D-154): mailbox minted, the original SM re-dispatched through the REAL local ingest (self-domain verificationKey path via own_keys), session issued, link surviving, one claim per link; instant-have as the possession short-circuit heading handleAccept (offered→expected→available in one action — claims, same-domain sends, D-26 dedup alike); `webauthn/` dependency-free (strict fixed-shape CBOR, fmt "none", ES256 + Ed25519, single-use 5-min challenges, sign-count regressions logged); register/login endpoints; client guest.html + mlp-guest.js (second viewer host, PIN prompt, blob downloads, claim + navigator.credentials) | **the guest journey passes end to end** — PIN gate, lock, un-tracked views, tracked downloads, claim → thread in the new inbox → `{state:"available", instant:true}` with no bytes moving, the link surviving its claim, expiry at day 31; the passkey ceremonies pass with synthetic authenticators (challenge reuse and tampered assertions refuse); found and fixed the drafts dialect break (untagged ManifestEntry marshaled CamelCase against D-170 — now snake_case at the root) |
| MEP-001 + MEP-002 | Both accepted (editor decision 2026-07-12). Spec → **draft-02** (rename + changelog): §3.4.1 `until`, §10.3 effective offer deadline, §9.5 declarant binding (MEP-001); §3.2.2 `preview_of` (MEP-002). Migration 0004 (`refs.preview_of`). TV-006 (custody `until` past the Manifest window) + TV-007 (preview_of validation outcomes) with committed generators. Go: strict `until` validation + parsed `Sources`, `effectiveDeadline` into `refs.available_until`, `ExpireOffers` lazy sweep, `Forward(…until)` custody param, `ownDeclaredUntil` §9.5 own-record binding, two-pass order-independent preview_of strip (ingest + compose parity), media fold hint; client media card folding | **TV-006 custody envelope byte-identical** (1728 canonical bytes); the effective deadline is the declared `until`, not the passed Manifest date; the source honors exactly its own hop-signed window (will-push inside, resend past); TV-007's four outcomes reproduce; frozen TV-001–005 untouched |
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
the parked S3.11 backlog. · D-234 MEP process posture: accepted MEPs
roll into the spec as draft-02 (git-mv rename + changelog); the wire
version stays 0.1 (additive members, D-101); frozen vectors TV-001–005
untouched; TV-006/TV-007 arrive with committed generators the CI glob
covers · D-235 MEP-001 implementation: the effective offer deadline
computed at materialization into refs.available_until (the latest of
the Manifest window and every covering source's until), consumed by
§10.3 via the lazy ExpireOffers sweep on list reads; a malformed
`until` is a malformed envelope (a known member, not D-43 unknown);
Forward(…until) carries the custody window (self entry only); a
source is bound solely by the until it itself hop-signed, validated
against its own dispatch records with a manifest-coverage check
(ownDeclaredUntil) · D-236 MEP-002 implementation: preview_of
constraints in a second pass over a pre-strip snapshot
(order-independent); violating members stripped, never fatal;
migration 0004 refs.preview_of, populated at materialize and compose;
the composer strips by the same rule (stripInvalidPreviews); the
member is descriptive only (auto-grant keys on size, D-139); media
cards carry the fold hint and the client folds · D-237 guests are
named explicitly in the draft (guests[]), never in envelope_to and
never inferred from dispatch failure; a guests-only send is legal ·
D-238 PIN posture: per-draft flag, 6 digits, disclosed once to the
SENDER (D-152's second channel is theirs); the D-153 notification
carries the link only; five failures lock the link (423, checked
before PIN evaluation); correct entry resets the counter; 30-day
default expiry · D-239 D-147 verbatim at the API: guest payload
reads record nothing; downloads write guest_downloads + a timeline
fact · D-240 the claim is possession-as-ceremony: link + PIN →
mailbox → re-dispatch of the original Signed Medialet through the
real local ingest (author signature preserved) → session; the
self-domain verificationKey path consults own_keys (a domain knows
its own keys; §6.3 resolution is for remote attribution); the link
survives; one claim per link (409) · D-241 instant-have generalized:
possession short-circuits handleAccept before any verdict lookup
(the mailbox's refs row is the authorization) — claims, same-domain
sends, D-26 dedup, one rule; the remote defer path is the
fallthrough · D-242 WebAuthn scope: attestation fmt "none" only,
ES256 + Ed25519 COSE keys, hand-rolled strict CBOR (no indefinite
lengths, dup-key and trailing-byte refusal, depth/size caps),
single-use 5-minute challenges, unknown-address login/begin
indistinguishable (empty allow-list), sign-count regression logged
not fatal · D-243 one drafts dialect (D-170): ManifestEntry carries
snake_case JSON tags — the latent CamelCase marshal was a bug fixed
at the root, not papered at call sites; blake3-wasm stays deferred
to S4.13 (D-233 restated). · D-244 in-browser hashing is the
vendored pure-JS `@noble/hashes` blake3 (seven ESM files, two
mechanical patches documented in the vendor README) rather than
wasm: the client CSP keeps `script-src 'self'` with no
`wasm-unsafe-eval` concession; wasm remains a post-1.0 performance
option; the client's address construction is CI-gated on the TV-001
media address (run-urn.js) · D-245 D-139 auto-grant is a RECIPIENT
POLICY knob (`sn.AutoGrant`; product ships `sn.D139AutoGrant` on):
the spec default stays defer-all at Tier 2 — exactly what TV-002
froze, and the blanket first implementation broke that vector, which
is the knob's justification; auto-granted refs step
offered→expected at materialization and complete to available on
arrival with no user action; possession is never disclosed to
strangers (grant, not have — §7.5 masking); the ephemeral GC-first
store class waits for S4.14 accounting · D-246 Send records every
recipient in the correspondents ledger (`first_outbound_at`,
COALESCE-keep) — the demo surfaced that tiers were read but never
written; §7.5 have-disclosure to correspondents is now reachable ·
D-247 mlpd composition: demo relaxations (`NewDemoFetcher`,
`sn.AllowInsecureTransport`, plain-HTTP pusher) exist behind `-peer`
only and log DEMO MODE; the BS mounts WITHOUT StripPrefix because
`r.URL.RequestURI()` reconstructs from the path StripPrefix mutates,
silently breaking the RFC 9421 @target-uri; PostVerdict speaks
`application/mlp-verdict+json` and surfaces non-2xx as errors ·
D-248 the demo's crash is simulated at the source read — the row
stays `pushing` with the receiver's checkpoint intact, which is what
kill -9 actually leaves — not via transport errors, which walk the
retry ladder to `failed`; the resend bullet runs AFTER the reply
because §7.5 keys `have` on correspondent tier; store routing and
topic bundles/sweep are documented in DEMO.md as parked (S3.7/S3.11,
D-233) rather than silently skipped. · **D-249** the D-104 audit is a
generated, drift-gated instrument: the corpus extractor and the
annotator both run in CI and `git diff --exit-code` the outputs; the
annotator hard-fails on any corpus entry without a status, so every
future MUST forces an explicit COVERED/PARTIAL/OPEN decision; the
status vocabulary (COVERED, PARTIAL, OPEN-CLIENT, OPEN-DEFERRED,
TRANSITIVE, META) is defined in the generator · **D-250** audit-found
grammar fix at the root: domain labels are IDNA2008 LDH (no
underscore, no leading/trailing hyphen); `_` moved out of validRun's
base charset and into the local-atom call sites' `extra` — §4.1's
atom grammar keeps it · **D-251** the D-139 class lives on
refs.ephemeral (as 0001 provisioned): auto-granted references carry
it from materialization; `SN.CollectGarbage` collects a live object
only with no pinned ref AND (all refs unavailable OR every
non-terminal ref ephemeral), flipping stranded references to
unavailable(expired-local) atomically with the row's removal, file
unlink after commit; standard-class reclamation is operator policy,
per §10.5's latitude, documented in OPERATOR.md; mlpd sweeps hourly ·
**D-252** the deferred set is named, not hidden: segments (§8.6), the
DNS hint path (§5.3), per-user resolution (§5.6), and seven
client-presentation MUSTs sit in the audit as OPEN with their
decisions; the NLnet budget is shaped against exactly this list ·
**D-253** the application package (docs/NLNET-APPLICATION.md) claims
only committed artifacts — D1–D5 each cite their repo paths and the
CI gates that keep the claims true; €38,000 across audit-gap
hardening, the separately-branded flagship, interop packaging, and
documentation; E2E and v2 explicitly reserved for a follow-up
(D-42's tight-scope rule restated). · **D-254** documentation
posture: diagrams are PlantUML in fenced Markdown blocks
(VS-Code-renderable per the editor's tooling), machine-checked with
plantuml.jar; generated documents (DATA-MODEL.md, the audit pair)
are edited only through their committed generators and drift-gated
in CI; OpenAPI covers the CLIENT API only — the federation surface
is signature-driven and its normative interface description is the
core specification, which OpenAPI's request/response frame would
misrepresent; the PRD is scoped to the PRODUCT (reference
implementation + flagship posture) because the protocol's
requirements document is the audited spec itself; user stories and
PRD rows cite decisions and certifying tests so the documentation
inherits the register's checkability. · **D-255**
scenario-suite posture: `config.Clock *time.Time` injects one
controllable clock through every component including `bs.Pusher.Now`
(the suite exposed the pusher as the one unwired consumer — frozen
receivers rejected real-time signatures on §6.6 freshness); the
world advances one second per protocol-visible operation because
§7.6 supersession ties at frozen `created` fall to a verdict_id
comparison that real deployments resolve via UUIDv7 millisecond
ordering — meaningless when milliseconds are frozen (a possible
future §7.6 clarification note / MEP candidate, editor's call);
forwarding scenarios drive `SN.Forward` directly and dispatch the
returned envelope over the real socket — a client API forwarding
endpoint remains client backlog; semantics pinned by the suite:
D-51's loop signature is the forwarding domain already in the
chain (not the destination), a blocked sender's outcome reads
`quarantined` (sender-visible policy, not a bounce), `block`
sweeps the existing record thread out of the inbox, D-04 is
asserted sender-side on `dispatches.envelope_canonical`, and the
D-139 line (4 MiB) selects which accept path a scenario exercises. · **D-256** the exploder
posture: mailing lists are APPLICATIONS on MLP, not protocol — the
roster is exploder data (as with mailman on SMTP) and MLP defines no
membership semantics; an exploder re-dispatches with
`automatic=true` because it IS automation, which is precisely what
arms D-51 against cross-subscription loops (the deliberate/automatic
distinction carries the whole loop-safety story); exploders re-
dispatch in Delegated mode by default — the list never holds
payloads, heavy media flows point-to-point from the author (a
custody-mode archive list is the already-proven MEP-001 variant,
composable without new machinery); moderation needs no protocol
surface — the tier system (block/quarantine/release) IS the
moderation queue. · **D-257** store
portability posture: the object store must be correct under both
POSIX and Windows file semantics — no rename or remove of a file
while holding its handle on the hot path (close-before-promote in
§8.4 finalize), and the content-addressed promote treats an
already-present destination as success (identical bytes by
construction: both sources verified against the URN's BLAKE3).
`ObjectPath` already strips `urn:mlet:` so no reserved characters
reach the filesystem; future file-touching code inherits this
posture. · **D-258** versioning policy: the 0.1 line is the
only line — additive capabilities land via MEPs and consolidate into
numbered drafts; 1.0 is declared at post-funding stabilization; "v2"
is reserved for wire-incompatible changes and does not exist today.
· **D-259** MEP-003 (bao verified streaming) is core-spec territory:
an optional negotiated capability — the BLAKE3 root every
`urn:mlet:` already carries commits to the whole Merkle tree, so
verified streaming and verified range slices need ZERO identifier
changes; outboard trees are private derived data (never in a
Manifest); the §8.4 checkpoint stays REQUIRED; acceptance triggers
core draft-03 + TV-008. · **D-260** MEP-004 (mailing-list profile)
is a companion document, not core text: the `spec/profiles/` series
(independently versioned, normative for claimants only), S4.17 as
the reference implementation, at most a one-line §14 registry
reservation ever touching core — email's shape (5321/5322 core,
2919/2369 layered), copied deliberately. · **D-261** search is
implementation, not protocol: local per-mailbox FTS over derived
text + extracted media text; documents are just a media type; one
additive Client API endpoint; cross-domain search explicitly out of
scope (D-04 envelope privacy). · **D-262** sequencing: search
implemented first (S4.19), MEP-003/004 filed as Drafts in the same
session; draft-03 is cut only on MEP-003 acceptance. · **D-263**
FTS4, not FTS5: the pinned mattn/go-sqlite3 default build ships FTS4
tag-free where FTS5 needs `sqlite_fts5`; build/test commands stay
tag-free on both OSes; unicode61 `remove_diacritics=1`; FTS5 remains
a one-migration swap. · **D-264** extraction is stdlib-only with
documented limits: OOXML via zip+xml; PDF minimal in-house (Flate,
Tj/TJ, kerning-gap spaces, printability filter; no ToUnicode, no
encryption); pluggable `Extractor` interface for production parsers;
the dependency posture (and the NLnet supply-chain story) stays
clean. · **D-265** index architecture: `object_text` is a per-URN
cache shared across mailboxes (content-addressed: extract once) —
safe because results scope through the refs/messages joins at query
time; `search_fts` is node-global with kind/key unindexed; the
DECLARED Manifest type chooses the extractor — bytes are never
sniffed to pick a parser. · **D-266** self-healing triggers:
synchronous extraction at `OnVerified` (prototype-honest; production
backgrounds it); `SyncMedialets` + `SyncObjects` before every query
(covers the sender-side gap and restores); unreferenced objects are
not negative-cached; `Reindex` rebuilds everything. · **D-267** caps
and query shape: 32 MiB read / 512 KiB text per object; queries
sanitized to lowercased bare terms (FTS operators neutralized),
implicit AND, trailing `*` = prefix; newest-first (mailbox search is
chronological, like the inbox); 400-row bound grouped in Go;
bracket-marked snippets. · **D-268** S4.19 scope: server + API +
scenario; the client search UI is S4.20; OpenAPI bumped to
0.1-draft-02 alongside Client API draft-02 (additive; draft-01
clients unaffected).

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
repaid. **Stage 4's planned substages are complete.** What remains is editor territory: submit the NLnet application (D-42), publish/announce (D-41's protocol-venues-first sequencing), and open S4.15+ only against the audit's OPEN list or new MEPs. Formerly: guest + claim (S3.6, D-151–D-155) — the
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
D-numbers (next free: **D-269**) → artifacts delivered as local
commits emitted as a `git format-patch` series against `origin/main`
for Igor's review, `git am`, and push (D-196) → next-session pointer. Honesty rules: caught problems are
surfaced, never patched silently; spec gaps go to the MEP queue;
conformance claims are machine-verified.

## 7. Open editor actions

1. ~~Decide MEP-001 and MEP-002~~ — done 2026-07-12: both accepted;
   spec at draft-02; TV-006/TV-007 anchored.
2. Product brand name before launch (D-180).
3. ~~Publish the repository~~ — done: github.com/1token/mlp is live
   and is the continuation carrier (cloned directly in S4.3; D-40,
   D-196).
