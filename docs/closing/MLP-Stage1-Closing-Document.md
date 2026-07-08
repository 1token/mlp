# Medialet Protocol (MLP) — Stage 1 Closing Document

| | |
|---|---|
| **Project** | Medialet — the asynchronous, federated protocol for heavy media exchange |
| **Document** | Stage 1 Closing Document (canonical decision record) |
| **Status** | Stage 1 **complete and frozen** — all decisions confirmed by the spec editor |
| **Wire version** | MLP/0.x (pre-1.0, unstable by declaration) |
| **Date** | 2026-07-04 |
| **Editor** | Igor (sole spec editor through 1.0, per D-40) |
| **Home** | medialet.org |
| **Licenses** | Code: Apache 2.0 · Specification: CC-BY 4.0 · Conformance suite: Apache 2.0 (per D-39) |

---

## 1. Purpose and status of this document

This document is the single authoritative record of every decision frozen during Stage 1 (concept consultation) of the Medialet project. It supersedes the pre-Stage-1 ideation documents (`README.md`, `SUMMARY.md`, `MAIN_ENTITIES.md`, `NICHE.md`), which are hereby retired as sources of truth; their surviving content is captured here, and their errors are retired explicitly in §5.

Stage 1 proceeded as eight structured consulting sessions. Each session produced a set of judgment calls that were explicitly confirmed by the editor before the next session began. Nothing in this document was frozen without sign-off. In-session, decisions were labeled with letters; because two sessions independently used the labels (a)–(f), this document renumbers all decisions as **D-01 through D-42**. Each session heading notes its original in-session labels for traceability to the Stage 1 transcript.

Amendments to any frozen decision after this point follow the MEP process (D-40): a numbered proposal, written rationale, and an editor verdict. Silent drift is not permitted.

## 2. Project definition

**Medialet** is an open, federated protocol for asynchronous point-to-point delivery of heavy media (video, audio, images, archives, datasets). It works like email — addresses, inboxes, forwarding, no central authority — but is purpose-built for payloads that email, federated chat, and synchronous P2P tools structurally cannot carry. Its core architectural move is the strict decoupling of a lightweight **signaling layer** (routing, negotiation, policy) from a heavy **storage layer** (blob custody and transfer), so that no byte of payload ever moves until the receiving side has explicitly granted a scoped, expiring, size-capped reservation.

Medialet is a **protocol project, not a startup**. Anyone may implement it from the published specification. The project's own implementations are reference material and a flagship service, not a moat.

### 2.1 The four project stages

1. **Stage 1 — Concept consultation** (this document): concept, architecture, use cases, target market, governance, funding. **Complete.**
2. **Stage 2 — Detailed specification**: step-by-step drafting of the normative spec (structure in §7).
3. **Stage 3 — Client design**: step-by-step design of the web-based client, requirements traced to the beachhead persona (D-35, D-38).
4. **Stage 4 — Implementation**: reference implementation — Go (single binary, SN+BS roles), SQLite for prototyping, plain JavaScript Web Components, ES6 modules.

Stages are sequential but with a declared feedback loop: implementation findings flow back into the spec as errata before 1.0 (D-40, D-41).

## 3. Frozen glossary

| Term | Definition |
|---|---|
| **Media** | A raw, opaque binary object, identified exclusively by content address. Stored in and transferred between Blob Stores. Carries no metadata of its own. Singular and plural: "one Media object", "three Media objects" (never "Medias"/"Medium"). |
| **Medialet** | The immutable, author-signed logical message: authored headers (`Author`, `Subject`, `Created`, `Displayed-To`, `Displayed-Cc`, `Medialet-ID`) + Body + Manifest + Author Signature. Byte-immutable after signing. What users create, read, and forward. |
| **Body** | The Medialet's document content, in the strict declarative HTML profile (D-31). |
| **Manifest** | The Medialet's explicit list of every referenced Media URN with declared size, media type, `available-until`, and optional segment digests. A Body URN absent from the Manifest is invalid; an unused Manifest entry is permitted. |
| **Envelope** | The ephemeral per-dispatch transport wrapper: `Envelope-To`, origin, negotiation state, encapsulated Medialet, Hop Signature. Exists only on the wire and in server queues; never seen by clients; no Bcc field exists (Bcc = omission). |
| **Author Signature** | Signature over the full Medialet by an `author`-role key (domain-held in v1, per D-13). |
| **Hop Signature / signature chain** | Per-dispatch SN signature over the Envelope; chained across forwards; doubles as the delegation capability (D-23). |
| **Displayed recipients** | `Displayed-To` / `Displayed-Cc` inside the Medialet: authored, signed, social information. |
| **Actual recipients** | `Envelope-To` in the Envelope: operational routing, never shown to recipients. |
| **Delivery Record** | Receiver-local metadata created by the Target SN (arrival, forwarder, verification verdict + `kid` + timestamp). Never transmitted. |
| **Signaling Node (SN)** | The role that routes Envelopes, runs negotiation, enforces acceptance policy, and manages mailboxes. |
| **Blob Store (BS)** | The role that stores Media and performs push transfers. A pure reservation-enforcer with no policy brain (D-18). |
| **Address** | `local-part@domain` (grammar in D-07). |
| **Discovery** | Resolution of a domain to its SN endpoint and key set (D-08–D-11). |
| **Domain Document** | The authoritative JSON at `https://<domain>/.well-known/medialet.json`. |
| **Reservation** | The receiving side's signed grant: URN + max size + `target_url` + expiry + single-use token, bound to a pusher identity (D-18, D-22). |
| **Fulfillment source** | The verified party that will push the bytes for a given URN; may differ from the enveloping domain (D-22). |
| **Verdicts** | Message: `accepted` / `rejected` / `quarantined`. Per-URN: `grant` / `have` / `defer` / `deny` (D-16). |
| **Pin / tombstone** | Recipient's "keep permanently" flag on a Media object; the honest render marker left when an unpinned object is garbage-collected (D-21). |
| **Guest delivery** | Non-federated product pattern for recipients without an Address: capability-URL web view + email notification (D-36). Informative annex only. |
| **MEP** | Medialet Enhancement Proposal — the change process for frozen decisions and the spec (D-40). |
| **`urn:mlet:`** | The Media content-address URN namespace (construction in D-25). |
| **MLP** | The Medialet Protocol; wire-versioned `MLP/0.x`. |

Naming conventions: "a Medialet" (entity, capitalized), "the Medialet Protocol" (protocol), "Medialet" bare (the project).

## 4. Decision register

Format: **D-nn — Title.** The decision. *Rationale in brief.*

### 4.1 Session 1 — Terminology and entity model *(in-session calls a–c)*

**D-01 — Three-layer entity model; roles, not boxes.** Exactly three payload entities in strict containment — Media inside (referenced by) Medialet inside Envelope — plus two node roles, SN and BS. SN and BS are roles that may share one binary or be deployed separately; the spec defines the interface, never the topology. *Everything else in the protocol hangs off this hierarchy.*

**D-02 — Medialet composition and immutability; the separate Manifest.** A Medialet = authored headers + Body + Manifest + Author Signature, byte-immutable after signing. The Manifest exists so the Target SN can make policy and quota decisions without parsing untrusted HTML. *Immutability is what makes chained forwarding and signature verification work.*

**D-03 — Envelope composition; Bcc by omission.** The Envelope carries `Envelope-To`, negotiation state, the unmodified Medialet, and the Hop Signature. It is ephemeral and client-invisible. There is no Bcc field anywhere: the Origin SN mints one Envelope per Bcc recipient naming only that recipient. *Structural Bcc privacy — no field to leak.*

**D-04 — Displayed vs. actual recipients; Delivery Record.** `Displayed-To`/`Displayed-Cc` (authored, signed, shown) and `Envelope-To` (routing, hidden) may legitimately diverge; sanctioned divergences are Bcc, forwarding, and list-style redistribution. Receiver-side bookkeeping (who forwarded, when arrived, verification verdict) lives in the Delivery Record, a third information category owned by the receiving SN and never transmitted. *Email's proven header/envelope split, made explicit and self-documenting.* Documented consequence: a recipient can verify authorship and hop integrity, never "intendedness" — inherent to Bcc existing.

**D-05 — `urn:mlet:` namespace.** Media URNs use the `urn:mlet:` namespace (full construction in D-25). *Short, unambiguous, greppable; these strings appear thousands of times in Bodies.*

**D-06 — Protocol naming and versioning.** "Medialet Protocol", abbreviated MLP, wire-versioned `MLP/0.x` HTTP-style. User identifiers are Addresses; resolution is Discovery. *One name for project, protocol, and entity is fine (cf. "email") given the usage conventions in §3.*

### 4.2 Session 2 — Addressing and discovery *(in-session calls a–f)*

**D-07 — Address grammar.** `local-part@domain`. Comparison case-insensitive after normalization; store/display preferred casing, route lowercased. Local part v1: conservative ASCII (letters, digits, dot, hyphen, underscore; 1–64 chars; no leading/trailing/double dots); internationalized local parts deferred to a versioned extension. Domains fully IDN from day one (punycode on the wire, Unicode on display). Subaddressing required: `alice+tag@domain` routes as `alice@domain`, tag delivered to the mailbox. *ASCII-locals/IDN-domains asymmetry is what demonstrably works in email; subaddressing costs one sentence and serves the beachhead workflow.*

**D-08 — The Domain Document is authoritative.** Discovery starts at `https://<domain>/.well-known/medialet.json`, authoritative because served under the domain's own TLS. Declares supported versions, the SN endpoint, and the key set. May be a static file; HTTPS-only redirects permitted, max 3 hops. *Hosted-provider (MX-style) outsourcing with trust rooted at the address's own origin.*

**D-09 — DNS TXT is corroborative; disagreement is fatal.** `_medialet.<domain>` TXT is a discovery hint/bootstrap (independently trustworthy only under DNSSEC). Verifiers MUST obtain keys via HTTPS; MAY cross-check DNS; if both exist and disagree, that is a hard verification failure, never a tiebreak. *Disagreement means misconfiguration or attack; defense-in-depth says stop.*

**D-10 — One public endpoint; optional per-user resolution.** Only the SN endpoint is ever advertised; dispatch targets the domain's SN, which routes internally (recipient existence answered at negotiation, like SMTP RCPT). BS push URLs exist solely as per-reservation negotiated values. An optional WebFinger-style courtesy endpoint (`GET {sn}/resolve?resource=acct:user@domain`) may serve compose-time UX; operators MAY disable it or restrict it to known peers (anti-enumeration). *Storage topology is never publicly discoverable.*

**D-11 — Hardened fetch profile (SSRF posture, corrected claim).** The media path is pure push — no server ever pulls payload from an untrusted origin. The control plane's only server-initiated outbound requests are discovery fetches under a hardened profile: GET-only, `/.well-known/` paths, port 443, response capped (~64 KB), redirect-limited, private/link-local IP ranges blocked. *Replaces the original README's overbroad "never executes outbound requests" claim with one that is true and auditable.*

**D-12 — Key model.** Three key roles published in the Domain Document key set: `sn` (Envelopes, negotiation replies), `bs` (media pushes), `author` (Medialets). Entries carry `kid` (the key's own multibase fingerprint — self-verifying), `alg` (Ed25519 mandatory-to-implement; field present for agility), the public key, roles, `notBefore`/`notAfter`. Every signature names its `kid`; rotation = overlapping validity windows. Keys MAY additionally be published DKIM-style as TXT selectors, same precedence rule as D-09. *Rotation designed in from day one; retrofitting it is miserable.*

**D-13 — Domain-attested authorship in v1.** Author keys are held and applied by the SN on behalf of local users, embedding the author's Address in signed content (DKIM's trust model: "this domain vouches this user authored this"). Cost, stated plainly: a malicious/compromised domain can forge its own users. Personal client-held keys are a designed-for v2 profile: the signature structure already carries `kid` and role, so the v2 profile changes where the key lives, not what is signed. *Zero client-side key management is what makes "as easy as email" true in v1.*

**D-14 — Transport baseline.** All protocol traffic is HTTPS with valid WebPKI certificates; no plaintext fallback, no self-signed exceptions. Standard HTTP caching for discovery documents (TTL bound in D-33); verifiers MUST re-fetch on unknown `kid`. Clients MUST render punycode or apply confusable-detection for addresses from unknown correspondents (IDN homograph defense). *XMPP's optional-TLS era is the cautionary tale.*

### 4.3 Session 3 — Acceptance policy and anti-abuse *(in-session calls a–f)*

**D-15 — The economic principle.** Normative design principle: **signaling is cheap and optimistic; storage is expensive and pessimistic.** Envelopes are accepted and evaluated liberally; no Media byte moves without an explicit, scoped, expiring, size-capped grant. *Medialet inverts email's spam economics; this principle is the answer.*

**D-16 — The verdict vocabulary.** Negotiation replies carry exactly one message verdict — `accepted` / `rejected` (machine-readable reason codes) / `quarantined` — plus one per-URN verdict per Manifest entry: `grant` (reservation attached), `have` (content already held; no transfer), `defer` (delivery yes, transfer awaits recipient decision; upgradeable to `grant`), `deny` (never accepted; Medialet may still be accepted with the reference marked unavailable). Verdicts mix freely within one reply. *The entire operator policy surface expresses itself in these seven words.*

**D-17 — No asynchronous bounces, ever.** All rejection is synchronous within the negotiation reply. The protocol never generates unsolicited notifications to unverified parties; post-acceptance status changes flow only to the already-verified Origin SN. *Email's backscatter problem is made structurally impossible.*

**D-18 — The Reservation; BS as pure enforcer.** A `grant` carries: the specific URN, max byte size (from the Manifest declaration; exceeding it aborts mid-stream), `target_url`, expiry (default **72 h**), and a single-use opaque reservation token. Push authentication is dual: token ("this transfer was invited") + HTTP Signature ("it is really the Origin BS"). The BS makes no acceptance decisions; it enforces reservations the SN minted. Expired reservations are simply re-negotiated. *Keeps the storage layer's trusted computing base tiny.*

**D-19 — Trust tiers; deferred transfer is the stranger default.** Tier 1 (correspondents: prior outbound contact, allowlist, same domain): default `grant` within quota headroom. Tier 2 (verified strangers): Medialet `accepted`, Media `defer` — recipient reads the letter before accepting the crate. Tier 3 (unknown/suspect): verification failures `rejected` outright; verifiable-but-suspicious `quarantined` with Media `defer`/`deny`. All thresholds operator-configurable. Deferral's sender-side cost is explicit: the Manifest field **`available-until`** (client default **7 days**) declares the sender's retention promise; late acceptance fails gracefully to "request a resend". *Storage cost stays with the party who chose to send.*

**D-20 — Caps, quotas, rate limits, replay.** Envelope hard cap **256 KB** (including encapsulated Medialet); Manifest cap **256 entries**. Receiver-side enforcement: per-mailbox storage quota (insufficient headroom → `defer` with reason `quota`), per-origin-domain pending-reservation caps, per-origin negotiation rate limits (HTTP 429 before expensive work). Replay: unique Envelope-ID + timestamp under the Hop Signature, duplicate rejection within retention horizon, ±48 h timestamp skew window, single-use tokens. *Nothing novel — but stated, bounded, and cheap-stage.*

**D-21 — Retention: pin-or-GC with tombstones; moderation hooks.** Medialets are kilobytes and permanent (per mailbox policy). Media are quota-bound: recipients may **pin**; unpinned objects become GC-eligible, leaving a **tombstone** (name, size, type, content address, "unavailable — expired \<date\>") that clients MUST render honestly. Recipient deletion always permitted; blobs are reference-counted across Medialets. Operator moderation is defined as interfaces, not policies: hash-blocklist hook at negotiation (including industry child-safety hash lists — natural here, since every object is content-addressed before transfer), type/size policy hook, quarantine classifier hook over Medialet text/HTML. *Decision points exist at the cheap stage, before bytes move; policies remain operator jurisdiction.*

### 4.4 Session 4 — Forwarding and delegated fulfillment *(in-session calls g–i)*

**D-22 — Fulfillment source is first-class.** Negotiation identifies, per URN, which verified party will push the bytes; Reservations bind to (URN, pusher identity, expiry, token) rather than assuming the enveloping domain pushes. *"Who sent the Envelope" and "who holds the bytes" legitimately differ under forwarding.*

**D-23 — The hop chain is a delegation capability.** Under delegated fulfillment (pointer forwarding), a downstream grant travels back along the hop chain and the origin BS pushes directly to the final recipient's BS; the origin verifies that the request descends from its own original dispatch signature via the chain. Origin-side controls: delegation honored only within `available-until` and a per-object fulfillment budget (default ~10; exhaustion → `deny: delegation-budget`, fallback to any custody-holding chain member, else graceful tombstone). Documented privacy trade: delegation reveals downstream forwarding events to the origin. *A structure added for provenance turns out to be the authorization evidence — intermediates never take custody they didn't ask for.*

**D-24 — Forwarding mode defaults.** Delegated fulfillment is the default for aliases, auto-forwards, and personal forwards (the relay handles kilobytes only). Custody forwarding (forwarder takes the bytes, becomes fulfillment source) is the default for mailing-list-style redistribution — the party multiplying the audience carries the cost — and is exposed in clients as "private forward" (escapes the D-23 privacy leak, origin retention, and budget). Any chain member with custody may fulfill; nearest hop preferred. *Incentive-correct cost allocation in both directions; content-address dedup makes list fan-out per-domain, not per-member.*

### 4.5 Session 5 — Transfer mechanics *(in-session calls j–o)*

**D-25 — URN construction.** `urn:mlet:<multibase(base32-lower, multihash(blake3-256, content))>`. BLAKE3-256 is mandatory-to-implement (tree hash: persistable/resumable incremental state, future verified-streaming path via Bao); SHA-256 available through the multihash layer for interop; multihash gives algorithm agility without a namespace bump; base32-lower is case-insensitivity-safe across HTML, URLs, filesystems. The Manifest carries size and type; the URN carries only content identity. *IPFS's encoding scar tissue adopted without importing their stack.*

**D-26 — tus 1.0 core as the push profile.** The push adopts tus 1.0 core rather than bespoke Content-Range semantics. The Reservation *is* the pre-created upload resource (`target_url`); resumption is native `HEAD → Upload-Offset → PATCH`. MLP additions ride as headers: reservation token on every request, plus a per-request HTTP Signature (`bs` key) over method, target, token, offset, that request's body-segment digest, and timestamp — each PATCH independently verifiable, no ambient session trust. The IETF resumable-uploads effort is tracked for a future migration profile. *A decade of production hardening for free; don't reinvent.*

**D-27 — Verification semantics.** The receiving BS hashes incrementally and persists hasher state alongside partial data (SHOULD survive restarts). Whole-object verification is at completion: match → stored, live, `have`-eligible; mismatch → partial discarded, `hash-mismatch` reported, reservation resets to offset 0 if unexpired. Manifest entries MAY carry fixed 16 MiB segment digests; receivers SHOULD verify segments as they complete and abort early (quality-of-implementation, not conformance). Mid-stream abort on exceeding the reserved size is a MUST. Unverified partials live in a reservation-keyed quarantine area, never visible or resolvable, GC'd on expiry. *Honest about when integrity is known; the 50 GB death-at-the-last-byte case resumes with zero redundant bytes.*

**D-28 — URNs are never rewritten.** Medialets are stored and forwarded verbatim, forever; any rewriting would break the Author Signature (D-02). Content addresses are location-independent: the recipient's client resolves any URN against its **own home BS** (`GET {bs}/o/{urn}`, authenticated), receiving the verified local copy, a tombstone, or a "deferred — accept transfer?" status. Bookkeeping lives in the Delivery Record, never in the signed artifact. *Formally retires the "rewrite to local paths" mechanism from the source documents.*

**D-29 — `have` masking.** The `have` verdict is a possession oracle (the classic dedup side channel). Default: `have` visible to Tier 1 correspondents, masked as `grant` for Tiers 2–3 (accept the push, discard duplicate bytes internally). Operators may alternatively scope dedup per-mailbox. *Costs bandwidth against strangers; leaks nothing.*

**D-30 — Parallelism.** One stream per object; concurrency across objects (Manifest cap bounds blast radius). Per-object parallel chunking deferred (tus concatenation / IETF migration path). *HTTP/2 plus multi-object concurrency saturates realistic links; single-stream keeps hasher-state trivial.*

### 4.6 Session 6 — Threat model *(in-session calls p–s)*

**D-31 — The HTML profile.** The Body is declarative content only — a document, not an application. Strict subset: semantic text markup, headings, lists, tables, links, embedded Media references. Prohibited: scripts, forms, event attributes, CSS beyond a safe inline allowlist, iframes/objects/embeds. **No external resource loading, period** — every embedded asset is a `urn:mlet:` reference resolved via the recipient's own BS; rendering triggers zero outbound requests (tracking pixels and silent read receipts are killed structurally; Medialets render fully offline; deliberate link clicks are consent). Sanitize twice: Target SN at ingest (normative) and client at render (normative); web clients ship strict CSP, native clients equivalent sandboxing; media previews decode sandboxed. Consequence embraced: read receipts can only ever exist as an explicit, recipient-consented protocol feature (Stage 2 backlog), never a surveillance trick. *The largest attack surface, closed at the spec level because interop requires agreement on renderability.*

**D-32 — Verification-at-ingest is normative.** The Target SN verifies signatures while keys are live and records verdict, `kid`, and timestamp in the Delivery Record. Post-rotation re-verification of stored mail is best-effort forensics, not conformance. *DKIM's known wound (evaporated keys ≠ fake old mail), answered honestly without demanding eternal key archives.*

**D-33 — Key-cache TTL ≤ 24 h.** Key sets are cacheable for at most 24 hours; verifiers MUST re-fetch on unknown `kid`. Revocation is honest-soft: removal from the Domain Document takes effect within the cache TTL, bounding revocation latency at 24 h worst-case — stated plainly. *Soft revocation disclosed beats hard revocation promised and unmet.*

**D-34 — The candor list ships in the spec as-is.** V1 explicitly does not provide: (1) end-to-end confidentiality — operators see everything; a v2 E2E profile slot is reserved (blobs are already opaque content-addressed objects; per-recipient HPKE changes what the URN names, not how bytes move); (2) protection against a domain forging *its own* users (D-13's inherited DKIM limit; v2 personal-key profile is the escape); (3) metadata privacy — operator visibility, the D-23 delegation disclosure, and traffic analysis are out of scope, as for email; (4) censorship resistance — availability is at operator mercy; federation's remedy is exit, not cryptography. *Reviewers forgive absent defenses; they don't forgive pretended ones.*

Supporting analysis frozen with this session (spec Security Considerations skeleton): the assets/adversaries taxonomy (A1 network attacker … A7 bulk metadata observer); the defended-claims table, each row anchored to a decision above; the single auditable sentence *"the protocol's only outbound server-initiated fetches are discovery fetches under the hardened profile"*; malicious-origin / malicious-receiver / malicious-relay walkthroughs (a relay can lose the parcel and read the postcard, never rewrite the letter); per-role key-compromise analysis (stolen `bs` key caps at bandwidth waste — garbage cannot impersonate content-addressed data); resource-exhaustion residue (negotiation floods → rate limits; reservation squatting → 72 h expiry + pending caps; slow-loris → minimum-throughput termination; oversize → mid-stream abort); and the who-learns-what privacy ledger, carried verbatim into the spec.

### 4.7 Session 7 — Use cases and beachhead *(in-session calls t–w)*

**D-35 — Beachhead persona.** User number one is the independent professional photographer/videographer delivering 5–200 GB shoots to paying clients ("Petra"). The adoption asymmetry is the strategy: the motivated sender pays the adoption cost; recipients get one-click reception. Second wave: small post-production teams (stable Tier 1 correspondent sets, dedup on versioned footage). Explicit v1 anti-personas: consumer family sharing (unwinnable network-effects fight), compliance verticals (medical imaging, research data — real fit, v2+), and mass distribution — one-to-thousands is publishing, not correspondence; no public objects, no global search, no swarm (values statement and liability shield; goes in the README). *The persona validates the locked architecture: delivery-page-as-Medialet, `available-until` as a surfaced feature, defer→grant as download-when-ready UX, dedup on resends, subaddressing as per-job filing.*

**D-36 — Guest delivery.** For recipients without an Address: the sender's own node serves the Medialet as a capability-URL web view (expiry, optional PIN) and notifies by plain email; a "claim your permanent address" step converts recipients at the moment of maximum goodwill. Normative status: **product pattern, not protocol** — informative annex in the spec, mandatory feature of the flagship Stage 3 client, never part of core federation (no bytes federate). *Every guest delivery is an adoption funnel; the spec stays a federation spec.*

**D-37 — Sender-visible status is protocol facts only.** Senders learn verdicts and transfer completion ("delivered", "files accepted", "transfer complete") — facts about servers, never about recipient behavior. No read status exists or will be inferred (structurally enforced by D-31); consent-based receipts remain a future feature. *Professional proof-of-delivery without the pixel war.*

**D-38 — Persona shapes the client, never the spec.** The protocol remains persona-neutral; every Stage 3 client requirement traces explicitly to the beachhead persona so scope creep is visible and declinable. *The test of a protocol layer is that different clients need zero spec changes — passed on paper across all four named use cases.*

### 4.8 Session 8 — Governance, roadmap, funding *(in-session calls x–aa)*

**D-39 — The IP bundle.** Code: Apache 2.0. Specification: CC-BY 4.0. Conformance suite: Apache 2.0 (embeddable in third-party CI without license anxiety). Trademark: register "Medialet" as an EU word mark (EUIPO, classes ~9/38/42) with a published Matrix/Mastodon-style policy — anyone may truthfully claim "Medialet-compatible" as verified by the conformance suite; nobody may imply endorsement or name an incompatible fork. The conformance suite is the objective test behind the trademark's teeth. *The only enforcement an open protocol has against embrace-and-extend; the .org domain alone protects nothing.*

**D-40 — Spec form and governance.** The spec is written in Internet-Draft form (RFC 2119/8174 language, numbered sections, self-administered IANA-style considerations) in a public repo from the first commit. Changes arrive as MEPs — numbered proposals, public discussion, editor verdict with written rationale. Igor is sole spec editor through 1.0 (declared BDFL, not performative committee). 1.0 exit criteria, fixed now: two independent interoperable implementations passing the conformance suite + an addressed external security audit + 6 months without breaking wire changes; until then, MLP/0.x with explicit instability warnings. Fiscal-host graduation (The Commons Conservancy or Software Freedom Conservancy holding trademark and domain in trust) is declared intent, executed when adoption warrants. *Precision-forcing form now; IETF Independent Submission is a later waypoint, not a day-one goal.*

**D-41 — Roadmap and the minimum credible demo.** Sequencing: Stage 2 spec → reference implementation (one Go binary, SN+BS, SQLite; web client in plain Web Components/ES6) → conformance suite as a first-class deliverable (protocol test vectors, negotiation transcripts, malformed-input corpora, resumption torture test — the ActivityPub-fragmentation lesson) → flagship node at medialet.org (free addresses, paid headroom) → announcement to protocol-literate venues first, photographer outreach after. Everything aims at the minimum credible demo: two real domains; compose on one, deliver to the other; a deferred 50 GB object accepted, killed mid-flight, resumed to completion; `have` dedup on resend. Brand separation declared now: the protocol lives at medialet.org, the flagship service visibly separate (app.medialet.org), fully separated when traction warrants. *The demo is the argument; rough consensus and running code.*

**D-42 — Funding posture.** Primary route: NLnet NGI Zero Commons Fund — deliverable-structured application (D1 spec, D2 reference SN/BS, D3 conformance suite, D4 web client, D5 two-domain flagship deployment), €30–50k ask, scoped tightly to leave E2E/v2 for a follow-up grant; NLnet's free supplementary security audit slots directly into the D-40 exit criteria. Long-term sustainability: managed hosting on the flagship (storage, guest-delivery bandwidth, custom domains — the protocol is free, convenience is the product). Published conflict-of-interest rule from day one: *the flagship node receives no protocol privileges — nothing that doesn't work identically for any other node.* Excluded on principle: VC funding, token/crypto financing, paid API tiers on the protocol itself. Correction recorded: Prototype Fund (German residency requirement) and Sovereign Tech Fund (maintenance of existing infrastructure, not greenfield) were struck from the earlier shortlist. *Not a startup — and structured so it never accidentally becomes one.*

## 5. Corrections registry — retired claims from the source documents

The pre-Stage-1 documents contain the following errors, formally retired; none may reappear in Stage 2+ artifacts.

| # | Retired claim | Source | Correction | Decision |
|---|---|---|---|---|
| C-1 | The *Envelope* is "the core domain entity containing metadata (To, Cc, Bcc, Subject) and an HTML payload" | `SUMMARY.md` | Subject and Body belong to the **Medialet**; routing belongs to the **Envelope**; Bcc exists as a stored field nowhere (Bcc = per-recipient Envelope omission) | D-01–D-04 |
| C-2 | The target "never executes outbound requests to untrusted third-party servers" | `README.md` | True claim: the **media path** is pure push; the control plane performs only hardened discovery fetches (GET-only, well-known, 443, size/redirect-capped, private ranges blocked) | D-11 |
| C-3 | On delivery, the Target SN "rewrites the URN pointers to local target paths" | `MAIN_ENTITIES.md`, `SUMMARY.md` | Medialets are byte-immutable and stored verbatim; rewriting would break the Author Signature; clients resolve location-independent URNs against their own home BS | D-02, D-28 |
| C-4 | Key discovery at `/.well-known/medialet-keys.json` via "DNS-based key discovery" | `README.md`, `SUMMARY.md` | Keys live in the **Domain Document** at `/.well-known/medialet.json`; HTTPS is authoritative, DNS TXT corroborative, disagreement = hard fail | D-08, D-09, D-12 |
| C-5 | Prototype Fund and Sovereign Tech Fund as funding candidates | Stage 1 session 1 (Claude) | Struck — residency mismatch and existing-infrastructure mandate respectively; NLnet NGI Zero is the primary route | D-42 |

## 6. Threat model summary (condensed)

Full skeleton frozen under D-31–D-34; this is the executive form.

**Defended (mechanism):** wire tampering/downgrade (mandatory HTTPS + per-request signatures, no plaintext mode); storage exhaustion by strangers (reservation economy — nothing moves without a scoped, expiring, capped grant); backscatter (structurally impossible — D-17); cross-domain author forgery (domain-attested signatures + authoritative HTTPS key discovery); media substitution (content addressing, verify-before-visible); replay (Envelope-IDs, skew windows, single-use tokens); SSRF (push-only media path + hardened fetch profile); dedup possession-probing (`have` masking); tracking pixels and silent read receipts (no external loads, structurally).

**Explicitly not defended in v1 (disclosed):** operator visibility of all content (no E2E; v2 profile slot reserved); a domain forging its own users (DKIM limit; v2 personal keys); metadata/traffic analysis, including the documented delegation disclosure to origin (custody forwarding is the escape); censorship by one's own operator (remedy is exit).

**Privacy ledger (who learns what):** origin SN — content, full recipient set incl. Bcc, timing; target SN — content, sender, timing, for its own users; each BS — blob contents, domain-level transfer graph; a relay — whatever transits it; origin under delegation — downstream forwarding events (disclosed trade, D-23); a discovery observer — that domain A looked up domain B around time T. No party learns more than its email-world analogue except the delegation case, which is new functionality priced in disclosed privacy.

## 7. Stage 2 — specification table of contents

The Stage 2 sessions draft the normative spec ("MLP/0.x") section by section, in Internet-Draft form per D-40. Each section below traces to the decisions that constrain it.

1. **Introduction & Motivation** — problem statement, design principles (D-15), non-goals (from D-34, D-35 anti-personas). *Informative.*
2. **Conventions & Terminology** — RFC 2119/8174; the §3 glossary, normativized.
3. **Entity Model** — Media, Medialet (headers, Body, Manifest, Author Signature, canonical serialization for signing), Envelope, Delivery Record. (D-01–D-06)
4. **Addressing** — grammar, normalization, subaddressing, IDN handling. (D-07, D-14)
5. **Discovery** — Domain Document schema, DNS hint, precedence, hardened fetch profile, caching. (D-08–D-11, D-33)
6. **Keys & Signatures** — key set schema, roles, `kid`, Ed25519 profile, signature formats (Author, Hop, per-request), rotation, verification-at-ingest. (D-12–D-14, D-26, D-32, D-33)
7. **Negotiation & Acceptance** — dispatch, verdict vocabulary, reason-code registry, Reservations, trust tiers as recommended defaults, caps, rate limits, replay. (D-15–D-20)
8. **Transfer** — the tus 1.0 profile, per-request signing, verification semantics, quarantine, resumption. (D-25–D-27, D-30)
9. **Forwarding & Delegation** — hop chains, fulfillment sources, delegated vs. custody modes, budgets. (D-22–D-24)
10. **Content Resolution & Retention** — home-BS resolution, pin/GC/tombstones, reference counting, `have` masking. (D-21, D-28, D-29)
11. **The HTML Profile** — tag/attribute allowlist, sanitization requirements, client rendering rules. (D-31)
12. **Security Considerations** — the full D-31–D-34 skeleton.
13. **Privacy Considerations** — the privacy ledger, delegation disclosure, anti-enumeration.
14. **Extensibility & Registries** — self-administered registries: verdicts, reason codes, key roles, multihash algorithms, header fields, MEP process pointer. (D-40)
15. **Annex A (informative): Guest Delivery** — recommended pattern: capability URLs, expiry, PIN, claim-conversion. (D-36)
16. **Annex B (informative): Deployment Topologies** — single-binary, split SN/BS, S3-backed BS, hosted-provider redirect.
17. **Annex C (informative): Conformance Overview** — test-suite structure and vectors. (D-39, D-41)

**Deferred register (v2 / backlog, frozen as intent):** E2E encryption profile (per-recipient HPKE over blobs); personal client-held author keys; consent-based read receipts; internationalized local parts; per-object parallel transfer (tus concatenation / IETF resumable-uploads migration); mailing-list profile; compliance verticals (DICOM, audit trails); Bao verified streaming.

## 8. Immediate next actions

1. Create the public repository; commit this document as the first artifact (D-40).
2. Begin Stage 2, session 1: spec skeleton (sections 1–2) + canonical Medialet serialization for signing — the first genuinely new technical work, since signing requires byte-exact canonical form.
3. Draft the NLnet NGI Zero application around deliverables D1–D5 (D-42); the threat model (§6) anchors it.
4. File the EUIPO word mark (D-39).
5. Stand up a minimal medialet.org holding page: one-paragraph pitch, link to the repo, "spec in progress" status — announcement itself waits for the minimum credible demo (D-41).

---

*End of Stage 1 Closing Document. Stage 1 is frozen; amendments proceed only via MEP.*
