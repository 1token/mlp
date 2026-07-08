# Medialet Protocol (MLP) — Stage 2 Closing Document

| | |
|---|---|
| **Document** | Stage 2 Closing Document (specification-drafting record) |
| **Status** | Stage 2 complete pending the D-108 freeze sign-off |
| **Produces** | `MLP-Core-Specification-0.1-draft-01.md` (134,952 chars, 17 sections) + conformance vectors TV-001–TV-005 |
| **Date** | 2026-07-05 |
| **Editor** | Igor |
| **Supersedes** | The eleven per-section working files, which become historical artifacts on freeze |

## 1. What Stage 2 produced

Twelve drafting sessions (S2.1–S2.12) plus one pre-freeze question session
turned the Stage 1 architecture into a complete normative specification:
sections 1–14 and Annexes A–C, every claim cross-referenced, every
provisional marker retired, and a five-vector conformance family in which
**every cryptographic value is genuine and machine-verified** — computed
from the four RFC 8032 pure-Ed25519 test keys, committed as JSON with
generators, and internally continuous (TV-004 reuses TV-001's Signed
Medialet byte-identically; TV-003 pushes under TV-002's Reservation; the
final BLAKE3 equals TV-001's URN by assertion).

The consistency pass (S2.12) applied 26 asserted patches — provisional
retirement, the §3.6 fixture refresh to the final key constructions, the
§6.4 label-table completion, and the D-105–D-107 multi-BS integration —
and the audit closed clean.

## 2. Decision register, D-43–D-107

**S2.1 — Skeleton & serialization.** D-43 JSON wire conventions
(integers-only, RFC 3339 UTC, snake_case). D-44 JCS document-signing
convention with protected metadata and domain-separation labels. D-45
RFC 9421 for HTTP signing. D-46 Medialet-ID (UUIDv7 recommended; (author,
id) scope). D-47 Medialet schema v1. D-48 single-document spec, session
plan.

**S2.2 — Entity model.** D-49 signed-document wire form; omit-absent/no-
null; *amends D-47*: `in_reply_to` by content address. D-50 Envelope
schema (no endpoint URLs; `forwarded_by`; `fulfillment_sources`). D-51
Hop Attestation chain (privacy-reduced; root = delegation credential;
loop rule). D-52 caps and the seven-step ingest validation. D-53 Delivery
Record minimum. D-54 TV-001 adopted.

**S2.3 — Addressing & discovery.** D-55 address grammar, three derived
forms, tag semantics. D-56 no reserved local parts. D-57 Domain Document
schema with the `domain` binding member. D-58 DNS hint semantics
(DNSSEC-validated = delegation; disagreement = hard fail). D-59 hardened
fetch parameters and caching. D-60 SN-mediated resolution.

**S2.4 — Keys & signatures.** D-61 multicodec key encoding. D-62 final
kid construction (algorithm-bound; self-verifying; DNS-selector-sized).
D-63 Ed25519 profile; exact-domain authority; no self-signed Domain
Document. D-64 signature-label registry. D-65 DNS key records. D-66
RFC 9421 profile (two hashes, two jobs). D-67 TV-001 final form.

**S2.5 — Negotiation.** D-68 SN API surface; client interface out of
scope. D-69 signed verdicts only for verified Envelopes. D-70 *amends
D-16*: per-recipient verdicts; per-URN stays domain-level union-need.
D-71 verdict/Reservation schemas; idempotent snapshot updates; transition
table. D-72 pusher-side IP safety on `target_url`. D-73 reason-code
registry. D-74 *refines D-20*: retry idempotency vs. replay. D-75 TV-002.

**S2.6 — Transfer.** D-76 tus 1.0 core binding. D-77 transactional PATCH
semantics; `digest-mismatch` vs `hash-mismatch`. D-78 16 MiB segment
digests. D-79 transfer failure codes; unsigned responses; intra-domain
SN↔BS out of scope. D-80 TV-003 (interrupted-and-resumed).

**S2.7 — Forwarding & delegation.** D-81 *resolves D-23*: direct-to-
source topology; sources validate against own dispatch records. D-82
`delegation/1` document and `/fulfill` endpoint. D-83 unsigned fulfill
responses; `not-available`/`medialet-mismatch`; budget accounting. D-84
chain-integrity duty; custody before dispatch. D-85 TV-004.

**S2.8 — Resolution & retention.** D-86 normative semantics, informative
API. D-87 the reference state machine and tombstone record. D-88
retention/GC invariants; sender-side promises. D-89 quota defaults
("junk weighs kilobytes" preserved). D-90 mailbox-membership capability;
the internal dedup oracle closed.

**S2.9 — Content profile.** D-91 the element/attribute allowlist;
drop-vs-unwrap. D-92 URL and Manifest-reference rules. D-93 the
no-functional-notation CSS grammar; honest content-hiding residual. D-94
*resolves D-28/D-31*: sanitization derives the render form, never touches
the signed artifact; tree-equality conformance. D-95 derived text
rendering. D-96 TV-005 corpus (mechanically generated, idempotence-
verified).

**S2.10 — Security & privacy.** D-97 *refines the Stage 1 audit claim*:
the five-class outbound-connection inventory. D-98 acceptance-timing
disclosure documented; transfers are visible acts, reading is not. D-99
sections 12–13 adopted.

**S2.11 — Registries & annexes.** D-100 registry administration (ten
registries; MEP Required; IANA status disclosed). D-101 versioning
policy; deferred register carried. D-102 Annex A guest delivery. D-103
Annex B topologies (raw storage always fronted). D-104 Annex C
conformance structure (every-MUST-a-failing-test bar).

**Pre-freeze question (multi-BS).** D-105 multi-instance BS affirmed and
made explicit. D-106 per-store dedup scope. D-107 Annex B.6 partitioned
storage (recipient-controlled routing; honest limits).

## 3. Amendments and resolutions ledger

Stage 2 changed or sharpened frozen Stage 1 material six times, each
flagged in-session per the MEP discipline: D-49 amended D-47
(`in_reply_to` addressing); D-70 amended D-16 (per-recipient verdicts);
D-74 refined D-20 (retry vs. replay); D-81 resolved D-23's topology
ambiguity (direct-to-source); D-94 resolved the D-28/D-31 tension (the
render form); D-97 replaced the Stage 1 single-sentence audit claim with
the enumerable outbound inventory. D-98 added a disclosure Stage 1
lacked (acceptance timing). Working practice codified along the way: the
machine-generated JSON vectors are copy-paste-authoritative; two
transcription errors in section prose were caught against them and
corrected.

## 4. Conformance vector inventory

| Vector | Content | Keys | Generator |
|---|---|---|---|
| TV-001 | Dispatch: Signed Medialet + Envelope + Domain Document fixture (final form) | RFC 8032 TEST 1 (author), TEST 2 (sn/bs) | committed |
| TV-002 | Negotiation transcript: defer verdict + grant upgrade with Reservation | TEST 3 (target sn/bs) | *to commit (§5.2)* |
| TV-003 | Interrupted-and-resumed push; RFC 9421 both shapes | TEST 2 (pusher bs) | *to commit (§5.2)* |
| TV-004 | Forward + delegation; TV-001 reused byte-identically | TEST 1024 (final sn/bs) | *to commit (§5.2)* |
| TV-005 | 14-case sanitization corpus, idempotence-verified | — | committed |

## 5. Next actions

1. **D-108 (pending)**: freeze `MLP-Core-Specification-0.1-draft-01` as
   the Stage 2 baseline; thereafter all changes via MEP.
2. Extract and commit standalone generator scripts for TV-002–TV-004
   (their code ran as session one-offs; the JSON is committed, the
   generators should be too — same standard as TV-001/TV-005).
3. Commit the assembled specification, this document, and the vector
   family to the public repository (D-40).
4. Update the NLnet application: deliverable D1 (the specification) now
   exists in complete draft — materially strengthens the submission
   (D-42).
5. Begin Stage 3 with the handoff below.

## 6. Stage 3 handoff — what the client design inherits

The specification deliberately leaves the client-facing surfaces to
Stage 3, with normative semantics they must satisfy: the compose flow
(including the D-105/D-107 store selector and the drag-in/background-
upload pattern from D-35); the accept affordance (surfacing
`available-until` deadlines, the delegation path, and — transparently —
the D-98 acceptance-timing disclosure: "download from sender"); tombstone
rendering per §10.4/§11; the render-form pipeline and client floor of
§11.5–11.7; status views speaking protocol facts only (D-37); guest
delivery per Annex A, mandatory in the flagship client (D-36); and the
§10.8 resolution sketch as the reference implementation's starting shape.
Every Stage 3 requirement traces to the beachhead persona per D-38, so
scope creep stays visible.

---

*On D-108 confirmation, Stage 2 is closed and this document joins the
Stage 1 Closing Document as the project's second frozen record.*
