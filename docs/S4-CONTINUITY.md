# MLP — Stage 4 Continuity Brief

> Purpose: resume implementation in a fresh working session with zero
> context loss. Read this first; everything else is referenced from it.
> Updated at the S4.4 → S4.5 boundary (2026-07-09).

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
  (S4.1), `store/` (S4.2), `discovery/` (S4.3), `sn/` (S4.4).
  `client/` — not started.

## 3. Stage 4 state

| Session | Delivered | Proof |
|---|---|---|
| S4.0 | Repo scaffold, MEP-001/002 filed, TV-002–004 generators reconstructed | all five vectors regenerate **byte-identically**; CI gate |
| S4.1 | `core/`: JCS (D-43 dialect, own RFC 8785 writers), multiformats, kid self-verify, SignDoc/VerifyDoc with label context match | `go test` recomputes **every** TV-001 value incl. deterministic sigs and UUIDv7s |
| S4.2 | `store/`: 0001 migration (~30 tables), runner (`user_version`), D-87 state machine **enforced by trigger** | legal walk green; 6 forbidden transitions abort; replay-unique; reservation terminal |
| S4.3 | Generator-debt repair (D-197); `discovery/`: Domain Document parsing (§5.2/§6.1–6.3), hardened fetch (§5.4), Resolver with 24 h ceiling + unknown-kid re-fetch + negative cache (§5.5) | TV-001 `domain_document` fixture is the parsing anchor; 21 tests green incl. dial-time SSRF wiring proof; all five vectors regenerate byte-identically **for real** now |
| S4.4 | `sn/`: §3.4.4 validation sequence, §7.3 `/dispatch` with D-74 retry idempotency, §7.4 verdict generation + verification, §7.5 reservations, §7.6 `/verdict` updates with the transition table, §7.7 default tiers, RFC 9457 problems | dispatching the TV-001 envelope reproduces TV-002 **verdict 1 byte-identically** (708 B, exact sig); recipient-accept reproduces **verdict 2** (923 B) and mints the reservation; failure matrix maps every §3.4.4 item to its §7.8 code; deny→grant refused as `invalid-transition` |

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
baseline.

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
**S4.4 — done** (see table). **S4.5 (next)**: tus transfer +
transactional PATCH (TV-003), consuming `reservations_in` (token-hash check, `hasher_state` BLAKE3 checkpoints per D-27/D-77) and `reservations_out` (push side, §7.5 D-72 pusher connection safety reusing the discovery address filter) · S4.6 forwarding/delegation (TV-004) ·
S4.7 Client API + SSE · S4.8–11 client (Body viewer + JS sanitizer
gated on TV-005 tree equality FIRST, then Inbox → composer →
Deliveries → Media → identity/junk) · S4.12 guest+claim · S4.13
two-domain demo (definition of done in Stage 3 Closing §5) · S4.14
conformance hardening + operator guide + NLnet.

## 6. The working ritual (unchanged)

Per session: design/implementation presented with lettered judgment
calls → Igor confirms explicitly → decisions frozen with sequential
D-numbers (next free: **D-205**) → artifacts delivered as local
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
