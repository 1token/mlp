# MLP — Stage 4 Continuity Brief

> Purpose: resume implementation in a fresh working session with zero
> context loss. Read this first; everything else is referenced from it.
> Updated at the S4.2 → S4.3 boundary (2026-07-08).

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
  (S4.1), `store/` (S4.2). `client/` — not started.

## 3. Stage 4 state

| Session | Delivered | Proof |
|---|---|---|
| S4.0 | Repo scaffold, MEP-001/002 filed, TV-002–004 generators reconstructed | all five vectors regenerate **byte-identically**; CI gate |
| S4.1 | `core/`: JCS (D-43 dialect, own RFC 8785 writers), multiformats, kid self-verify, SignDoc/VerifyDoc with label context match | `go test` recomputes **every** TV-001 value incl. deterministic sigs and UUIDv7s |
| S4.2 | `store/`: 0001 migration (~30 tables), runner (`user_version`), D-87 state machine **enforced by trigger** | legal walk green; 6 forbidden transitions abort; replay-unique; reservation terminal |

Register tail since the Stage 3 closing doc: **D-182–D-195** —
D-182 repo/CI · D-183 MEP template · D-184/185 MEP-001/002 filed ·
D-186 generator debt paid · D-187 module + zeebo/blake3 (mandated by
§6.4) · D-188 JCS approach (dialect violations are errors) · D-189
core API surface · D-190 TV-001 green acceptance · D-191 driver
posture (ship modernc.org/sqlite; validate with mattn in sandbox;
code driver-agnostic) · D-192 schema conventions (RFC3339 TEXT;
JSON-as-TEXT; minted secrets stored as `*_hash`, presented tokens
plaintext) · D-193 refs trigger = D-87 verbatim · D-194 federation
records (dispatches = §9.5 credential store; full verdict history for
the D-149 timeline; `hasher_state` per D-27) · D-195 schema accepted.

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

**S4.3 (next)** — Discovery + Domain Document: hardened fetch profile
(§5.4/D-59: redirect/size/timeout limits, SSRF address filtering),
`domain` binding check, kid self-verification wired on key-set load,
24 h cache ceiling into `domain_docs`/`domain_keys` (D-33); TV-001's
Domain Document fixture as the parsing anchor.
Then: S4.4 `/dispatch`+verdicts (TV-002) · S4.5 tus transfer +
transactional PATCH (TV-003) · S4.6 forwarding/delegation (TV-004) ·
S4.7 Client API + SSE · S4.8–11 client (Body viewer + JS sanitizer
gated on TV-005 tree equality FIRST, then Inbox → composer →
Deliveries → Media → identity/junk) · S4.12 guest+claim · S4.13
two-domain demo (definition of done in Stage 3 Closing §5) · S4.14
conformance hardening + operator guide + NLnet.

## 6. The working ritual (unchanged)

Per session: design/implementation presented with lettered judgment
calls → Igor confirms explicitly → decisions frozen with sequential
D-numbers (next free: **D-196**) → artifacts delivered (tarball per
session) → next-session pointer. Honesty rules: caught problems are
surfaced, never patched silently; spec gaps go to the MEP queue;
conformance claims are machine-verified.

## 7. Open editor actions

1. Decide MEP-001 and MEP-002 (accept/reject/amend).
2. Product brand name before launch (D-180).
3. Publish the repository (D-40 public-repo commitment) — also the
   best continuation carrier: a public GitHub repo can be cloned
   directly in future sessions (github.com is reachable), replacing
   tarball uploads entirely.
