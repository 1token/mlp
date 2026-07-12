# NLnet NGI Zero Commons Fund — application package (D-42)

Working draft for submission. Every claim below maps to a committed,
verifiable artifact in this repository; nothing is aspirational.
Posture per D-42: a protocol project, not a startup — deliverable-
structured, tightly scoped, with E2E encryption and v2 features
explicitly reserved for a follow-up application.

## Abstract (proposal text)

**The Medialet Protocol (MLP)** is an open, federated protocol for
asynchronous point-to-point delivery of heavy media — video, audio,
image sets, archives, datasets. It works like email (addresses,
inboxes, forwarding, no central authority) but is purpose-built for
payloads email structurally cannot carry: a lightweight signaling
layer negotiates scoped, expiring, size-capped reservations, and no
payload byte moves until the receiving side has granted one.
Deliveries are author-signed and content-addressed end to end;
transfers are resumable with zero redundant bytes; recipients without
any MLP account receive capability-link guest deliveries they can
claim into a real mailbox in one step. The reference implementation
(Go server, dependency-light web client), the specification, and a
regenerable conformance suite are all published under free licenses
(Apache-2.0 code, CC-BY 4.0 specification).

## The deliverables, now demonstrable

**D1 — The specification.** `spec/MLP-Core-Specification-0.1-draft-02.md`:
17 sections, ~2,600 lines, 69 normative-requirement lines under a
change-control process (MEP) that has already exercised a full cycle —
two extension proposals filed, decided, and rolled into draft-02 with
conformance vectors. The requirement language is audited:
`conformance/MUST-AUDIT.md` maps every MUST to a covering test or a
named, decision-tied gap (50 of 64 testable requirements covered by
failing-input or vector tests at submission; CI regenerates the audit
and fails on drift, so a spec edit forces an audit decision).

**D2 — The reference server.** `server/`: Go, stdlib crypto only.
Federation (discovery with a hardened fetch profile, dispatch,
signed verdicts, delegation with budgets, custody forwarding),
resumable transfer (tus-profile with RFC 9421 request signatures,
checkpointed BLAKE3, transactional verification), the reference
SQLite schema with the §10.3 state machine enforced by trigger, and
`cmd/mlpd` — one binary per domain, with an operator guide
(`docs/OPERATOR.md`).

**D3 — The conformance suite.** `conformance/`: seven frozen test
vectors (TV-001–TV-007) with committed generators that CI reproduces
byte-identically; a 14-case sanitizer corpus passed by two
independent implementations (Go and JS) under tree equality and
idempotence; malformed-input matrices (envelope validation, address
grammar, JSON dialect, WebAuthn CBOR); the resumption torture path
(kill mid-flight, resume from the receiver's checkpoint, byte-verify)
exercised in CI; and the MUST audit above as the suite's own
completeness instrument.

**D4 — The web client.** `client/`: no frameworks, no build step —
plain Web Components and ES6 modules served as written. The §11
sanitization pipeline runs server-side AND client-side (the dual
duty), gated on the shared TV-005 corpus; in-browser content
addressing (vendored pure-JS BLAKE3 — no wasm, so the CSP stays at
`script-src 'self'`) drives a hash-first upload door; passkey-first
identity (a dependency-free WebAuthn implementation, registration
and assertion, synthetic-authenticator tested).

**D5 — The two-domain deployment.** `demo/run.sh` boots two complete
domains; `server/cmd/mlpd/main_test.go` (`TestTwoDomainDemo`)
executes the minimum credible demo programmatically over real TCP
sockets, every run of CI: Tier-2 deferral with auto-granted
previews, the interrupted-and-resumed large transfer with zero
redundant bytes, possession answering a resend, reply threading, and
the guest-delivery → claim → instant-availability funnel.

## Requested amount and budget shape

€38,000, structured against the remaining path to 1.0:

- Hardening the deferred machinery the MUST audit names (segments,
  the DNS hint path, client presentation-layer gates) — every open
  audit entry closed or explicitly re-scoped. (€14k)
- The flagship node at medialet.org (free addresses, paid headroom)
  as a *separately branded* service with the published
  conflict-of-interest rule: the flagship receives no protocol
  privileges. (€10k)
- Interoperability: a second-implementation-friendly test harness
  (the vectors already regenerate from committed generators;
  packaging them for third parties), protocol-venue announcement,
  photographer-community pilot with the Petra workflow. (€8k)
- Documentation to operator and implementer grade beyond the current
  guides. (€6k)

NLnet's supplementary security audit slots directly into the exit
criteria (D-40): the §5.4 fetch profile, the §6.6 transfer
signatures, the sanitizer pair, and the WebAuthn implementation are
the requested focus areas.

## Sustainability and governance

Managed hosting on the flagship (storage, guest-delivery bandwidth,
custom domains) funds maintenance — the protocol is free;
convenience is the product. Excluded on principle: VC funding,
token/crypto financing, paid API tiers on the protocol itself
(D-42). Specification under CC-BY 4.0 with a sole editor through
1.0 and a public MEP process (D-40); code Apache-2.0.

## Follow-up scope (explicitly out of this application)

End-to-end payload encryption (the v1 trust model is
domain-operator-visible by design, documented in §12),
internationalized local parts (D-07), topic bundles, and the v2
items parked in the decision register.
