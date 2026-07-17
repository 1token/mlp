# Product Requirements — the MLP reference implementation & flagship

**Scope note.** The *protocol's* requirements document is the
specification (`spec/MLP-Core-Specification-0.1-draft-03.md`) under
the D-104 audit; this PRD covers the *product* built on it — the
reference server (`mlpd`), the web client, and the medialet.org
flagship posture. Every requirement carries its decision anchor and,
where implemented, its verification. Status: **SHIPPED** (in the
repo, tested), **PARTIAL**, or **PLANNED** (with its funding line in
docs/NLNET-APPLICATION.md).

## 1. Product definition

One binary per domain giving a person or organization an email-like
address for heavy media: authored, signed, negotiated deliveries of
arbitrarily large payloads, with a no-account guest path and a
first-class claim funnel. Not a startup (D-42): the protocol is
free; the flagship sells convenience (storage headroom, bandwidth,
custom domains) and holds no protocol privileges.

## 2. Functional requirements

### FR-1 Identity & access
| # | Requirement | Anchors | Status |
|---|---|---|---|
| FR-1.1 | Passkey-first authentication (WebAuthn registration + assertion), dependency-free implementation, ES256 + Ed25519 | D-161, D-242 | SHIPPED — `webauthn/`, `TestWebAuthnCeremonies` |
| FR-1.2 | Password fallback per mailbox; sessions listable and revocable | D-161 | SHIPPED — `/auth/password`, `/sessions` |
| FR-1.3 | Guest claim mints a mailbox from link+PIN possession and issues a session | D-154, D-240 | SHIPPED — `TestGuestJourneyEndToEnd` |
| FR-1.4 | Recovery email/codes seeded at claim | D-161 | PARTIAL — schema provisioned (0001), flow unwired |

### FR-2 Composing & sending
| # | Requirement | Anchors | Status |
|---|---|---|---|
| FR-2.1 | Drafts autosave; send arms a 10 s undo hold | D-138 | SHIPPED |
| FR-2.2 | Attach by reference with have-check; attach by file with in-browser hashing and resumable upload | D-135, D-244 | SHIPPED — `run-urn.js`, composer door |
| FR-2.3 | Manifest previews paired to masters (`preview_of`) | MEP-002, D-236 | SHIPPED — TV-007 |
| FR-2.4 | Guests named per draft; per-draft PIN; sender-side second-channel disclosure | D-237, D-238 | SHIPPED |
| FR-2.5 | Job tags on deliveries; timeline of protocol facts | D-148, D-149 | SHIPPED |
| FR-2.6 | Idempotent mutating POSTs (client-minted keys) | D-169 | SHIPPED |

### FR-3 Receiving & reading
| # | Requirement | Anchors | Status |
|---|---|---|---|
| FR-3.1 | Tiered acceptance: correspondents full, strangers quarantine-capable, blocked nothing | §7.7, D-162 | SHIPPED |
| FR-3.2 | Tier-2 auto-grant of small media within budget (policy knob, spec default off) | D-139, D-245 | SHIPPED — the knob's story is in the S4.13 commit |
| FR-3.3 | Dual-duty sanitization (server derives, client re-sanitizes in isolated DOM) | D-31, §11 | SHIPPED — TV-005 both sides |
| FR-3.4 | Junk release/block with correspondent overrides outranking classifiers | D-165, D-21 | SHIPPED |
| FR-3.5 | Threads: reply threading, read/done/flag, inbox/junk/deliveries/media lenses | D-110 | SHIPPED |
| FR-3.6 | Topic bundles and the sweep gesture | S3.11 | PLANNED — parked (D-233), audit OPEN-CLIENT |

### FR-4 Media custody
| # | Requirement | Anchors | Status |
|---|---|---|---|
| FR-4.1 | Accept→transfer with resume from receiver checkpoint; byte verification before availability | §8, D-76 | SHIPPED — TV-003, `TestTwoDomainDemo` |
| FR-4.2 | Instant availability on local possession (claims, resends, dedup) | D-241 | SHIPPED |
| FR-4.3 | Pin retains absolutely; owner delete always allowed; honest tombstones | §10.4–10.5, D-88 | SHIPPED — `TestGCInvariants` |
| FR-4.4 | Ephemeral-class GC of auto-granted media | D-139, D-251 | SHIPPED |
| FR-4.5 | Multi-store routing at accept time | D-141, D-160 | PARTIAL — schema + store rows exist; selector UI parked (S3.7) |
| FR-4.6 | Custody forwarding with declarant-bound windows | §9, MEP-001 | SHIPPED — TV-004, TV-006 |

### FR-5 Guest surface
| # | Requirement | Anchors | Status |
|---|---|---|---|
| FR-5.1 | Capability links, hash-stored; 30-day expiry; 5-failure PIN lock checked before evaluation | D-152, D-155, D-238 | SHIPPED |
| FR-5.2 | Views never recorded; downloads recorded | D-147, D-239 | SHIPPED |
| FR-5.3 | Zero-tracking notification carrying the link only | D-153 | SHIPPED (notifier hook; SMTP integration operator-supplied) |
| FR-5.4 | Link survives claim; one claim per link | D-154, D-155 | SHIPPED |

### FR-6 Operations
| # | Requirement | Anchors | Status |
|---|---|---|---|
| FR-6.1 | Single-binary node; first-run key + mailbox provisioning; static client serving | D-247 | SHIPPED — `cmd/mlpd`, OPERATOR.md |
| FR-6.2 | Demo mode strictly flag-gated and loudly logged | D-247 | SHIPPED |
| FR-6.3 | Additive key rotation with cache-honoring windows | §5.5 | SHIPPED (procedure documented; mechanism tested) |
| FR-6.4 | Quota accounting and pressure-driven standard-class reclamation | §10.5 | PLANNED — `/quota` endpoint stubs the surface |

## 3. Non-functional requirements

| # | Requirement | Anchors | Verification |
|---|---|---|---|
| NFR-1 | **Interoperability**: every signed artifact byte-reproducible from committed generators; two-implementation parity on the sanitizer | D-104, D-197 | CI regenerates 7 vectors + runs both TV-005 suites every push |
| NFR-2 | **Security — transport**: HTTPS-only federation; hardened discovery (64 KiB cap, redirect budget, private-range refusal at dial); RFC 9421 signatures on transfer | §5.4, §6.6, D-72 | discovery + bs failing-input suites |
| NFR-3 | **Security — content**: sanitization idempotent and proven on the shared corpus; strict CSP (`script-src 'self'`, no wasm concession); isolated render DOM | §11, D-244 | TV-005 ×2, run-html.js, client floor |
| NFR-4 | **Privacy**: no read receipts anywhere in the data model; possession masked from strangers; guest views unrecorded; no tracking pixels in notifications | D-147, §7.5, D-153 | `TestGuestJourneyEndToEnd` asserts the *absence* of events |
| NFR-5 | **Integrity**: content addressing end-to-end (BLAKE3 multihash); author signatures survive forwarding and re-dispatch | D-25, §3.3, §9 | TV-001/004/006 byte-identity |
| NFR-6 | **Resilience**: transfers resume from durable checkpoints with zero redundant bytes; crash-equivalent kill tested | §8.7, D-248 | `TestTwoDomainDemo` |
| NFR-7 | **Simplicity of deployment**: one process, one data directory, SQLite, no external services | D-41 | OPERATOR.md; the demo boots two nodes in one script |
| NFR-8 | **Client buildability**: no frameworks, no build step, no bundler; vendored dependencies are files | D-116, D-244 | `tsc --noEmit` + files-as-served |
| NFR-9 | **Evolvability**: unknown-member tolerance tested as a MUST; MEP change control exercised through a full cycle | D-43, D-40 | `TestUnknownMemberTolerance`; draft-03 exists — two full MEP cycles (001/002, 003/004) |
| NFR-10 | **Auditability**: every MUST mapped to a test or a named gap; the audit drift-gated in CI | D-104, D-249 | `MUST-AUDIT.md` |

## 4. Explicit non-goals (v1)

End-to-end payload encryption (§12 documents the domain-visible
trust model; E2E is the follow-up grant's headline), synchronous or
streaming transfer, internationalized local parts (D-07), any
central directory or search, protocol-level monetization (D-42).
