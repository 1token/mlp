# The Medialet Protocol (MLP) — Core Specification

| | |
|---|---|
| **Version** | MLP/0.1 — document revision draft-03 (draft-02 + MEP-003, MEP-004) |
| **Status** | Pre-1.0, declared unstable (D-101); all changes via MEP (D-40) |
| **Editor** | Igor (sole editor through 1.0, per D-40) |
| **Date** | 2026-07-17 |
| **License** | CC-BY 4.0 (D-39) |
| **Conformance** | Test vectors TV-001–TV-008 with committed generators (Annex C) |
| **Decision register** | Stage 1 Closing Document (D-01–D-42); Stage 2 Closing Document (D-43 onward) |

> **Changelog.** draft-03 (2026-07-17): MEP-003 (bao verified
> streaming — §5.2 `capabilities` with the `bao-stream/1` token, §8.9
> verified-streaming push, Annex D encoding, `bao-verify-failed`,
> TV-008; integrated with the fetch-surface correction recorded in the
> MEP's editor decision: MLP has no cross-domain read — D-11 pure push
> — so slice consumption binds deployment read surfaces, informative
> per D-68/D-79) and MEP-004 (the mailing-list profile — §3.4.1 `list`
> member; the profile itself lives at `spec/profiles/`, normative for
> claimants only) accepted and applied.
> draft-02 (2026-07-12): MEP-001 (fulfillment-window
> override — §3.4.1 `until`, §10.3 effective offer deadline, §9.5
> declarant binding) and MEP-002 (`preview_of` Manifest member —
> §3.2.2) accepted and applied; conformance grows TV-006 and TV-007.
> draft-01 (2026-07-05): the Stage 2 freeze (D-108).

**Contents**

- 1. Introduction
- 2. Conventions and Terminology
- 3. Entity Model
- 4. Addressing
- 5. Discovery
- 6. Keys and Signatures
- 7. Negotiation and Acceptance
- 8. Transfer
- 9. Forwarding and Delegation
- 10. Content Resolution and Retention
- 11. The mlp-html/1 Content Profile
- 12. Security Considerations
- 13. Privacy Considerations
- 14. Extensibility and Registries
- Annex A (informative): Guest Delivery
- Annex B (informative): Deployment Topologies
- Annex C (informative): Conformance Overview
- Annex D (normative): The application/mlp-bao Encoding

## 1. Introduction

*This section is informative.*

### 1.1 Problem statement

Most open communication protocols were built to pass text. When asked to carry
heavy media — multi-gigabyte video masters, raw audio sessions, image sets,
datasets — they fail in characteristic ways: email (SMTP/JMAP) enforces global
size limits three orders of magnitude too small; federated chat and social
protocols (Matrix, ActivityPub) use pull-based media distribution and carry
state-synchronization overhead unrelated to point-to-point delivery;
browser-to-browser transfer (WebRTC and kin) is synchronous, requiring both
parties online simultaneously; and web-link services are centralized products,
not protocols. The result is a persistent gap: there is no open, federated,
asynchronous standard for delivering heavy media from one address to another.

The Medialet Protocol (MLP) fills that gap. It behaves like email — addresses,
inboxes, forwarding, no central authority — but is designed from first
principles for heavy payloads: the lightweight signaling plane (routing,
negotiation, policy) is strictly decoupled from the heavy storage plane (blob
custody and transfer), and no payload byte ever moves until the receiving side
has granted an explicit, scoped, expiring, size-capped reservation.

### 1.2 Design principles

The following principles, frozen during concept consultation, govern every
normative choice in this document:

1. **Signaling is cheap and optimistic; storage is expensive and pessimistic**
   (D-15). Envelopes are accepted and evaluated liberally; Media moves only
   under an explicit grant.
2. **The Medialet is an immutable, author-signed artifact** (D-02, D-28). Once
   signed it is stored and forwarded verbatim, forever; nothing on the delivery
   path rewrites it.
3. **All content is content-addressed** (D-05, D-25). Media objects — and the
   signed Medialet artifact itself — are identified by cryptographic digest,
   making integrity verification, deduplication, and location-independent
   resolution structural properties rather than features.
4. **The media path is pure push; rejection is always synchronous** (D-11,
   D-17). No server pulls payload from an untrusted origin, and the protocol
   never emits asynchronous notifications to unverified parties; backscatter
   cannot exist.
5. **The Body is a document, not an application** (D-31). Declarative content
   only; rendering a Medialet triggers zero outbound requests.
6. **Honesty over promises** (D-34). The protocol documents what it does not
   defend — operator visibility, domain self-forgery, metadata exposure —
   rather than implying otherwise.

### 1.3 Non-goals

MLP version 0.x/1.0 explicitly does not attempt: end-to-end confidentiality
(reserved as a v2 profile; D-34); censorship resistance (availability is at
operator mercy; the federated remedy is exit); mass one-to-many distribution
(no public objects, no global search, no swarm; D-35); consumer social sharing;
real-time or synchronous transfer semantics; and compliance-vertical features
(audit trails, DICOM integration), which are deferred (D-35).

### 1.4 Document roadmap

Sections 3–11 define the normative protocol: entities, addressing, discovery,
keys and signatures, negotiation, transfer, forwarding, retention, and the
content profile. Sections 12–13 collect security and privacy considerations.
Section 14 defines extensibility and registries. Annexes A–C are informative.

## 2. Conventions and Terminology

### 2.1 Requirement language

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD",
"SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and "OPTIONAL" in this
document are to be interpreted as described in BCP 14 (RFC 2119, RFC 8174)
when, and only when, they appear in all capitals.

### 2.2 Terminology

The following terms are normative throughout this specification.

**Media** — a raw, opaque binary object, identified exclusively by content
address. Stored in and transferred between Blob Stores. Carries no metadata of
its own. Used as both singular and plural ("one Media object", "three Media
objects").

**Medialet** — the immutable, author-signed logical message: authored headers,
Body, Manifest, and Author Signature. Byte-immutable after signing (D-02).

**Body** — the Medialet's document content, conforming to the `mlp-html/1`
profile (Section 11).

**Manifest** — the Medialet's explicit list of every referenced Media URN with
declared size, media type, availability window, and optional segment digests.
A Body URN absent from the Manifest is invalid; an unused Manifest entry is
permitted (D-02).

**Envelope** — the ephemeral per-dispatch transport wrapper: actual recipients,
negotiation state, the encapsulated Medialet, and the Hop Signature. Exists
only on the wire and in server queues; never delivered to clients. No Bcc
field exists; Bcc is realized by per-recipient Envelope omission (D-03).

**Author Signature** — the signature over the Medialet by an `author`-role key
(domain-held in v1; D-13).

**Hop Signature / signature chain** — the per-dispatch Signaling Node signature
over the Envelope; chained across forwards; doubles as the delegation
capability (D-23).

**Displayed recipients / Actual recipients** — the authored, signed
`displayed_to`/`displayed_cc` fields inside the Medialet versus the routing
`envelope_to` in the Envelope. The two MAY legitimately diverge (Bcc,
forwarding, redistribution; D-04).

**Delivery Record** — receiver-local metadata created by the Target SN
(arrival time, forwarder, verification verdict, key ID, verification
timestamp). Never transmitted (D-04, D-32).

**Signaling Node (SN)** — the role that routes Envelopes, conducts negotiation,
enforces acceptance policy, and manages mailboxes.

**Blob Store (BS)** — the role that stores Media and performs push transfers; a
pure reservation-enforcer with no policy authority (D-18). SN and BS are
roles, not deployment units (D-01).

**Address** — `local-part@domain` (Section 4).

**Discovery** — resolution of a domain to its SN endpoint and key set via the
Domain Document (Section 5).

**Domain Document** — the authoritative JSON document at
`https://<domain>/.well-known/medialet.json` (D-08).

**Reservation** — the receiving side's signed grant to push one Media object:
URN, maximum size, target URL, expiry, and a single-use token, bound to a
pusher identity (D-18, D-22).

**Fulfillment source** — the verified party that will push the bytes for a
given URN; may differ from the enveloping domain (D-22).

**Verdicts** — message-level: `accepted`, `rejected`, `quarantined`; per-URN:
`grant`, `have`, `defer`, `deny` (D-16).

**Pin / tombstone** — the recipient's retention flag on a Media object; the
rendering marker left when an unpinned object is garbage-collected (D-21).

**Medialet-ID** — the author-generated identifier of a Medialet; uniqueness and
deduplication scope is the pair (author, id).

**MEP** — Medialet Enhancement Proposal, the change process for this
specification (D-40).

**Hop Attestation** — the privacy-reduced record of a prior dispatch
carried in an Envelope's `hops` chain: origin, envelope identity, key ID,
and Hop Signature — never prior recipient sets (§3.4.2, D-51).

**Render form** — the receiver-derived, sanitized rendering artifact of a
Body (§11.5). Never authoritative; the verbatim Signed Medialet remains
the stored and forwarded object (D-94).

**Reference** — the per-mailbox retention record binding a recipient to a
Manifest entry, carrying the §10.3 state machine (D-87).

Naming conventions: "a Medialet" (the entity), "the Medialet Protocol" or
"MLP" (this protocol), "Medialet" bare (the project).

### 2.3 JSON conventions

All MLP protocol documents (Medialets, Envelopes, Domain Documents,
negotiation messages) are JSON, encoded in UTF-8.

1. **Numbers.** All numeric values MUST be integers. Floating-point values
   MUST NOT appear anywhere in MLP JSON. Every integer MUST satisfy
   |n| ≤ 2^53−1 (the IEEE-754 exact range). Byte sizes are expressed as
   integer byte counts.
2. **Timestamps.** All timestamps are RFC 3339 strings in UTC with the `Z`
   designator (e.g., `2026-07-04T12:00:00Z`). Epoch numbers MUST NOT be used.
3. **Field names** are lower snake_case ASCII.
4. **Unicode.** The protocol applies no Unicode normalization to content
   values; signed bytes are authoritative as transmitted. Creators SHOULD emit
   NFC. Address comparison applies its own normalization (Section 4).
5. **Unknown members** MUST be ignored on read (forward compatibility) but are
   covered by signatures where present (a signed document with unknown members
   still verifies as its exact signed form).

### 2.4 Signing conventions (overview)

MLP uses exactly two signing mechanisms, each specified fully in Section 6:

- **Document signatures** (Author Signature, Hop Signature, signed negotiation
  verdicts): Ed25519 over the RFC 8785 (JCS) canonical form of a signing
  structure `{"mlp_sig": <label>, "protected": {kid, alg, created},
  "payload": <document>}`. The `protected` block is inside the signed input;
  the `mlp_sig` label provides domain separation between signature roles.
  Creators SHOULD emit payloads in JCS form on the wire; verifiers MUST
  canonicalize-then-verify; storage of signed artifacts is verbatim received
  bytes (D-28).
- **HTTP request signatures** (transfer pushes; D-26): RFC 9421 HTTP Message
  Signatures under an MLP profile pinning the covered components (method,
  target, reservation token, upload offset, content digest, created).
---

## 3. Entity Model

### 3.1 Overview

MLP defines exactly three payload entities in a strict containment hierarchy
(D-01): **Media**, raw content-addressed binary objects, referenced by —
**Medialets**, immutable author-signed messages, encapsulated during transport
by — **Envelopes**, ephemeral per-dispatch routing wrappers. A fourth,
receiver-local information category, the **Delivery Record** (§3.5), is never
transmitted.

A domain MAY operate any number of Blob Store instances, each with its
own `bs`-role keys (§6.3); “the BS” throughout this specification names
the role, and per-object routing among a domain's instances is the SN's
discretion at reservation time (§7.5, Annex B.6). (D-105)

Media objects are defined entirely by their bytes and their content address
(Section 8 defines their transfer; D-25 defines the URN construction). This
section defines the Medialet, its signed wire form, the Envelope, and the
Delivery Record.

All documents in this section follow the JSON conventions of §2.3 and the
signing conventions of §2.4. Additionally:

- **Optional members are omitted when absent.** The value `null` MUST NOT be
  used to represent absence. Empty optional arrays are omitted.
- Receivers MUST ignore unknown members (§2.3, rule 5); where a document is
  signed, unknown members present in the signed form are covered by the
  signature like any other member.

### 3.2 The Medialet

A Medialet is a JSON object with the following members.

#### 3.2.1 Fields

| Member | Type | Req. | Definition |
|---|---|---|---|
| `mlp` | string | REQUIRED | Protocol version, `"0.1"`. Inside the signed content deliberately (downgrade protection, D-47). |
| `id` | string | REQUIRED | The Medialet-ID: 1–64 characters from `[A-Za-z0-9_-]`, generated by the author's side. UUIDv7 (RFC 9562) is RECOMMENDED. Uniqueness and deduplication scope is the pair (`author`, `id`) (D-46). |
| `author` | string | REQUIRED | The author's Address (Section 4). Under the v1 key model (D-13) the author's domain attests this value. |
| `subject` | string | OPTIONAL | 1–256 Unicode code points. |
| `created` | string | REQUIRED | Authoring timestamp, RFC 3339 UTC. |
| `in_reply_to` | string | OPTIONAL | The **content address** (§3.3.3) of the Signed Medialet being replied to. *Amends D-47, which specified a bare Medialet-ID: bare IDs are unique only within one author's scope and are therefore ambiguous as cross-author references; the content address is globally unique and verifiable.* (D-49) |
| `displayed_to` | array of Recipient | OPTIONAL | Authored, signed, displayed recipients (D-04). |
| `displayed_cc` | array of Recipient | OPTIONAL | As above. |
| `body` | object | REQUIRED | `{ "profile": "mlp-html/1", "content": <string> }`. The profile identifier is fixed in v1; the content grammar is Section 11. |
| `manifest` | array of Manifest Entry | OPTIONAL | Omitted for text-only Medialets, which are legal and expected (replies, acknowledgements). |

A **Recipient** object is `{ "addr": <Address, REQUIRED>, "name": <string,
OPTIONAL, 1–128 code points> }`. Recipients are always objects, never bare
strings (D-49). Client display of `name` is subject to the anti-spoofing
requirements of Section 4 (a `name` that itself resembles an address differing
from `addr` MUST trigger caution UI).

#### 3.2.2 Manifest Entries

| Member | Type | Req. | Definition |
|---|---|---|---|
| `urn` | string | REQUIRED | The Media object's content address (`urn:mlet:`, D-25). Entries MUST have distinct `urn` values. |
| `size` | integer | REQUIRED | Exact size in bytes, ≥ 0. Reservations bind to this value; a push exceeding it is aborted mid-stream (D-18). |
| `type` | string | REQUIRED | MIME media type of the object. |
| `name` | string | OPTIONAL | Display filename, 1–255 code points. A display string only: receivers MUST NOT interpret it as a filesystem path; path separators and traversal sequences carry no meaning and clients MUST neutralize them on save (D-47). |
| `available_until` | string | REQUIRED | RFC 3339 UTC. The sender side's retention promise for this object (D-19); delegated fulfillment is honored only within this window (D-23). |
| `segments` | array of string | OPTIONAL | Per-segment digests over fixed 16 MiB segments for early-abort verification (D-27); exact digest encoding is defined in Section 8. |
| `preview_of` | string | OPTIONAL | The `urn` of another entry **in the same Manifest** for which this entry is a reduced-fidelity preview (MEP-002). Constraints: the referenced `urn` MUST be present in the Manifest; an entry carrying `preview_of` MUST NOT itself be the target of any `preview_of` (no chains); self-reference is forbidden. A violating member is **ignored** at ingest validation (the entry otherwise stands). Purely descriptive — never a policy input (D-111/D-107 inherited; auto-grant continues to key on `size` alone, D-139). |

#### 3.2.3 Constraints

1. A Manifest contains at most **256 entries** (D-20).
2. Every `urn:mlet:` reference appearing in the Body MUST correspond to a
   Manifest entry; a Manifest entry not referenced by the Body is permitted
   (D-02). Enforcement occurs during ingest sanitization (Section 11), which
   is the protocol's only mandated HTML processing step: references absent
   from the Manifest are removed as invalid. Acceptance-policy decisions
   (Section 7) never require Body parsing — the Manifest alone is the policy
   input, by design (D-02).
3. No independent size cap applies to the Body or the Medialet: both are
   bounded transitively by the Envelope cap (§3.4.4).

### 3.3 The Signed Medialet

#### 3.3.1 Wire form

```json
{
  "medialet":  { ...as defined in 3.2... },
  "signature": {
    "mlp_sig":   "author/1",
    "protected": { "kid": "...", "alg": "ed25519", "created": "..." },
    "value":     "<base64url, unpadded>"
  }
}
```

#### 3.3.2 Author Signature construction

Signing (performed by the author's SN under the v1 key model, D-13):

1. Construct the medialet object `M` per §3.2. Creators SHOULD emit `M` in
   JCS (RFC 8785) canonical form on the wire (D-44).
2. Construct `P = { "kid": <key ID of an author-role key of the author's
   domain>, "alg": "ed25519", "created": <signing time, RFC 3339 UTC> }`.
   `P.created` MAY differ from `M.created`.
3. Compute the signing input
   `I = JCS({ "mlp_sig": "author/1", "protected": P, "payload": M })`.
4. Compute `value = BASE64URL-NOPAD( Ed25519-Sign( sk, I ) )`.
5. Assemble the wire object per §3.3.1.

Verification (mirror image): parse the wire object; check
`signature.mlp_sig == "author/1"`; reconstruct `I` by JCS over the parsed
members (verifiers MUST canonicalize-then-verify, D-44); resolve
`signature.protected.kid` via Discovery against the **author role** of the
domain of `medialet.author` (Sections 5–6); verify. Verification timing and
recording obligations are governed by D-32 (verification at ingest, recorded
in the Delivery Record).

The Signed Medialet is byte-immutable thereafter (D-02): it is stored verbatim
as received (D-28) and forwarded unchanged. Because verification is
canonical-form-based, incidental re-serialization by a conforming JSON stack
does not invalidate signatures — but this is robustness, not license: no party
on the delivery path may alter signed content.

#### 3.3.3 Identity: Medialet-ID and the content address

A Medialet has two identifiers with distinct jobs (D-46):

- **The Medialet-ID** (`medialet.id`): the authored, logical message identity.
  Used for (author, id) deduplication and threading.
- **The content address**: `urn:mlet:` computed (per D-25) over the **JCS
  canonical form of the complete Signed Medialet wire object**. Derived, never
  carried inside the Medialet itself. Because it is computed over canonical
  form, all parties derive the identical address regardless of received byte
  variations. It is globally unique, serves exact-duplicate detection, and is
  the reference form for `in_reply_to`.

### 3.4 The Envelope

An Envelope is created by a Signaling Node for one dispatch to one target
domain. It exists only on the wire and in server queues; it is never delivered
to clients (D-03).

#### 3.4.1 Fields

| Member | Type | Req. | Definition |
|---|---|---|---|
| `mlp` | string | REQUIRED | Protocol version, `"0.1"`. |
| `envelope_id` | string | REQUIRED | Same grammar as Medialet-ID; UUIDv7 RECOMMENDED. Deduplication scope is (`origin`, `envelope_id`) (D-20). |
| `created` | string | REQUIRED | Dispatch timestamp, RFC 3339 UTC. Subject to the ±48 h acceptance skew window (D-20). |
| `origin` | string | REQUIRED | The dispatching SN's domain (A-label form for IDN domains). The Hop Signature verifies against this domain's `sn`-role keys. |
| `envelope_to` | array of string | REQUIRED | Non-empty array of bare Addresses (no display names — routing data only). All entries MUST share a single domain: the target domain. Maximum **128** entries (D-52). Bcc semantics: one Envelope per Bcc recipient, naming only that recipient, even when several Bcc recipients share a domain (D-03). |
| `forwarded_by` | string | OPTIONAL | The Address of the mailbox whose action caused this dispatch, when the dispatch results from a forward. Supplies the "received via B" delivery metadata of D-04. A forwarding SN MAY omit it for forwarder privacy. Absent on original dispatches. (D-50) |
| `list` | string | OPTIONAL | An Address: the mailing list on whose behalf this dispatch was exploded. Set by re-dispatching exploders claiming the mailing-list profile (`spec/profiles/`, MEP-004); the profile binds its claimants — for core receivers the member is informational delivery metadata (display "via the list", threading hints). Hop-signed like every Envelope member: the dispatch is the list's own act. |
| `fulfillment_sources` | array of object | OPTIONAL | Each `{ "domain": <string, REQUIRED>, "urns": <array of string, OPTIONAL — absent means all Manifest URNs>, "until": <string, OPTIONAL, RFC 3339 UTC — MEP-001> }`, in preference order (nearest hop first, D-24). `until` is the declaring source's own offer window for the URNs this entry covers: its promise that it will honor grants (as enveloping origin, §7.6) or delegation requests (§9.4) for those objects until the stated time. Absent means the single source `origin` — the direct-dispatch case. An SN dispatching a delegated forward MUST list at least the custody-holding sources it knows, the root origin at minimum (D-22–D-24). |
| `hops` | array of Hop Attestation | OPTIONAL | The signature chain, oldest first (§3.4.2). Absent on original dispatches. Maximum **32** entries (D-51). |
| `medialet` | object | REQUIRED | The Signed Medialet (§3.3), unchanged. |

Note what the Envelope deliberately does **not** contain: endpoint URLs.
Dispatch and callback endpoints are obtained via Discovery from `origin` and
the recipient domain (Section 5); negotiation state — verdicts, Reservations,
`target_url`s — lives exclusively in the synchronous negotiation reply
(Section 7, per D-17). The `origin_url`/`target_url` Envelope members of the
pre-Stage-1 documents are retired (cf. Closing Document §5).

#### 3.4.2 The hop chain

A **Hop Attestation** is
`{ "origin": <domain>, "envelope_id": <string>, "created": <RFC 3339>,
"kid": <string>, "sig": <base64url> }` — the identifying core of a prior
dispatch and its Hop Signature.

When an SN forwards a received Signed Envelope `E_in` as a new dispatch
`E_out`, it: copies `E_in.envelope.hops` (if any) into `E_out.hops`; appends
one Hop Attestation formed from `E_in` (`origin`, `envelope_id`, `created`
from `E_in.envelope`; `kid` from `E_in.signature.protected.kid`; `sig` from
`E_in.signature.value`); carries the Signed Medialet unchanged; and sets its
own `origin`, fresh `envelope_id` and `created`, the new `envelope_to`, and
`fulfillment_sources` per its forwarding mode (D-24).

**Privacy property (D-51).** A Hop Attestation deliberately excludes the prior
Envelope's `envelope_to` and `forwarded_by`. Recipient sets of earlier hops —
including Bcc structure and co-recipients — never travel downstream. The
consequence is accepted openly: third parties cannot cryptographically verify
prior-hop signatures, because they lack the signed content. They do not need
to. Verification duties are:

- The receiving SN MUST fully verify the **current** Hop Signature (it holds
  the complete Envelope) and the **Author Signature** (it holds the complete
  Signed Medialet). Hop Attestations are validated structurally (grammar,
  count, chronology SHOULD be non-decreasing) and treated as provenance data.
- The **root** Hop Attestation is the delegation credential (D-23): a party
  requesting delegated fulfillment presents the chain, and the root origin
  validates the attestation **against its own dispatch records** — it stored
  what it signed — checking that the referenced `envelope_id` was genuinely
  dispatched by it, carried this Medialet (compared by content address), and
  remains within `available_until` and the delegation budget. Protocol
  details in Section 9.

**Loop prevention (D-51).** An SN MUST NOT automatically re-dispatch
(auto-forward, alias-expand, list-redistribute) an Envelope when its own
domain already appears in `hops[].origin` or as `origin` of the received
Envelope. Deliberate, user-initiated forwards MAY proceed regardless (a human
choosing to send something back is not a loop).

#### 3.4.3 The Signed Envelope wire form

```json
{
  "envelope":  { ...as defined in 3.4.1... },
  "signature": {
    "mlp_sig":   "hop/1",
    "protected": { "kid": "...", "alg": "ed25519", "created": "..." },
    "value":     "<base64url, unpadded>"
  }
}
```

Construction and verification are exactly §3.3.2 with label `"hop/1"`, the
envelope object as payload, and the key resolved against the **`sn` role** of
`envelope.origin`. Because the envelope object embeds the Signed Medialet, the
Hop Signature transitively covers the Medialet bytes and the recipient set —
a signed Envelope cannot be replayed with a substituted recipient list or
content (D-20).

#### 3.4.4 Caps and validation at receipt

The complete Signed Envelope document, as transmitted in UTF-8, MUST NOT
exceed **262,144 bytes** (256 KiB, D-20); the cap is measured on the
transmitted bytes (D-52).

On receipt, before any policy evaluation, the Target SN validates in order —
each failure is a synchronous rejection with a machine-readable reason code
(registry in Section 7):

1. Size cap; well-formed JSON; §2.3 conventions; schema of §§3.2–3.4.
2. Supported `mlp` version.
3. `envelope_to` locality: every Address MUST be at a domain this SN serves;
   Envelopes mixing domains or naming non-local domains are rejected.
4. Structural caps: `envelope_to` ≤ 128; `manifest` ≤ 256; `hops` ≤ 32.
5. Timestamp skew: `envelope.created` within ±48 h (D-20).
6. Replay: (`origin`, `envelope_id`) not seen within the retention horizon
   (D-20).
7. Hop Signature verification against `origin`'s `sn` keys; Author Signature
   verification against the author domain's `author` keys (D-32).

Only then does acceptance policy (Section 7) run. Recipient existence,
per-recipient verdicts, and per-URN verdicts are policy outcomes, not
validation outcomes.

### 3.5 The Delivery Record

The Delivery Record is receiver-local (D-04); its storage encoding is
implementation-free, but for each delivered Medialet the receiving SN MUST
retain at minimum (D-32, D-53):

- `received_at`; the delivering Envelope's `origin` and `envelope_id`;
- `forwarded_by` if present, and the received `hops` chain (needed later for
  delegated-fulfillment requests, Section 9);
- the message verdict and per-URN verdicts as issued;
- the Author Signature verification record: result, `kid`, verification time;
- the Hop Signature verification record: result, `kid`, verification time.

Verification at ingest is normative; later re-verification is best-effort
forensics (D-32).

### 3.6 Worked example (informative): Test Vector TV-001

TV-001 is the first entry of the conformance suite (D-41, D-54): a direct
dispatch — one recipient, one Media object, no forwarding. Key material uses
the well-known Ed25519 test keys of RFC 8032 §7.1 so that every value below
is independently recomputable. **All signatures below are genuine computed
values, machine-verified during drafting.** The machine-readable form ships
as `mlp-tv-001.json` with generator `mlp-tv-001-generator.py`.

**Keys** (RFC 8032 TEST 1 as `author` role of `origin.example`, TEST 2 as its
`sn` role; `kid` construction per §6.2):

```
author seed  9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60
author pub   d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a
author kid   bdyqljzaynjgp5mecdwrg6tk4xthzykaup2iua3jnsogerknzmlsizmi
sn seed      4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb
sn pub       3d4017c3e843895a92b70aa74d1b7ebc9c982ccf2ec4968cc0cd55f12af4660c
sn kid       bdyqnbqeil3qzkhtocxe77a7j5qactmknmkncicd6k2glhgyrbvnzs5a
```

**Media object**: the 36 UTF-8 bytes `"MLP test vector 001: media object A\n"`:

```
urn:mlet:bdyqbcgucl4thqvg5tqzv66l4q6myvy3j647jojvokzpw2c4ohcctu6y
```

(Every BLAKE3-256 multihash in base32-lower begins `bdyq` — the multibase
prefix `b` followed by the fixed multihash header bytes `0x1e 0x20`. A useful
eyeball check.)

**The Medialet** (display form; the signed form is the JCS canonicalization):

```json
{
  "mlp": "0.1",
  "id": "019f2c92-1900-7b0f-8f1e-30c7d7d77f8c",
  "author": "petra@origin.example",
  "subject": "TV-001 sample delivery",
  "created": "2026-07-04T10:00:00Z",
  "displayed_to": [ { "addr": "novak@target.example", "name": "Novák Family" } ],
  "body": {
    "profile": "mlp-html/1",
    "content": "<p>Hello from TV-001. File: <a href=\"urn:mlet:bdyqbcgucl4thqvg5tqzv66l4q6myvy3j647jojvokzpw2c4ohcctu6y\">sample.txt</a></p>"
  },
  "manifest": [ {
    "urn": "urn:mlet:bdyqbcgucl4thqvg5tqzv66l4q6myvy3j647jojvokzpw2c4ohcctu6y",
    "size": 36, "type": "text/plain", "name": "sample.txt",
    "available_until": "2026-07-11T10:00:00Z"
  } ]
}
```

**Author Signature** (`protected` uses the author kid, `alg` `ed25519`,
`created` `2026-07-04T10:00:00Z`; signing input is the JCS form of
`{"mlp_sig":"author/1","protected":…,"payload":…}` — reproduced in full in
`mlp-tv-001.json`):

```
value: kJ5A09wU5TcBFjxgWmTWA6QpK_BFaJk2usG0LJ1Nlqhd80jSGKwQ1Zr77YI7aXbG9b5CJ6h30C8u9EUJ8T39CA
```

**Content address of the Signed Medialet** (JCS canonical form of the wire
object, 1,025 bytes):

```
urn:mlet:bdyqhmtxg343efvdn34cvh4xacxbfa7keroljucjvcpvg63rtkvhmlqa
```

**The Envelope** (direct dispatch: no `hops`, no `fulfillment_sources` —
fulfillment defaults to `origin`):

```json
{
  "mlp": "0.1",
  "envelope_id": "019f2c92-2c88-7c16-a1fe-4548abf07edd",
  "created": "2026-07-04T10:00:05Z",
  "origin": "origin.example",
  "envelope_to": [ "novak@target.example" ],
  "medialet": { ...the Signed Medialet above, verbatim... }
}
```

**Hop Signature** (`sn` kid, `created` `2026-07-04T10:00:05Z`, label
`"hop/1"`):

```
value: TiQzJ3TUxh0bpuBiLxzbOXp3Kp5WWfGR1MMDGMCWs0dG3RuFEwG3KjgWUOUwA9yLkJenGbwKwAPPR0bfjgigDQ
```

**Complete Signed Envelope**: 1,297 bytes in JCS canonical form — 0.5% of the
256 KiB Envelope cap.

*Conformance uses (Annex C): signature generation and verification against
known keys; JCS canonicalization agreement; URN and content-address
computation; the `bdyq` multihash prefix; cap accounting.*

## 4. Addressing

### 4.1 Address grammar

An **Address** identifies a mailbox as `local-part@domain` (D-07). The
grammar, in ABNF (RFC 5234):

```abnf
address     = local "@" domain
local       = base [ "+" tag ]
base        = atom *( "." atom )
atom        = 1*( ALPHA / DIGIT / "-" / "_" )
tag         = 1*( ALPHA / DIGIT / "-" / "_" / "." / "+" )
domain      = <a valid IDNA2008 domain name, see 4.3>
```

Constraints:

1. The complete `local` (including any `+` and `tag`) is 1–64 characters.
   In v1, `local` is ASCII only; internationalized local parts are deferred
   to a versioned extension (D-07).
2. The `base` cannot begin or end with `.` and cannot contain `..` (enforced
   by the grammar: atoms are non-empty).
3. Subaddressing: the **first** `+` in `local` separates `base` from `tag`.
   A `tag`, when present, is non-empty and MAY itself contain `+`
   (`alice+a+b` has base `alice`, tag `a+b`). (D-55)
4. `domain` MUST contain at least one `.` (single-label names do not
   federate) and obeys DNS limits: each label ≤ 63 octets, the whole name
   ≤ 253 octets in A-label form.
5. The complete Address in routing form does not exceed 320 octets
   (informative consequence of the above).

### 4.2 Normalization and comparison

Every implementation derives three forms from user input:

**Routing form** — the on-wire form used in `envelope_to`, `author`,
Recipient `addr`, and all protocol documents: `local` lowercased (safe:
ASCII-only in v1), `domain` converted to lowercase A-labels (4.3). The
routing form **retains the tag**.

**Mailbox key** — the delivery identity: routing form with the tag removed
(`base@domain`). The Target SN routes on the mailbox key; the tag is
delivered alongside the message as filing metadata, recorded in the Delivery
Record as `delivered_to` (the full matching `envelope_to` entry, tag
included — extending the D-53 minimum contents). (D-55)

**Display form** — U-labels for the domain, the user's preferred casing for
the local part. Display never feeds comparison.

Comparison rules: equality anywhere in the protocol is equality of routing
forms. Trust-tier and correspondent matching (Section 7) SHOULD compare
mailbox keys — tags do not create or destroy correspondent relationships.

### 4.3 Internationalized domains

Domains are fully IDN-capable from v1 (D-07). On the wire (all protocol
documents, DNS queries, TLS SNI), domains appear as lowercase **A-labels**
(Punycode, RFC 5890–5893). Implementations processing user input MUST apply
IDNA2008 with UTS-46 mapping, non-transitional processing — the contemporary
browser behavior. Invalid IDNA input is rejected, never repaired silently.

### 4.4 Display safety

Two normative client rules, consolidating D-14 and §3.2.1:

1. **Homograph defense.** When displaying an Address from other than a
   Tier 1 correspondent, clients MUST either render the domain in A-label
   form or apply Unicode confusable detection (UTS-39) and visually flag
   mixed-script or confusable domains.
2. **Display-name spoofing.** If a Recipient or author display `name`
   matches the Address pattern of 4.1 and its routing form differs from the
   accompanying `addr`, clients MUST NOT render the name as if it were the
   address and MUST apply caution UI (show the real `addr` prominently).

### 4.5 No reserved local parts

MLP reserves no mailbox names and assigns no semantics to any local part
(there is no `postmaster@` convention). The operational point of contact for
a domain is published in the Domain Document `contact` member (5.2), where
software can find it deterministically. (D-56)

## 5. Discovery

### 5.1 Overview and algorithm

Discovery resolves a domain to its SN endpoint and key set. Exactly one
public endpoint per domain is ever advertised — the SN (D-10); Blob Store
URLs exist only as per-Reservation negotiated values.

The normative algorithm, given a domain `D` in A-label form:

1. **Optional DNSSEC shortcut.** The resolver MAY query the TXT record
   `_medialet.D` (5.3). If the response is DNSSEC-validated and carries a
   `url` parameter, the resolver MAY fetch the Domain Document from that URL
   directly under the hardened profile (5.4). A hint that is *not*
   DNSSEC-validated MUST NOT substitute for step 2; it MAY be used only to
   pre-warm connections or signal probable MLP support. (D-58)
2. **Authoritative path.** GET `https://D/.well-known/medialet.json` under
   the hardened profile (5.4), following at most 3 redirects, each HTTPS
   (D-08).
3. **Validation.** The response MUST be a valid Domain Document (5.2) whose
   `domain` member equals `D` and whose `mlp` versions intersect the
   resolver's. Any failure renders `D` undiscoverable for this attempt.
4. **Cross-check.** If the resolver holds *both* an authoritative-path
   document and a DNSSEC-validated hint-path document for `D`, they MUST
   agree semantically — equal `domain`, equal `sn`, and an identical key set
   compared entry-by-entry. Disagreement is a hard verification failure for
   `D`: the domain is treated as undiscoverable, not resolved by tiebreak
   (D-09).
5. **Caching** per 5.5.

### 5.2 The Domain Document

The Domain Document is a JSON document (conventions of §2.3) served at the
well-known location, possibly via redirects. It MAY be a static file. Size
cap: **65,536 bytes** (D-57, aligned with the hardened-profile response cap).

| Member | Type | Req. | Definition |
|---|---|---|---|
| `domain` | string | REQUIRED | The domain this document is authoritative for, lowercase A-labels. Resolvers MUST verify it equals the queried domain. Prevents cross-domain document confusion, notably at multi-tenant providers. (D-57) |
| `mlp` | array of string | REQUIRED | Supported protocol versions, e.g. `["0.1"]`. |
| `sn` | string | REQUIRED | The domain's SN endpoint: an `https` URL (any port; path allowed). The SN HTTP API is rooted here (Section 7). |
| `keys` | array of Key Entry | REQUIRED | The domain's key set; at most **64** entries. |
| `contact` | string | OPTIONAL | Operational contact: an Address or an `https`/`mailto` URL. (D-56) |
| `capabilities` | array of string | OPTIONAL | Optional-capability tokens this domain's endpoints support, from the §14 capability registry (initial token: `bao-stream/1`, MEP-003). Unknown tokens MUST be ignored. Absence of the member, or of a token, means the capability is not offered — capabilities are strictly additive and their absence never degrades draft-02 behavior. |

A **Key Entry**:

| Member | Type | Req. | Definition |
|---|---|---|---|
| `kid` | string | REQUIRED | The key's self-verifying fingerprint identifier. Construction: §6.2. (D-12) |
| `alg` | string | REQUIRED | `"ed25519"` (mandatory-to-implement; field present for agility, D-12). |
| `key` | string | REQUIRED | The public key material, multibase-encoded. Encoding: §6.1. |
| `roles` | array of string | REQUIRED | Any of `"sn"`, `"bs"`, `"author"` (D-12). A signature verifies only against a key whose roles include the role demanded by the signature's context (§3.3.2, §3.4.3). |
| `not_before` | string | OPTIONAL | RFC 3339 UTC validity start. |
| `not_after` | string | OPTIONAL | RFC 3339 UTC validity end. Keys outside their window MUST be rejected for verification of newly received material; stored-mail semantics follow verification-at-ingest (D-32). |

Multiple concurrently valid keys per role are expected — that is how
rotation works (publish successor, overlap, retire; D-12).

**Provider hosting.** The Domain Document is logically per-domain: a
provider hosting many domains serves a distinct document per domain (the
redirect target encodes the domain in its path or query, e.g.
`https://mlet.provider.net/dd/example.org.json`). The `domain` binding check
of 5.1 step 3 makes cross-tenant mixups a hard failure. (D-57)

**Example** (informative; the TV-001 fixture family):

```json
{
  "domain": "origin.example",
  "mlp": ["0.1"],
  "sn": "https://mlp.origin.example/sn",
  "contact": "hostmaster@origin.example",
  "keys": [
    { "kid": "bdyqljzaynjgp5mecdwrg6tk4xthzykaup2iua3jnsogerknzmlsizmi",
      "alg": "ed25519", "key": "b5ua5owuyagblccvx2vf75u6jmqdtudxbolz5vjrdewxqegti64dvcgq",
      "roles": ["author"] },
    { "kid": "bdyqnbqeil3qzkhtocxe77a7j5qactmknmkncicd6k2glhgyrbvnzs5a",
      "alg": "ed25519", "key": "b5uat2qaxypuehck2sk3qvj2ndn7lzheyfths5rewrtam2vprfl2gmda",
      "roles": ["sn", "bs"] }
  ]
}
```

### 5.3 The DNS hint

The TXT record at `_medialet.<domain>` carries semicolon-separated
`key=value` pairs; unknown pairs are ignored:

```
_medialet.example.org.  IN TXT  "v=MLP1; url=https://mlet.provider.net/dd/example.org.json"
```

`v` is REQUIRED and MUST be `MLP1` for this specification. `url`, when
present, MUST be `https` and points at the Domain Document. A record without
`url` merely signals MLP support.

Trust semantics (D-09, D-58): with DNSSEC validation, the hint is an
authoritative *delegation* — domain-controlled proof equivalent in strength
to the TLS origin — and MAY be followed directly. Without DNSSEC validation
it is corroborative only. In all cases the fetched document remains subject
to the hardened profile, the `domain` binding check, and the 5.1 step 4
cross-check with its hard-fail rule.

Keys MAY additionally be published DKIM-style under
`<selector>._medialetkey.<domain>` (D-12); the same precedence and
disagreement rules apply. Detailed record format: §6.5.

### 5.4 The hardened fetch profile

All server-initiated discovery fetches — the protocol's *only*
server-initiated outbound requests outside granted reservations (D-34) —
obey (D-11, D-59):

1. Method GET; scheme `https`; port 443.
2. Response size cap 65,536 bytes — connections MUST be aborted beyond it.
3. At most 3 redirects, each `https`, each re-subjected to every rule here.
4. IP safety: before connecting (and again on every redirect hop), resolve
   the target and refuse addresses in IANA special-purpose registries —
   including loopback (`127.0.0.0/8`, `::1`), private (RFC 1918,
   `fc00::/7`), link-local (`169.254.0.0/16`, `fe80::/10`), CGNAT
   (`100.64.0.0/10`), unspecified, multicast, and documentation ranges.
   Implementations MUST pin the resolved address for the actual connection
   (defeating DNS-rebinding races between check and use).
5. Timeouts: connect SHOULD be ≤ 5 s, total ≤ 10 s.
6. No credentials, no cookies, no request bodies.

### 5.5 Caching and key freshness

Domain Documents are cached per standard HTTP semantics, with an absolute
ceiling of **24 hours** regardless of served cache headers (D-33). Verifiers
MUST re-fetch upon encountering an unknown `kid`. Failed discovery SHOULD be
negative-cached briefly (RECOMMENDED 5 minutes) to avoid hammering
misconfigured domains. Revocation latency is therefore bounded by the cache
ceiling and is documented as such (D-33).

### 5.6 Per-user resolution (optional)

A domain MAY offer compose-time address resolution. Topology is
**SN-mediated** (D-60): the sender's client asks its *home* SN, which
queries the target SN server-to-server —

```
GET {sn}/resolve?resource=acct:alice@example.org
```

— returning `200` with `{ "addr": "<routing form>", "name": "<optional
display name>" }`, or `404`/`501`. No avatars, no presence, no further
profile data in v1: every additional field is enumeration bait and a
tracking surface.

This endpoint is a courtesy, never a delivery prerequisite (D-10):
recipient existence is authoritatively answered only during negotiation.
Operators MAY disable it entirely, answer only authenticated or Tier 1
peers, and MUST rate-limit it if enabled; the anti-enumeration rationale is
normative context (D-10).

## 6. Keys and Signatures

### 6.1 Key material and encoding

MLP public keys are encoded with the multiformats conventions already used
throughout this specification (multihash for digests, D-25; multibase
everywhere, always base32-lower):

```
key = "b" + BASE32LOWER-NOPAD( multicodec-key-bytes )
multicodec-key-bytes = <multicodec prefix> || <raw public key>
```

For Ed25519 — the mandatory-to-implement algorithm (D-12) — the multicodec
prefix is `0xED 0x01` (`ed25519-pub`) and the raw public key is 32 bytes,
yielding a 56-character encoding after the `b` prefix. (D-61)

The key material is thereby self-describing independently of surrounding
fields. On loading any Key Entry, verifiers MUST check that the multicodec
prefix corresponds to the entry's `alg`; a mismatch invalidates the entry.
Private keys never appear in any protocol document or record.

### 6.2 Key identifiers

The `kid` of a key is its self-verifying fingerprint (D-12):

```
kid = "b" + BASE32LOWER-NOPAD( multihash( blake3-256, multicodec-key-bytes ) )
```

The digest is computed over the **multicodec-prefixed** key bytes, not the
raw key: the algorithm identity is thereby bound into the fingerprint, so
identical raw bytes under different algorithms yield different kids,
foreclosing cross-algorithm confusion at the identifier level. (D-62)

Because the kid is deterministic from published material, self-verification
is normative: on loading a key set, implementations MUST recompute each
entry's kid from its `key` member and MUST ignore entries whose published
`kid` does not match (the remainder of the document is still processed).
The same rule applies to keys obtained from DNS records (§6.5).

A kid is 57 characters — within the 63-octet DNS label limit by design; the
kid itself serves as the DNS selector in §6.5, so no separate selector
namespace exists to manage.

### 6.3 Key sets, roles, and authority

Key Entries and their `roles`, validity windows, and the 64-entry cap are
defined in §5.2. The following semantics complete them:

1. **Role enforcement.** A signature verifies only against a key whose
   `roles` include the role its context demands: `author/1` requires
   `author`; `hop/1` and `verdict/1` require `sn`; HTTP transfer signatures
   (§6.6) require `bs`. Role mismatch is verification failure. (D-12)
2. **Exact-domain authority.** Keys speak for exactly the `domain` of the
   Domain Document that publishes them. There is no inheritance across the
   DNS hierarchy: keys of `example.org` prove nothing about
   `sub.example.org`, and vice versa. (D-63)
3. **Validity windows.** An absent `not_before` means valid from the
   beginning of time; absent `not_after`, no scheduled expiry. Windows are
   evaluated at verification time — which, per D-32, is ingest; stored
   material retains its ingest verdict when keys later expire or vanish.
4. **Rotation** (informative): publish the successor entry alongside the
   incumbent, begin signing under the new kid, retire the old entry after
   the overlap exceeds the 24-hour cache ceiling (D-33). Every signature
   names its kid, so mixed-key periods are unremarkable.
5. **Revocation** is removal from the Domain Document, effective within the
   cache ceiling — soft, bounded, and disclosed (D-33). On suspected
   compromise, operators remove and rotate; §12 carries the per-role
   compromise analysis frozen in Stage 1.
6. **The Domain Document is not self-signed.** Its authority is the TLS
   origin that serves it (D-08); a signature by a key the document itself
   introduces would be circular and is deliberately absent. (D-63)
7. **Dedicated keys.** MLP keys SHOULD NOT be reused as TLS, SSH, DKIM, or
   other-protocol keys. Domain separation (§6.4) protects MLP's own label
   space, not foreign protocols' signing formats.

### 6.4 Document signatures

This section generalizes the construction of §3.3.2 to all MLP document
signatures. (D-44, D-64)

**Signing.** Given a payload object `M`, a signature label `L`, and a
signing key with identifier `kid`:

```
P = { "kid": kid, "alg": "ed25519", "created": <RFC 3339 UTC> }
I = JCS( { "mlp_sig": L, "protected": P, "payload": M } )
value = BASE64URL-NOPAD( Ed25519-Sign( sk, I ) )
signature = { "mlp_sig": L, "protected": P, "value": value }
```

**Verification.** Parse; require `signature.mlp_sig` to equal the label the
consuming context demands (an `author/1` signature presented where `hop/1`
is expected fails, whatever its cryptographic validity); reconstruct `I` by
JCS over the parsed members; resolve `kid` via Discovery against the
required role of the authoritative domain for the context (the domain of
`medialet.author` for `author/1`; `envelope.origin` for `hop/1`; the
verdict-issuing domain for `verdict/1`); check role, validity window, and
kid self-verification per §§6.2–6.3; verify under RFC 8032.

**Ed25519 profile** (D-63): pure Ed25519 per RFC 8032 — not Ed25519ph (all
signing inputs are small JCS documents; pre-hashing buys nothing) and not
Ed25519ctx (context-string library support remains uneven; domain separation
is achieved in-band by the `mlp_sig` label, which is inside the signed
input). Signatures are 64 bytes, base64url without padding (86 characters).
Implementations SHOULD use established, audited cryptographic libraries and
MUST NOT implement the primitives themselves.

**Signature labels** form a registry, founded here and administered per
Section 14 (D-64). Label grammar: `1*( %x61-7A / DIGIT / "-" ) "/" 1*DIGIT`
— a lowercase name, a slash, a version. Initial entries:

| Label | Payload | Signing role | Defined in |
|---|---|---|---|
| `author/1` | Medialet | `author` | §3.3 |
| `hop/1` | Envelope | `sn` | §3.4 |
| `verdict/1` | Negotiation reply | `sn` | §7 |
| `delegation/1` | Delegation request | `sn` | §9.4 |

Further labels are added by registry action, never by reuse of an existing
label for a new payload type.

**A note on malleability and content addresses** (informative): content
addresses of signed artifacts bind to the exact signature bytes (§3.3.3).
Ed25519 signatures are not unique per message in principle (a signer can
produce distinct valid signatures by varying `P.created`, if nothing else),
so two artifacts can share (author, id) while differing in content address.
This is by design: (author, id) is *logical* identity and drives
deduplication; the content address is *exact-artifact* identity and drives
references (`in_reply_to`). No MLP mechanism assumes signature uniqueness.

### 6.5 DNS key records (optional)

A domain MAY additionally publish individual keys DKIM-style (D-12, D-65):

```
<kid>._medialetkey.<domain>.  IN TXT  "v=MLP1; alg=ed25519; key=<multibase key>"
```

The owner label is the key's own kid (§6.2). Pairs are semicolon-separated;
unknown pairs are ignored; `v` and `key` are REQUIRED, `alg` defaults to
`ed25519`.

Trust semantics mirror §5.3 (D-58): a **DNSSEC-validated** record is an
authoritative source for that kid, usable even when the Domain Document is
temporarily unreachable; an unvalidated record is corroborative only and
never substitutes for the HTTPS chain. If any two sources yield the same
kid with different key bytes — record vs. record, or record vs. Domain
Document — that is a hard verification failure for the domain (D-09).
Records lack `roles` and validity windows; a key learned only from DNS
therefore verifies only after its roles are confirmed via the Domain
Document, except under DNSSEC where the record MAY carry an additional
`roles=` pair (comma-separated) with the same meaning as §5.2.

### 6.6 HTTP message signatures (transfer profile)

Transfer requests (Section 8) are signed per **RFC 9421** with the following
MLP profile (D-45, D-66):

1. Signature algorithm `ed25519` (RFC 9421 §3.3.6); `keyid` is a kid whose
   key carries the `bs` role of the pushing domain; signature label (the
   RFC 9421 dictionary key) is `mlp`.
2. Covered components — requests with a body (PATCH):
   `("@method" "@target-uri" "content-digest" "upload-offset"
   "mlp-reservation")`; requests without a body (HEAD):
   `("@method" "@target-uri" "mlp-reservation")`.
   `MLP-Reservation` is the reservation-token header (Section 8);
   `Upload-Offset` is the tus offset header.
3. Signature parameters: `created` (REQUIRED) and `keyid` (REQUIRED);
   `alg` MAY be present and MUST then be `ed25519`. Receivers MUST enforce
   a freshness window on `created` (RECOMMENDED 300 seconds) — replay of
   PATCH bodies is additionally neutralized by offset advancement, and
   reservation tokens are single-use-per-completion (D-18).
4. `Content-Digest` (RFC 9530) uses **sha-256** — a registered digest
   token with universal tooling. Request-level digests are transport
   hygiene and die with the request; *object identity and verification
   remain BLAKE3 via the URN* (D-25, D-27). Two jobs, two hashes,
   deliberately.

### 6.7 TV-001, final form (informative)

Recomputed under §§6.1–6.2; identifiers unchanged, key-derived values final;
machine-readable form in `mlp-tv-001.json`, which now also carries the
Domain Document fixture below. The canonical Signed Envelope remains
**1,297 bytes** (all replaced strings are length-preserving).

```
author key   b5ua5owuyagblccvx2vf75u6jmqdtudxbolz5vjrdewxqegti64dvcgq
author kid   bdyqljzaynjgp5mecdwrg6tk4xthzykaup2iua3jnsogerknzmlsizmi
sn/bs key    b5uat2qaxypuehck2sk3qvj2ndn7lzheyfths5rewrtam2vprfl2gmda
sn/bs kid    bdyqnbqeil3qzkhtocxe77a7j5qactmknmkncicd6k2glhgyrbvnzs5a

author sig   kJ5A09wU5TcBFjxgWmTWA6QpK_BFaJk2usG0LJ1Nlqhd80jSGKwQ1Zr77YI7aXbG9b5CJ6h30C8u9EUJ8T39CA
hop sig      TiQzJ3TUxh0bpuBiLxzbOXp3Kp5WWfGR1MMDGMCWs0dG3RuFEwG3KjgWUOUwA9yLkJenGbwKwAPPR0bfjgigDQ
content addr urn:mlet:bdyqhmtxg343efvdn34cvh4xacxbfa7keroljucjvcpvg63rtkvhmlqa
```

Domain Document fixture for `origin.example`:

```json
{
  "domain": "origin.example",
  "mlp": ["0.1"],
  "sn": "https://mlp.origin.example/sn",
  "contact": "hostmaster@origin.example",
  "keys": [
    { "kid": "bdyqljzaynjgp5mecdwrg6tk4xthzykaup2iua3jnsogerknzmlsizmi",
      "alg": "ed25519",
      "key": "b5ua5owuyagblccvx2vf75u6jmqdtudxbolz5vjrdewxqegti64dvcgq",
      "roles": ["author"] },
    { "kid": "bdyqnbqeil3qzkhtocxe77a7j5qactmknmkncicd6k2glhgyrbvnzs5a",
      "alg": "ed25519",
      "key": "b5uat2qaxypuehck2sk3qvj2ndn7lzheyfths5rewrtam2vprfl2gmda",
      "roles": ["sn", "bs"] }
  ]
}
```

*Additional conformance uses established by this section (Annex C): kid
self-verification (positive and corrupted-entry cases); multicodec/`alg`
cross-check; label context-mismatch rejection (`author/1` where `hop/1`
expected); RFC 9421 signature generation/verification for PATCH and HEAD
shapes.*

## 7. Negotiation and Acceptance

### 7.1 Overview

Negotiation is where the protocol's governing principle (D-15) becomes wire
reality: an Envelope dispatch is answered *synchronously* with a signed
verdict; no Media byte moves except under a Reservation contained in such a
verdict; and no message ever flows to an unverified party (D-17).

One rule shapes every exchange in this section: **signed verdicts are issued
only for verified Envelopes** (D-69). Structural failures, cap violations,
rate limiting, and signature-verification failures receive plain, unsigned
HTTP problem responses — a Target SN never binds its signing key to
statements about material it could not verify.

### 7.2 The SN API

The SN's server-to-server API is rooted at the `sn` URL of the Domain
Document (§5.2). Three endpoints exist in this specification (D-68):

| Endpoint | Method | Purpose |
|---|---|---|
| `{sn}/dispatch` | POST | Receive a Signed Envelope; reply with a signed verdict (§7.3–7.4). |
| `{sn}/verdict` | POST | Receive a verdict *update* for a previously dispatched Envelope (§7.6). |
| `{sn}/resolve` | GET | Optional per-user resolution (§5.6). |

Media types: requests to `/dispatch` carry
`application/mlp-envelope+json`; signed verdicts are
`application/mlp-verdict+json`; unsigned errors are
`application/problem+json` (RFC 9457) whose `type` member is the reason-code
URI `urn:mlp:err:<code>` (§7.8). All traffic is HTTPS per D-14. Servers
SHOULD answer rate-limited peers with 429 and `Retry-After`.

The client↔SN interface (composition, mailbox access, the recipient's
accept action) is explicitly out of scope for this core specification; it is
a product surface (Stage 3), with a possible companion specification later
(D-68). Nothing in federation depends on its shape.

### 7.3 Dispatch processing

On `POST {sn}/dispatch`, the Target SN runs the §3.4.4 validation sequence.
Failures there yield unsigned problem responses: 413 `envelope-too-large`;
400 `malformed`, `version-unsupported`, `timestamp-skew`; 401
`signature-invalid`; 409 `replay`; 429 `rate-limited`; 502
`discovery-failed` (the origin's Domain Document could not be obtained to
verify). After successful validation, acceptance policy (§7.7) runs, and
the SN answers `200` with a signed verdict document (§7.4) — including for
outcomes that are rejections: a verified rejection is a protocol result,
not a transport error.

**Retry idempotency** (D-74, refining D-20). Dispatch is retried by origins
whenever the reply is lost. A re-POST presenting an already-verdicted
(`origin`, `envelope_id`) whose Signed Medialet content address matches the
recorded dispatch MUST be answered with the current verdict snapshot
(re-issued or re-signed as needed), not an error — within an idempotency
window of RECOMMENDED 24 hours. The same (`origin`, `envelope_id`) with
*different* content is the replay attack D-20 targets and is refused with
`replay`.

### 7.4 The verdict document

The verdict is a signed document under label `verdict/1` (§6.4), signed by
an `sn`-role key of the issuing domain. Its payload:

| Member | Type | Req. | Definition |
|---|---|---|---|
| `mlp` | string | REQUIRED | Protocol version. |
| `verdict_id` | string | REQUIRED | Identifier of this verdict document (Medialet-ID grammar; UUIDv7 RECOMMENDED). |
| `created` | string | REQUIRED | RFC 3339 UTC. Ordering key for snapshots (§7.6). |
| `issuer` | string | REQUIRED | The issuing (target) domain; the signature verifies against its `sn` keys. |
| `envelope_origin` | string | REQUIRED | `origin` of the dispatch being answered. |
| `envelope_id` | string | REQUIRED | `envelope_id` of the dispatch being answered. |
| `message` | string | REQUIRED | Summary verdict: `accepted` if at least one recipient accepted; else `quarantined` if at least one quarantined; else `rejected`. Derived from `recipients`, present for cheap handling. |
| `reason` | string | OPTIONAL | Reason code when the summary is `rejected` or `quarantined` and one code describes it. |
| `recipients` | array | REQUIRED | Per-recipient outcomes (below). |
| `media` | array | OPTIONAL | Per-URN outcomes (below); present when the Manifest was non-empty. |

**Per-recipient outcomes** (D-70, amending D-16): each entry is
`{ "addr": <routing form from envelope_to>, "verdict": "accepted" /
"rejected" / "quarantined", "reason": <code, OPTIONAL> }`. Stage 1's single
message verdict implicitly assumed one recipient per Envelope; with up to
128 recipients of differing standing (one a correspondent, one a stranger,
one nonexistent), outcomes are irreducibly per-recipient — a typo in one
address must not poison delivery to five valid ones. Every `envelope_to`
entry MUST appear exactly once in `recipients`.

**Per-URN outcomes**: each entry is `{ "urn": <from the Manifest>,
"verdict": "grant" / "have" / "defer" / "deny", "reason": <code, OPTIONAL>,
"reservation": <Reservation, REQUIRED with grant> }`. Per-URN verdicts
remain **singular per Envelope, at domain level** (D-70): Media lands in the domain's storage once, so the verdict is computed as the **union
need** across accepted recipients — `grant` if any accepted recipient's
policy grants transfer; `have` if the store already holds the object
(subject to §7.5 masking); `defer` if no policy grants now but a recipient
acceptance could later upgrade; `deny` if no recipient could ever receive
it. Recipients with stricter standing than the union are gated by local
mailbox policy, not by refusing bytes that another recipient legitimately
caused to arrive — content addressing makes this the intended deduplication
behavior, not a bypass. Every Manifest `urn` MUST appear exactly once in
`media` (when any recipient was accepted or quarantined; a fully rejected
Envelope MAY omit `media`).

### 7.5 Reservations

A Reservation is the receiving side's signed, scoped invitation to push
(D-18), carried inside a `grant` entry:

| Member | Type | Req. | Definition |
|---|---|---|---|
| `urn` | string | REQUIRED | The invited object — and only it. |
| `max_size` | integer | REQUIRED | The Manifest-declared size. A push exceeding it is aborted mid-stream (D-18, Section 8). |
| `target_url` | string | REQUIRED | The per-reservation BS ingestion URL (`https`; the only context in which BS location is ever disclosed, D-10). |
| `token` | string | REQUIRED | Opaque capability, 1–512 characters; presented in the `MLP-Reservation` header of every push request (§6.6); single-use-per-completion (D-18). |
| `expires` | string | REQUIRED | RFC 3339 UTC; RECOMMENDED issue-time + 72 hours (D-18). Expired reservations are worthless; origins simply re-negotiate. |

**Pusher-side connection safety** (D-72). `target_url` is
remote-controlled input to the pusher. Before connecting — and on any
redirect, which pushers SHOULD refuse entirely — the pushing BS MUST apply
the IP-safety rules of §5.4 item 4 (special-purpose ranges blocked,
resolve-then-pin) and MUST require `https`. Without this, a malicious
target domain could aim an origin's Blob Store at cloud metadata endpoints
or internal services and feed them attacker-chosen bytes.

**`have` masking** (D-29, restated at its enforcement point): to
non-correspondents, possession is not disclosed — the SN issues an
ordinary-looking `grant`, and the BS discards the duplicate bytes on
completed ingest. The masked reservation is indistinguishable by
construction; conformance tests exercise only Tier 1 `have` visibility.

### 7.6 Verdict updates

Later state changes — a recipient accepting deferred Media (D-19), a quota
freeing up, an operator revocation — flow as **new verdict documents**
POSTed to the *origin's* `{sn}/verdict` endpoint (D-71). Updates are
**idempotent snapshots**: a full current `recipients` and `media` state for
the referenced (`envelope_origin`, `envelope_id`), superseding by
`created` order (latest wins; ties broken by `verdict_id` lexicographic
order, which UUIDv7 makes chronological anyway).

The receiving origin SN: verifies the `verdict/1` signature against the
`issuer`'s `sn` keys; matches (`envelope_origin` = itself, `envelope_id`)
to an outstanding dispatch (else 404 `unknown-envelope`); and applies
transitions per this table —

| From | To | Meaning |
|---|---|---|
| `defer` | `grant` | Recipient accepted; Reservation attached; push now. |
| `defer` | `deny` | Recipient declined or policy closed; terminal. |
| `grant` | `grant` | Fresh Reservation (e.g., prior one expired unused). |
| `grant` | `deny` | Revocation; honored if the push has not completed. |
| `have`, `deny` | — | Terminal; updates MUST NOT alter them. |

Anything else is `invalid-transition` (problem response; the update is
discarded). Origins answer valid updates `204`.

Verdict updates are the protocol's *only* receiver-initiated contact with
an origin, they target only the already-verified negotiation counterparty,
and they reference only material that party dispatched — preserving D-17's
no-backscatter guarantee by construction.

### 7.7 Default acceptance policy

Mechanics above are MUST-grade; the policy that drives them is
operator-owned. This subsection renders the frozen tiers (D-19) as
RECOMMENDED defaults — the behavior of an unconfigured conformant SN:

| Tier | Membership | Message default | Media default |
|---|---|---|---|
| 1 — Correspondent | Prior outbound dispatch from this mailbox to the sender (mailbox-key comparison, D-55); explicit allowlist; same domain | `accepted` | `grant` within quota headroom; `have` disclosed |
| 2 — Verified stranger | First contact; signatures verify; domain hygiene passes | `accepted` | `defer`, reason `pending-acceptance`; `have` masked |
| 3 — Suspect | Verifiable but flagged by policy hooks (D-21) | `quarantined` | `defer` or `deny` |
| — Unverifiable | Signature or discovery failure | *(no verdict — unsigned 401, §7.3)* | — |

The recipient-acceptance flow completing Tier 2: the recipient's SN, upon
the accept action, issues `defer → grant` updates (§7.6) within the
sender's declared `available-until` windows (D-19); past-window upgrades
fail gracefully at push time and surface as "request a resend."

Enforcement points restated at their wire locations (D-20): quota headroom
is checked before any `grant`, insufficiency yielding `defer` with reason
`quota`; per-origin pending-reservation caps bound outstanding invitations;
negotiation rate limits answer 429 *before* signature verification where
load demands.

### 7.8 Reason codes

The reason-code registry is founded here and administered per Section 14
(D-73). Codes are lowercase-hyphen tokens; in problem responses they appear
as `urn:mlp:err:<code>`. Initial entries:

**Transport/validation (unsigned contexts):** `malformed`,
`envelope-too-large`, `version-unsupported`, `signature-invalid`,
`timestamp-skew`, `replay`, `rate-limited`, `discovery-failed`,
`unknown-envelope`, `invalid-transition`.

**Recipient-level:** `unknown-recipient`, `mailbox-disabled`, `policy`,
`suspected-junk`.

**Media-level:** `pending-acceptance`, `quota`, `type-forbidden`,
`size-exceeds-policy`, `hash-blocklist`, `delegation-budget` (§9.5).

### 7.9 Worked example (informative): Test Vector TV-002

TV-002 is the negotiation transcript for the TV-001 dispatch, in
`mlp-tv-002.json` with the `target.example` Domain Document fixture. The
issuer's `sn` key is RFC 8032 Ed25519 TEST 3 (public
`fc51cd8e…908025`, asserted in the generator):

```
target kid   bdyqdn2sf4ms6wvldnq7ieuwkd6lmzk4kk7esux3od27t5v73arcinga
target key   b5ua7yuonrzrbrindrwsh5uacgdyfqcaw5uj3umydvro6xeivjciiaji
```

**Verdict 1** — the synchronous reply at `2026-07-04T10:00:06Z`
(Tier 2 first contact; 708 canonical bytes):

```json
{
  "mlp": "0.1",
  "verdict_id": "019f2c92-3070-7d18-adda-f5b677a35e4a",
  "created": "2026-07-04T10:00:06Z",
  "issuer": "target.example",
  "envelope_origin": "origin.example",
  "envelope_id": "019f2c92-2c88-7c16-a1fe-4548abf07edd",
  "message": "accepted",
  "recipients": [ { "addr": "novak@target.example", "verdict": "accepted" } ],
  "media": [ { "urn": "urn:mlet:bdyqbcgucl4thqvg5tqzv66l4q6myvy3j647jojvokzpw2c4ohcctu6y",
               "verdict": "defer", "reason": "pending-acceptance" } ]
}
```
```
signature.value: MffmU5sreo-2sW5Bwi-EgWq56-wrg4CX-rPxYNMRgKM_1MC_jp1Qhq0wWmFJsYwlDOFEOjfBWd55P2SRVYp2BA
```

**Verdict 2** — the upgrade snapshot at `2026-07-04T12:30:00Z`, after the
recipient's accept action, POSTed to `origin.example`'s `/verdict`
endpoint (923 canonical bytes). Identical addressing members; the `media`
entry becomes:

```json
{ "urn": "urn:mlet:bdyqbcgucl4thqvg5tqzv66l4q6myvy3j647jojvokzpw2c4ohcctu6y",
  "verdict": "grant",
  "reservation": {
    "urn": "urn:mlet:bdyqbcgucl4thqvg5tqzv66l4q6myvy3j647jojvokzpw2c4ohcctu6y",
    "max_size": 36,
    "target_url": "https://bs.target.example/ingest/24c372e9a5a3c559",
    "token": "Yam_b2ATZeRdhaofjfPPEasHKNDlGBAB",
    "expires": "2026-07-07T12:30:00Z"
  } }
```
```
verdict_id:      019f2d1b-6d40-7dae-a190-9b835c6df3f6
signature.value: tWsW5eCEb1qURos7X_kvOMEpZxaJt3EOWHXP8OapVW34Pm3VTIwWTfaQe1beuwqLmvenCQj6Zq5CXKcDA1H6Bg
```

*Conformance uses (Annex C): `verdict/1` generation and verification;
per-recipient/per-URN completeness rules; snapshot supersession ordering;
transition-table enforcement (including `invalid-transition` on
`deny → grant`); retry-idempotency behavior; the reservation-token and
`target_url` shapes feeding Section 8's transfer tests.*

## 8. Transfer

### 8.1 Overview and scope

Transfer moves one Media object from a pushing Blob Store to a receiving
Blob Store under a Reservation (§7.5). It is the protocol's only heavy-byte
path, it is pure push (D-11), and it binds the **tus 1.0 core protocol**
(D-26) with MLP's authentication and verification riding on top.

Scope boundaries, stated once: transfer is strictly server-to-server — no
browser is ever a party to a push. The interface between an SN and its own
BS is **intra-domain and not specified** (D-79): SN and BS are roles, not
boxes (D-01), and how a domain's SN learns of completed ingests, mints
upload resources, or shares reservation state is deployment freedom. Only
the cross-domain surface below is normative. Per D-30, at most one stream
per object; concurrency is achieved across objects, bounded by the
Manifest cap.

### 8.2 The upload resource

The Reservation *is* the upload resource (D-26): granting it pre-creates
the resource at `target_url`; tus creation and termination extensions are
therefore not used — creation happened at negotiation, termination is
expiry (D-76). Binding requirements:

1. Every request and response carries `Tus-Resumable: 1.0.0`.
2. Every request carries `MLP-Reservation: <token>` and the RFC 9421
   signature headers per the §6.6 profile (`bs`-role key of the pushing
   domain; covered components exactly as ordered in D-66).
3. `Upload-Length` equals the Reservation's `max_size` — which is the
   Manifest-declared **exact** size — and is known from creation.
4. Responses SHOULD carry `Upload-Expires` (HTTP-date of the Reservation's
   `expires`). Responses are not signed in v1 (D-79): the pusher reached
   `target_url` from a signed verdict over TLS; response signing would add
   ceremony without a threat it addresses.
5. At most one PATCH may be in flight per resource; an overlapping PATCH
   is refused with 409 `offset-mismatch` semantics (D-76).
6. Pushers MUST apply the connection-safety rules of D-72 (§7.5) and
   SHOULD refuse redirects outright.

### 8.3 HEAD — offset discovery

`HEAD {target_url}` (signed, bodiless component set) returns 200 with
`Upload-Offset` (the current durable offset — §8.5 defines durable),
`Upload-Length`, and `Cache-Control: no-store`. HEAD is idempotent, is the
resumption primitive, and never changes state.

### 8.4 PATCH — pushing bytes

`PATCH {target_url}` with `Content-Type: application/offset+octet-stream`
(else 415), `Upload-Offset: N`, `Content-Digest` (sha-256 over this
request's body, RFC 9530), the token header, and the signature. `N` MUST
equal the resource's current offset, else 409 `offset-mismatch` (the
pusher then HEADs and realigns). The body is the contiguous byte range
starting at `N`; pushers choose their own chunking, RECOMMENDED at most
256 MiB per PATCH (D-76); aligning PATCH boundaries to the 16 MiB segment
grid (§8.6) is convenient but not required.

**Transactional verification pipeline** (D-77). Order of operations at the
receiving BS:

1. Token validity and expiry; signature verification from headers alone —
   the signature covers the *claimed* `Content-Digest` value, so header
   verification is possible before any body byte is read. Failures here
   consume nothing.
2. The body streams to the reservation's quarantined partial (§8.7) while
   two incremental digests advance: the object-level BLAKE3 and the
   request-level SHA-256. A cumulative size exceeding `max_size` aborts
   the stream immediately, mid-request (D-18, D-27).
3. At body end, the computed SHA-256 is compared with the `Content-Digest`
   header. On mismatch the request fails with 422 `digest-mismatch`, and
   the resource **rolls back**: the partial is truncated to the offset of
   the last successful PATCH and the persisted BLAKE3 state reverts to
   that checkpoint. On success, offset and checkpoint advance together.

The invariant: **offset and hasher state advance only on a fully verified
PATCH; every successful PATCH boundary is a durable checkpoint.** This is
what HEAD reports and what restarts recover to. The two failure taxa must
not be conflated (D-77): `digest-mismatch` is *this request* arriving
corrupted — retryable at the same offset; `hash-mismatch` (§8.6) is *the
object* failing its URN — reset to zero, the source is wrong.

**Completion.** When a verified PATCH brings the offset to `Upload-Length`,
the BS finalizes the BLAKE3 state and compares it with the URN's digest.
Match: the object leaves quarantine, becomes live and `have`-answerable,
the token is consumed (D-18), and the response is 204 with
`MLP-Object-State: verified`. Mismatch: 422 `hash-mismatch`, the partial
is discarded, and — if the Reservation is unexpired — the resource resets
to offset 0 for a clean re-push (D-27).

### 8.5 Failure taxonomy

Problem responses (`application/problem+json`, `urn:mlp:err:` types) with
status mappings; the starred codes extend the §7.8 registry (D-79):

| Status | Code | Meaning / pusher action |
|---|---|---|
| 409 | `offset-mismatch`\* | `Upload-Offset` ≠ current offset, or overlapping PATCH. HEAD, realign, retry. |
| 422 | `digest-mismatch`\* | Request body failed its `Content-Digest`. Retry same offset. |
| 422 | `hash-mismatch`\* | Object failed URN verification (final or segment). Investigate source; re-push from 0 if reservation lives. |
| 410 | `reservation-expired`\* | Past `expires`. Re-negotiate (§7.6 `grant → grant`). |
| 401/410 | `reservation-invalid`\* | Unknown or already-consumed token. Re-negotiate. |
| 408 | `stalled`\* | Terminated below minimum throughput (§8.6). Resume from checkpoint. |
| 401 | `signature-invalid` | RFC 9421 verification failed (incl. `created` outside the 300 s window, D-66). |
| 415 | — | Wrong content type (tus semantics). |

### 8.6 Object verification, segments, and quarantine

**Incremental BLAKE3 with persisted state** (D-27): the hasher state is
persisted alongside the partial at every checkpoint and SHOULD survive
process restarts. If state is lost (crash before persistence, storage
fault), the BS MAY discard the partial; HEAD then reports offset 0 and the
pusher restarts — graceful degradation, never wrong answers.

**Segment digests** (D-78, finalizing the encoding deferred from D-47):
when a Manifest entry carries `segments`, each element is the multibase
multihash (BLAKE3-256, the familiar `bdyq…` strings) of one **fixed 16 MiB
segment** of the object, the final element covering the remainder; the
array length MUST equal `ceil(size / 16 MiB)` or the entry is malformed.
A receiving BS SHOULD verify each segment as its boundary is crossed and
abort early on mismatch — which is an **object-level** failure:
`hash-mismatch`, reset to zero, exactly as if finalization had failed.
Segment verification is quality-of-implementation (D-27); the completion
check is the conformance floor.

**Slow-loris defense** (Stage 1 threat model, made concrete): the BS MAY
enforce a minimum average throughput after a grace period (RECOMMENDED:
no less than 64 KiB/s averaged over 5 minutes, after 1 minute of grace)
and terminate violators with `stalled`. The Reservation survives; the
checkpoint stands; a sincere-but-slow pusher resumes and merely makes slow
progress across attempts.

**Quarantine invariants** (D-27): unverified partials are keyed by
reservation, invisible to resolution (§10), never `have`-answerable, never
counted as recipient storage, and garbage-collected at reservation expiry.

### 8.7 The resumption procedure

The normative recovery loop, applicable at any interruption — network
death, either host rebooting, a lost 204:

1. Pusher: `HEAD {target_url}` (signed). If `reservation-expired` /
   `reservation-invalid` → re-negotiate via §7.6; a fresh Reservation MAY
   arrive with a new `target_url`, and the transfer restarts from that
   resource's offset (0 for a new resource).
2. Read `Upload-Offset: N` — the receiver's durable checkpoint. Any bytes
   the pusher sent beyond `N` were rolled back or never landed; they are
   simply re-sent.
3. Seek to `N` in the source object; PATCH the remainder (one or more
   requests, each independently signed).
4. On 204: if `MLP-Object-State: verified`, done. On `offset-mismatch`:
   go to 1 (another retry raced ahead, or rollback moved the checkpoint).
   On `digest-mismatch`: retry the same range. On `hash-mismatch`: stop
   and alarm — the source bytes do not match the Manifest's URN, which is
   a content problem, not a transport problem (D-27: a professional
   sender wants this message loudly).

The Stage 1 headline scenario, restated as arithmetic: a 50 GB push dying
at byte 49,999,999,999 resumes with one HEAD, one PATCH of the final byte,
and finalization — zero redundant bytes re-transferred, the same loop as
any other interruption.

### 8.8 Worked example (informative): Test Vector TV-003

TV-003 (`mlp-tv-003.json`) is a complete interrupted-and-resumed push of
the TV-001 object under the TV-002 Reservation — pusher key: RFC 8032
TEST 2 (`bs` role of `origin.example`), all signatures genuine and
machine-verified; the final BLAKE3 equals the TV-001 URN by assertion.

**Step 1 — PATCH bytes 0–19** (`created=1783168260`). The exact RFC 9421
signature base, showing every covered component of the D-66 body profile:

```
"@method": PATCH
"@target-uri": https://bs.target.example/ingest/24c372e9a5a3c559
"content-digest": sha-256=:elXyczRHrRqKGKzoTRfF6/frGBwDKKhGCX5MVfIvkIE=:
"upload-offset": 0
"mlp-reservation": Yam_b2ATZeRdhaofjfPPEasHKNDlGBAB
"@signature-params": ("@method" "@target-uri" "content-digest" "upload-offset" "mlp-reservation");created=1783168260;keyid="bdyqnbqeil3qzkhtocxe77a7j5qactmknmkncicd6k2glhgyrbvnzs5a";alg="ed25519"
```

*(the keyid is the TV-001 `sn`/`bs` kid; the JSON file is the
copy-paste-exact source — this rendering wraps long lines)*

The server accepts (offset → 20, durable checkpoint written), but the 204
is lost in transit — the pusher cannot distinguish "not applied" from
"reply lost", which is precisely why the procedure of §8.7 exists.

**Step 2 — HEAD** (`created=1783168265`, bodiless component set) →
`200`, `Upload-Offset: 20`, `Upload-Length: 36`,
`Upload-Expires: Tue, 07 Jul 2026 12:30:00 GMT`. The pusher learns the
checkpoint: 20 bytes are safe; nothing is re-sent.

**Step 3 — PATCH bytes 20–35** (`created=1783168270`,
`Content-Digest: sha-256=:jSCnywBz96P6mkQJqDHYTuicZ0YjE8qX4RZCIlUeRP0=:`)
→ offset reaches `Upload-Length`; finalized BLAKE3 equals
`urn:mlet:bdyqbcgucl4thqvg5tqzv66l4q6myvy3j647jojvokzpw2c4ohcctu6y`;
response `204` with `Upload-Offset: 36`, `MLP-Object-State: verified`.
The object is live; the token is consumed; `target.example` may now
answer `have` for this URN (to Tier 1, per D-29).

*Conformance uses (Annex C): RFC 9421 base construction for both request
shapes against known keys; the checkpoint/rollback invariant
(digest-mismatch truncation); offset realignment after lost replies;
completion verification and token consumption; `hash-mismatch` reset; the
distinction table of §8.5 exercised failure by failure.*

### 8.9 Verified-streaming push: mlp-bao (MEP-003)

The URN's BLAKE3 root already commits to the object's entire Merkle
tree; the plain §8.4 pipeline uses that commitment only at completion.
When the **receiving** domain advertises the `bao-stream/1` capability
(§5.2), a pusher MAY instead send the object as its combined encoding
(Annex D): `Content-Type: application/mlp-bao`, with `Upload-Length`,
`Upload-Offset`, and the checkpoint/rollback machinery of §8.4 all
measured in **encoded-stream bytes** (the encoded length is a
deterministic function of the content length; Annex D). A pusher MUST
NOT send `application/mlp-bao` to a resource whose receiving domain
does not advertise `bao-stream/1`; such a request fails 415.

The receiving BS verifies incrementally: each parent node and each
chunk group is checked against its expected chaining value the moment
it is complete (Annex D), and the final group's verification **is** the
§8.4 URN comparison — there is no separate completion hash. The BS
MUST reject a non-verifying node or group with 422 `bao-verify-failed`
and reset the resource to offset 0: like `hash-mismatch`, this is the
*object* failing its URN — the source is wrong — merely detected at
the first bad 16 KiB instead of after the full transfer. The
`digest-mismatch` taxon (§8.4, this request corrupted in transit)
applies unchanged and remains retryable at the same offset.

Everything else is untouched: the Reservation, the token, headers,
segments, resumption (§8.7 checkpoints hold encoded offsets), and the
completion effects (live, `have`-answerable, token consumed). A
delegated fulfillment push (§9.4) uses this section under the same
rule — the requester whose BS ingests is the party whose capability
advertisement governs.

## 9. Forwarding and Delegation

### 9.1 Overview

Forwarding re-dispatches a received Medialet — byte-identical, Author
Signature intact (D-02) — inside a fresh Envelope (§3.4.2). What
distinguishes the two frozen modes (D-24) is who supplies the bytes:

- **Delegated fulfillment**: the forwarder relays pointers only; a
  downstream recipient's grant is fulfilled by an upstream custody holder
  pushing directly to the recipient's BS. Default for aliases,
  auto-forwards, and personal forwards.
- **Custody forwarding**: the forwarder first takes the bytes into its own
  BS and becomes the fulfillment source. Default for list-style
  redistribution; exposed in clients as "private forward."

**Topology resolution** (D-81, resolving D-23's ambiguity). Stage 1
described a downstream grant as traveling "back along the hop chain,"
which admits two readings: a relayed multi-hop protocol, or direct
presentation of the chain to a chosen source. This specification adopts
**direct-to-source**: the requesting SN contacts fulfillment sources
directly, in preference order, presenting the relevant Hop Attestation as
its credential. Chain-walking is rejected because it would hold a
recipient's download hostage to every intermediate server's availability —
intolerable for relays that are "just plumbing" (D-24) — and would demand
a relayed-request sub-protocol the capability design makes unnecessary.

The uniformity that makes this work: **every legitimate fulfillment source
validates the same way — against its own dispatch records** — because
every source is, by definition, a domain that dispatched an Envelope
somewhere in the chain. The requester presents the attestation of whichever
Envelope that source itself signed; the source needs nothing from anyone
else to decide.

### 9.2 Constructing a forwarded dispatch

Mechanics are §3.4.2 (chain append, fresh routing fields, Medialet
unchanged); this section adds duties:

1. **Chain integrity** (D-84). The forwarder MUST carry the complete
   received chain — the received Envelope's own attestation appended to
   all prior attestations, none omitted, reordered, or altered. Provenance
   is truthful or the dispatch is non-conformant. Privacy *from the
   origin* is achieved by choosing custody mode (§9.7) — by not exercising
   delegation — never by falsifying the chain.
2. **`fulfillment_sources`** (D-50, rules completed here): a delegating
   forwarder MUST list at least the custody-holding sources it knows, the
   root origin at minimum; a custody forwarder SHOULD list itself first
   (nearest hop) and MAY list upstream holders as fallback. Every listed
   domain MUST be a chain member — the `origin` of the dispatched Envelope
   or of a Hop Attestation in it; receivers MUST ignore listed sources
   that are not (D-81), which forecloses directing recipients' requests at
   arbitrary third parties.
3. **`forwarded_by`** per D-50: RECOMMENDED on personal forwards (it is
   the "via Bob" UX), omittable for forwarder privacy.
4. **Loop prevention** per D-51 applies to every automatic re-dispatch.

### 9.3 The requester flow

When a recipient's accept action targets deferred Media on a *forwarded*
Envelope (no local `defer → grant` upgrade is possible — the enveloping
domain holds nothing), the recipient's SN:

1. Builds the candidate list: the received `fulfillment_sources` in order,
   defaulting to `[origin of the received Envelope]`; non-chain-member
   entries discarded (§9.2).
2. For each candidate `S`, selects the credential: the Hop Attestation
   whose `origin` equals `S` — where `S` is the received Envelope's own
   origin, the attestation is constructed from that Envelope's identifying
   fields and its Hop Signature, exactly as a forwarder would (§3.4.2).
   The Delivery Record retains everything needed (D-53).
3. Discovers `S` (Section 5) and POSTs a signed delegation request
   (§9.4) minting Reservations for its **own** BS.
4. On refusal or timeout, proceeds to the next candidate. When all fail,
   the client surfaces the graceful terminal state: the object is
   unavailable — "request a resend" (D-23), rendered per §10.

Pushes that follow are ordinary Section 8 transfers; the requester's BS
verifies as always — a delegated push is distinguishable from a direct one
by nothing but the pusher's identity.

### 9.4 The delegation request

**Document.** The fourth signature label, `delegation/1` (registry entry
per D-64; anticipated there), signed by an `sn`-role key of the requesting
domain. Payload (D-82):

| Member | Type | Req. | Definition |
|---|---|---|---|
| `mlp` | string | REQUIRED | Protocol version. |
| `request_id` | string | REQUIRED | Medialet-ID grammar; UUIDv7 RECOMMENDED. Sources SHOULD deduplicate on (`requester`, `request_id`) — replays answer with the prior response and consume no budget. |
| `created` | string | REQUIRED | RFC 3339 UTC; subject to the ±48 h skew window. |
| `requester` | string | REQUIRED | The requesting domain; the signature verifies against its `sn` keys; Reservations herein point at its BS. |
| `root` | object | REQUIRED | The Hop Attestation being exercised — the credential (§9.3 step 2). |
| `medialet_ca` | string | REQUIRED | Content address (§3.3.3) of the Signed Medialet the request concerns. |
| `media` | array | REQUIRED | Entries `{ "urn": <string>, "reservation": <Reservation, §7.5> }` — the ingesting side mints Reservations here exactly as a Target SN does in a verdict: **Reservations are always minted by the party whose BS will ingest** (D-82). |

**Endpoint.** `POST {sn}/fulfill` with `application/mlp-delegation+json` —
extending the D-68 API surface to four endpoints and the media-type set
accordingly (D-82).

**Response** (D-83): `200` with an **unsigned** JSON body
`{ "media": [ { "urn": …, "status": "will-push" / "refused",
"reason": <code, with refused> } ] }`, or a problem response for
request-level failures (`malformed`, `signature-invalid`,
`unknown-envelope`, `rate-limited`). The response is deliberately unsigned:
the meaningful, verifiable act is the push itself, which arrives minutes
later under the source's `bs` key, per-request signed and content-verified
(Sections 6.6, 8). Signing the promise would add ceremony without a threat
addressed; integrity of the response rides on TLS to an endpoint the
requester discovered authoritatively.

### 9.5 Source-side validation

A source receiving a delegation request validates, in order (failures
answer the codes shown):

1. Schema, version, skew, signature against `requester`'s `sn` keys via
   Discovery (`malformed`, `version-unsupported`, `timestamp-skew`,
   `signature-invalid`).
2. `root.origin` equals the source's own domain, and `root.envelope_id`
   names a dispatch in its records whose recorded Hop Signature equals
   `root.sig` (`unknown-envelope` otherwise). The source stored what it
   signed (D-51); byte comparison suffices, re-verification against its
   own key is equivalent.
3. The recorded dispatch's Signed Medialet content address equals
   `medialet_ca` — else **`medialet-mismatch`** (registry addition,
   D-83): an attestation spliced onto foreign content is an alarm, not a
   miss, and gets its own code.
4. Per requested URN: it appears in that Medialet's Manifest; the current
   time is within its `available-until` and the object is still held
   (**`not-available`**, registry addition, otherwise — the Stage 1
   "sender no longer offers this file"); the Reservation's `max_size`
   equals the Manifest size (`malformed`); the delegation budget for
   (`envelope_id`, `urn`) is not exhausted (`delegation-budget`, reserved
   in §7.8 for exactly this moment).
5. Local policy MAY refuse (`policy`) — a source is never obligated to
   fulfill.

A source honoring requests as a custody holder is bound by the `until`
**it itself declared** in its dispatch (validated against its own
records); a root origin remains bound by its own Manifest
`available_until`. **No party's declaration ever extends another
party's obligations** (MEP-001).

**Budget accounting** (D-83): per (root `envelope_id`, `urn`);
default **10** (D-23); *accepted* requests consume budget at acceptance;
a source MAY refund budget when a reservation expires unused. Exhaustion
answers `delegation-budget`, upon which the requester falls through to the
next candidate source or the graceful resend state.

Accepted entries are handed to the source's own BS for pushing — an
intra-domain act, out of scope per D-79. Pushers apply D-72 connection
safety to the requester-supplied `target_url`, which is remote-controlled
input exactly like any Reservation's.

### 9.6 Security and privacy properties

Stated without euphemism, for §12 to inherit:

- **The credential is possession.** A root attestation functions as a
  bearer capability. Its possessor set is: servers on the dissemination
  path of that dispatch (the origin's counterparty and every domain
  legitimately forwarded to, transitively). That is precisely the set that
  could have received the content by ordinary forwarding anyway; delegation
  changes *who ships the bytes*, not *who can obtain them*. The budget
  (D-23) bounds total origin-side cost per object per dispatch.
- **Intermediate hops are unverifiable by design** (D-51): the source
  validates only its own attestation and its own records. The privacy of
  prior recipient sets is bought at exactly this price, openly.
- **What the origin learns** (the D-23 trade, restated at its wire
  location): the requesting *domain*, the object, and timing — a
  downstream forwarding event became visible. It does not learn mailbox
  identities beyond what `envelope_to` of its own dispatch already told
  it, nor intermediate recipients (attestations carry domains and
  envelope identities, never recipient lists, D-51). The escape remains
  custody forwarding (§9.7).
- **A hostile chain member** can: burn an object's delegation budget with
  self-serving requests (bounded nuisance; later requesters degrade to
  resend); direct pushes only at *its own* BS (Reservations name the
  requester's infrastructure and are signed by the requester); and replay
  requests fruitlessly (request dedup, single-use tokens). It cannot
  splice attestations onto other content (`medialet-mismatch`), forge
  another domain's request (signatures), or extend availability past the
  sender's promise (`available-until` is checked from the source's own
  records, not from requester input).

### 9.7 Custody forwarding and redistribution

The custody forwarder upgrades its own deferred verdicts, completes
ingestion and verification, then dispatches with itself as the leading
fulfillment source — it SHOULD NOT dispatch a custody-mode forward before
its objects are live (D-84), so that its `will-push` answers are promises
it can already keep. List-style redistribution defaults to custody (D-24):
the party multiplying the audience carries the cost, and content-address
deduplication caps the fan-out at one transfer per recipient *domain*
(`have` short-circuits repeats). "Private forward" in clients is custody
mode plus, at the forwarder's option, an omitted `forwarded_by` — the
chain itself is never falsified (§9.2). Nearest-hop preference (D-24)
stands for the requester: the closest custody holder leaks least and holds
freshest availability.

### 9.8 Worked example (informative): Test Vector TV-004

TV-004 (`mlp-tv-004.json`) extends the fixture family to three domains:
`novak@target.example` auto-forwards the TV-001 dispatch to
`carol@final.example` at `10:00:07Z` — *before* his own `12:30Z`
acceptance (TV-002), so the relay holds no custody and fulfillment
delegates to the root. `final.example` signs under RFC 8032 Ed25519
TEST 1024 (public `278117fc…1d426e`, asserted in the generator):

```
final kid    bdyqcchdbohwznvk63ujxoooxlrwyjlwzog4ng4ek6wuajtoqpuajolq
final key    b5uaspaix7qkey4rub5t5b4rrn2byntx7x4vsikgjyup667czp4oue3q
```

**The forwarded Envelope** (1,669 canonical bytes; Hop Signature by
`target.example`'s TEST 3 key at `10:00:07Z`,
`value: I2KM6C7gJF1QcboTSbudVWTxZGhFSc53ZcH1LQUjCLpwmymjMI1xgAIFDCtE0X4GQMIjJEkhA5ZfSy_-kP5nDw`):

```json
{
  "mlp": "0.1",
  "envelope_id": "019f2c92-3458-7ba2-9bec-0190697bca43",
  "created": "2026-07-04T10:00:07Z",
  "origin": "target.example",
  "envelope_to": [ "carol@final.example" ],
  "forwarded_by": "novak@target.example",
  "fulfillment_sources": [ { "domain": "origin.example" } ],
  "hops": [ {
    "origin": "origin.example",
    "envelope_id": "019f2c92-2c88-7c16-a1fe-4548abf07edd",
    "created": "2026-07-04T10:00:05Z",
    "kid": "bdyqnbqeil3qzkhtocxe77a7j5qactmknmkncicd6k2glhgyrbvnzs5a",
    "sig": "TiQzJ3TUxh0bpuBiLxzbOXp3Kp5WWfGR1MMDGMCWs0dG3RuFEwG3KjgWUOUwA9yLkJenGbwKwAPPR0bfjgigDQ"
  } ],
  "medialet": { …the TV-001 Signed Medialet, byte-identical… }
}
```

The Hop Attestation *is* TV-001's hop signature, verbatim — immutability
and chain-as-capability visible in one place.

**The delegation request** (1,095 canonical bytes; `delegation/1` by
`final.example` at `11:00:00Z`,
`value: -_AzAVW-LxopGXOJkli948hxAsvb5Qh3b-RZO6TpotHJLLhO8W-z-39kBgugx88jOM7z28QCZX2NcjM4MOiGCQ`):

```json
{
  "mlp": "0.1",
  "request_id": "019f2cc9-0780-796b-b6b9-f0bcd5f10c95",
  "created": "2026-07-04T11:00:00Z",
  "requester": "final.example",
  "root": { …the attestation above, verbatim… },
  "medialet_ca": "urn:mlet:bdyqhmtxg343efvdn34cvh4xacxbfa7keroljucjvcpvg63rtkvhmlqa",
  "media": [ {
    "urn": "urn:mlet:bdyqbcgucl4thqvg5tqzv66l4q6myvy3j647jojvokzpw2c4ohcctu6y",
    "reservation": {
      "urn": "urn:mlet:bdyqbcgucl4thqvg5tqzv66l4q6myvy3j647jojvokzpw2c4ohcctu6y",
      "max_size": 36,
      "target_url": "https://bs.final.example/ingest/7b236472a18b169b",
      "token": "SG0L0KEh3gj7GvsW7zAuzIrG2CkvCtMH",
      "expires": "2026-07-07T11:00:00Z"
    }
  } ]
}
```

`origin.example` validates per §9.5 — its own envelope, its own recorded
signature, matching content address, inside the `2026-07-11` availability
window, budget 1 of 10 — and answers
`{ "media": [ { "urn": "…", "status": "will-push" } ] }`. The push that
follows is TV-003's shape aimed at `bs.final.example`.

*Conformance uses (Annex C): chain construction and integrity checks on
forward; credential selection per source; `delegation/1` generation and
verification; the §9.5 validation ladder failure by failure
(`unknown-envelope`, `medialet-mismatch`, `not-available`,
`delegation-budget`); request deduplication; budget consumption and
refund.*

## 10. Content Resolution and Retention

### 10.1 Overview and scope

This section defines what happens to Media after negotiation and transfer
have done their work: how a recipient's domain answers "where is this
object and what may I do with it," how long anything lives, and what
remains when it doesn't.

Scope follows the rule established at D-68/D-79 (D-86): resolution is
**entirely intra-domain** — a recipient resolves only against their own
home BS (D-28) — so the **semantics below are normative** (federation
behavior depends on them: `have`-answerability, quota enforcement,
delegation availability, tombstone honesty), while the client-facing API
**shape** is deployment freedom, sketched informatively in §10.8.

### 10.2 Objects and references

Retention state lives in two layers (D-87):

**Objects** — domain-level, content-addressed, in the BS. Three states:
`absent`; `pending` (an unexpired Reservation exists; any partial sits in
quarantine, invisible and never `have`-answerable, §8.6); `live` (verified
against its URN; eligible to satisfy resolution and — subject to D-29
masking — `have` verdicts). Objects carry no ownership; ownership lives in
references.

**References** — per-mailbox, in the SN. Every Manifest entry of every
delivered Medialet creates one reference per local recipient mailbox.
References also arise on the *sending* side: each dispatched Medialet's
Manifest entries create outbound `promised` references binding the sender
to its `available-until` declarations (§10.5). All user-visible questions
— can I open this, can I still accept this, why is this gone — are
questions about references, never about raw objects.

### 10.3 The reference state machine

Inbound reference states and their meaning:

| State | Meaning |
|---|---|
| `offered` | Verdict was `defer`; the object is not local; acceptance remains possible within the sender's window (or via delegation, §9). Costs the recipient nothing. |
| `expected` | A grant or delegation request is outstanding; transfer not yet complete. |
| `available` | The object is `live` locally and reachable by this mailbox. |
| `pinned` | `available` plus retention protection (D-21). |
| `unavailable` | Terminal, with a recorded `cause` (below). Rendered as a tombstone. |

Normative transitions — anything not listed is forbidden:

| From | To | Trigger |
|---|---|---|
| `offered` | `expected` | Recipient accepts → `defer → grant` upgrade (§7.6) or delegation request (§9.3). |
| `offered` | `unavailable(expired-remote)` | The **effective offer deadline** passes unaccepted — the latest of the Manifest `available_until` and the `until` of every listed fulfillment source covering the URN (MEP-001). Absent any `until`, the Manifest value governs. |
| `offered` | `unavailable(declined)` | Recipient declines, or policy `deny` supersedes. |
| `expected` | `available` | Verified ingest completes (§8.4), or the object was already `live` (`have`). |
| `expected` | `offered` | The Reservation expired unused and the sender window still stands; otherwise `unavailable(expired-remote)`. |
| `expected` | `unavailable(failed)` | Transfer terminally failed (`hash-mismatch` unresolved, all fulfillment sources exhausted, §9.3). |
| `available` | `pinned` / back | Owner pins / unpins. |
| `available` | `unavailable(expired-local)` | Local garbage collection (§10.5). |
| `available`, `pinned` | `unavailable(deleted)` | Owner deletion — a pin protects against GC, never against its owner. |

`unavailable` is terminal **within the reference** (D-87): recovery is a
new dispatch ("request a resend"), which creates new references — the old
record honestly preserves what happened.

### 10.4 Tombstones

The `unavailable` record MUST retain: `urn`, declared `size`, `type`,
`name` (when the Manifest carried one), the `cause`
(`expired-remote` / `expired-local` / `declined` / `failed` / `deleted`),
and the transition timestamp (D-87). Clients MUST render tombstones
honestly — the Medialet still displays, each expired object showing its
metadata and unavailability, never pretending the message was text-only
(D-21). Section 11 binds this duty into Body rendering.

### 10.5 Retention, pinning, and garbage collection

Invariants (D-88):

1. An object MUST be retained while any local reference to it is
   `pinned`.
2. GC MAY collect a `live` object only when no local reference is
   `pinned`; collection flips every local `available` reference to
   `unavailable(expired-local)` atomically with the object's removal.
   Which unpinned objects to collect, and when, is operator policy (quota
   pressure, age, class) — the protocol constrains only the invariants.
3. Objects with only `unavailable` references are immediately
   collectable.
4. Quarantined partials are collected at Reservation expiry (§8.6),
   independent of this machinery.

**Sender-side promises.** Outbound `promised` references bind the origin
through the latest outstanding `available-until` per object; senders
SHOULD retain accordingly (a promise, D-19 — but disks die, so the
protocol demands honesty rather than heroics: a source that no longer
holds an object answers `not-available`, §9.5, and the recipient degrades
gracefully to resend). A `promised` reference expires when its window
passes; an object with neither local pins, live promises, nor available
references is collectable on the sending side too. Delegation (§9)
extends no window: budget and `available-until` are checked against the
sender's own records at request time.

### 10.6 Quota accounting

RECOMMENDED defaults, operator-variable (D-89): `available` and `pinned`
references charge their object's full size to their mailbox — per
mailbox, even when domain-level deduplication stores one copy, because
quota is a per-user policy lever while dedup is the operator's disk
economics; `expected` references hold headroom for their declared size
(D-20's pending-reservation accounting at its storage location);
`offered` references and tombstones cost effectively nothing. That last
line is load-bearing: it is the "junk weighs kilobytes" property (D-15,
D-19) surviving into the retention layer — a mailbox full of unaccepted
strangers' offers consumes no meaningful storage.

### 10.7 Resolution semantics and access control

Given an authenticated mailbox owner and a URN, the home side MUST answer
from the owner's references (D-90):

- `available` / `pinned` → the verified bytes (or a ranged portion), plus
  reference metadata.
- `offered` → the accept affordance's substance: declared size, type,
  name, the `available_until` deadline, and whether fulfillment is local
  upgrade or delegation.
- `expected` → transfer status (offset progress is a quality-of-
  implementation nicety).
- `unavailable` → the tombstone record.
- No reference in this mailbox → **absent**, indistinguishable from a URN
  the domain has never seen.

That last rule is the access model (D-90): **capability is mailbox
membership; URNs are not secrets and confer nothing.** Its corollary
closes an oracle: domain-level deduplication must not be probeable by the
domain's own users — resolution answers `absent` for URNs outside the
caller's references *regardless of domain-level presence*, or the D-29
side channel, masked against remote strangers, would reopen from the
inside.

`have`-answerability, restated at its storage location: an object
satisfies `have` when `live` at domain scope, disclosed per D-29 masking
(Tier 1 visible, otherwise masked as `grant` with internal discard);
operators choosing per-mailbox dedup scope answer `have` only when the evaluated recipient's own references
include the object; per-store scope (partitioned deployments, Annex
B.6) confines deduplication within one BS instance (D-106).

### 10.8 Informative sketch: a resolution interface

Non-normative; the client↔home-side API remains a product surface (D-68)
pending a companion specification. The reference implementation intends:

```
GET {bs}/o/{urn}            owner-authenticated
  200  bytes (Content-Type from the Manifest; Range honored)
  202  {"state":"offered"|"expected", ...reference metadata...}
  410  {"state":"unavailable","cause":...,...tombstone record...}
  404  absent
POST {bs}/o/{urn}/pin | /unpin | /accept | /decline | /delete
```

Any shape satisfying §10.7's semantics is conformant; this sketch exists
so independent implementations converge by choice rather than diverge by
accident.

### 10.9 Conformance notes (Annex C material)

Behavioral cases, no fixture: every legal transition of §10.3 and
rejection of every illegal one (notably `unavailable → *` and
`pinned → unavailable(expired-local)`); GC invariant 1–3 enforcement under
multi-mailbox reference mixes (pinned-elsewhere blocks collection;
collection atomically tombstones all `available` references); quota
accounting per §10.6 including headroom holds and their release;
tombstone record completeness; the §10.7 absent-vs-present
indistinguishability probe (a second mailbox's URN resolves `404`
despite domain-level `live` state); sender-side `not-available` after
window expiry against a live delegation request.

## 11. The mlp-html/1 Content Profile

### 11.1 Design rule

The Body is a document, not an application (D-31). Everything below
follows from that sentence, enforced as an allowlist: what is not
explicitly permitted does not exist in `mlp-html/1`. Rendering a
conformant Body performs zero outbound requests, executes zero code, and
loads zero resources except Manifest-listed objects resolved through the
recipient's own home BS (Section 10).

### 11.2 Elements

**Permitted elements** and their element-specific attributes (D-91):

| Group | Elements | Element-specific attributes |
|---|---|---|
| Blocks | `p` `br` `hr` `h1`–`h6` `blockquote` `pre` `code` `div` | — |
| Lists | `ul` `li` `dl` `dt` `dd` | `ol`: `start` |
| Inline semantics | `em` `strong` `b` `i` `u` `s` `sub` `sup` `mark` `small` `q` `abbr` `dfn` `kbd` `samp` `var` `del` `ins` `wbr` `span` | `time`: `datetime` |
| Tables | `table` `caption` `thead` `tbody` `tfoot` `tr` | `th`: `colspan` `rowspan` `scope`; `td`: `colspan` `rowspan` |
| Links | `a` | `href` (§11.4) |
| Embeds | `img` `video` `audio` `source` `figure` `figcaption` | `img`: `src` `alt` `width` `height`; `video`: `src` `poster` `width` `height`; `audio`: `src`; `source`: `src` `type` |

**Global attributes** on any permitted element: `title`, `dir`
(`ltr`/`rtl`/`auto`), `lang` (BCP-47-shaped, ≤35 chars), `class`
(word characters, spaces, hyphens; ≤256 chars), `id` (pattern
`[A-Za-z][A-Za-z0-9_-]*`), `style` (§11.3). The `name` attribute is
permitted nowhere (DOM clobbering). Integer attributes (`width`,
`height`, `colspan`, `rowspan`, `start`) are 1–6 digit unsigned decimals.

**Removal is two-tier** (D-91). Elements on the **drop list** are removed
*with their entire subtree* — their content is code, controls, or foreign
markup, not prose: `script` `style` `iframe` `frame` `frameset` `object`
`embed` `applet` `form` `input` `button` `textarea` `select` `option`
`optgroup` `label` `fieldset` `legend` `template` `svg` `math` `link`
`meta` `base` `noscript` `slot` `canvas` `dialog` `map` `area`
`marquee`. Any other non-permitted element (`article`, `section`,
`font`, vendor inventions…) is **unwrapped**: the element vanishes, its
children survive. Comments, processing instructions, and doctypes are
removed. Client-side media presentation (controls, autoplay policy)
belongs to the client: presentation attributes such as `autoplay`,
`loop`, `preload`, and `controls` are stripped, and clients MUST present
playable media with user-facing controls and MUST NOT autoplay.

### 11.3 The style attribute

CSS exists only as the `style` attribute — there is no `style` element,
no selectors, no at-rules (D-93). A declaration survives sanitization iff
its property is allowlisted and its value matches the profile grammar.

**Properties:** `color` `background-color` `font-size` `font-weight`
`font-style` `font-family` `text-align` `text-decoration` `line-height`
`letter-spacing` `margin`(`-top/right/bottom/left`)
`padding`(`-top/right/bottom/left`) `border`(`-top/right/bottom/left`)
`border-width` `border-style` `border-color` `border-radius` `width`
`max-width` `height` `max-height` `display` `flex-direction`
`justify-content` `align-items` `gap` `vertical-align` `white-space`
`overflow-wrap` `word-break` `list-style-type` `object-fit`.

**Value grammar — the load-bearing rule:** a value consists solely of
letters, digits, `#`, `%`, `.`, `,`, hyphens, and whitespace. **No
functional notation, no parentheses, no quotes, no slashes, no `!`.**
This single machine-checkable constraint eliminates `url()` (resource
loading via CSS), `expression()`, `calc()`/`var()`/`attr()`, quoted-string
smuggling, and `!important` in one stroke. Consequences accepted
knowingly: colors are hex or named only (no `rgb()`), font families are
unquoted only. Additionally, `display` values are restricted to `block`,
`inline`, `inline-block`, `flex` — `none` is excluded, as are the
properties `position`, `visibility`, and `opacity` (overlay and
content-hiding vectors). Style attributes exceeding 2,048 characters are
dropped whole.

**Honesty note** (D-93): content-hiding is reduced, not solved —
same-color-on-background text and kin remain expressible. This residual
is classifier material (the D-21 quarantine hook sees what humans may
not), and the spec says so rather than pretending.

### 11.4 URLs and references

(D-92.) `href` on `a` admits exactly: `https:` URLs; `mailto:` URLs;
`urn:mlet:` references **present in the Manifest**; and intra-Medialet
fragments `#id` matching the `id` pattern. Embed sources (`src` on
`img`/`video`/`audio`/`source`, `poster` on `video`) admit **only**
Manifest-listed `urn:mlet:` references. Everything else — `http:`,
`javascript:`, `data:`, `blob:`, `file:`, protocol-relative, and
*relative URLs of any kind* (a Medialet has no base URL; relativity is
meaningless) — is invalid.

Enforcement (§3.2.3's promised location): an embed element whose `src`
is invalid or absent from the Manifest is removed entirely (a broken
embed is noise); an `a` with an invalid `href` loses the attribute, its
text surviving. `img` elements always carry `alt` after sanitization
(inserted empty when missing). Clients MUST open external links with
`noopener`/`noreferrer` semantics, SHOULD disclose the destination on
or before activation, and resolve `urn:mlet:` links through Section 10 —
deliberate navigation is consent (D-31); rendering is not.

### 11.5 The sanitization pipeline

**Immutability clarification** (D-94, resolving the apparent D-28/D-31
tension). Ingest sanitization NEVER modifies the Signed Medialet: it
**derives the render form**, a receiver-local artifact stored with or
alongside the Delivery Record. The verbatim signed artifact remains what
is stored, verified, content-addressed, and — always — forwarded.
Clients render the derived form and MUST sanitize again at render time
(defense in depth against a compromised or buggy home server; D-31's
dual-sanitization duty, now with both artifacts named).

**Normative algorithm** (D-94):

1. Parse the Body with a spec-compliant **HTML5 parser** in body-fragment
   context. Regex-based or SGML-era processing is non-conformant — HTML5
   tree construction is the mXSS defense's foundation.
2. Walk the tree depth-first: remove comments/PIs/doctypes; drop-list
   elements decompose with their subtrees; process children before
   unwrapping non-permitted elements; filter attributes of permitted
   elements per §§11.2–11.4.
3. Serialize per HTML5 fragment-serialization rules.
4. **Idempotence is REQUIRED**: sanitize(sanitize(B)) MUST equal
   sanitize(B) as parsed trees. Implementations SHOULD verify the
   fixpoint (re-parse own output, compare trees) and degrade to step 6 on
   mismatch — a serializer/parser disagreement is exactly the mutation-XSS
   seam, and the response to finding one is retreat, not hope.
5. **Complexity caps**: tree depth ≤ 64, node count ≤ 16,384 (the 256 KiB
   Envelope cap bounds raw size transitively, §3.2.3).
6. **Degradation**: a Body violating caps or defeating the pipeline is
   reduced to its derived text rendering (§11.6) — delivery proceeds, the
   content survives as text, nothing executable survives at all. No new
   rejection path exists; a hostile Body earns a boring one.

**Conformance comparison is parsed-tree equality, never byte equality**
(D-94): serializers legitimately differ in attribute order and
void-element syntax (TV-005's benign case demonstrates both), and
specifying bytes would manufacture false failures.

### 11.6 Derived text rendering

A deterministic reference algorithm (SHOULD-grade, D-95), serving as the
cap-violation fallback, an accessibility surface, preview material, and
the quarantine classifier's input (D-21): element text content with
block-level elements emitting line breaks; `img` as `[image: <alt>]`;
`a` as `text <destination>` for external links and `text` alone for URN
and fragment links; table rows line-per-row, cells tab-separated; list
items prefixed `- ` (`n.` for `ol`, honoring `start`). Clients MAY
improve on it; the reference exists so "the text version" means roughly
the same thing everywhere.

### 11.7 Client floor

Web clients MUST render Bodies inside an isolated DOM (shadow root or
sandboxed frame) under a strict CSP whose effect is: no script, no
external fetches of any kind, images and media exclusively from the
client's own home-BS origin, inline style attributes only. Native
clients provide the platform equivalent. Media previews and thumbnails
decode in sandboxed decoders (D-31 — decompression bombs and codec
exploits are a client-side concern the protocol flags but cannot solve).
The structural consequence stands as v1's quiet headline: a sender
learns nothing when a Medialet is opened, because opening one touches no
network the sender can observe (D-31, D-37).

### 11.8 Worked corpus (informative): Test Vector TV-005

TV-005 (`mlp-tv-005.json`, generator `mlp-tv-005-generator.py`) is a
14-case corpus of Bodies paired with sanitized outputs, **generated
mechanically by a prototype of the §11.5 algorithm** (html5lib tree
construction), idempotence machine-verified on every case; outputs are
canonical upon conformance-suite adoption (D-96). The Manifest for all
cases contains exactly the TV-001 object URN. Highlights:

| Case | Input (abbreviated) | Sanitized result |
|---|---|---|
| `script_drop` | `<p>before</p><script>alert(1)</script><p>after</p>` | `<p>before</p><p>after</p>` |
| `tracking_pixel` | `<img src="https://tracker.example/p.gif">` | *(removed)* |
| `js_href` | `<a href="javascript:alert(1)">click</a>` | `<a>click</a>` |
| `urn_not_in_manifest` | `<img src="urn:mlet:bdyqhpdu…">` | *(removed — §3.2.3)* |
| `css_url_smuggle` | `style="color:#333;background-color:url(https://x);margin-top:4px"` | `style="color:#333;margin-top:4px"` |
| `svg_mxss` | `<p>a</p><svg><circle r="4"/></svg><p>b</p>` | `<p>a</p><p>b</p>` |
| `position_overlay` | `style="position:fixed;top:0;color:#000"` | `style="color:#000"` |
| `display_none_hiding` | `<span style="display:none">hidden</span>` | `<span>hidden</span>` |
| `fragment_nav` | `<a href="#s2">` + `<h2 id="s2">` | *(preserved intact)* |
| `unwrap_unknown` | `<article><p>content</p></article>` | `<p>content</p>` |

*Conformance uses (Annex C): the full corpus under tree-equality
comparison; idempotence on every case plus fuzzed inputs; cap-violation
degradation to §11.6 text; alt insertion; the two-tier drop/unwrap
distinction; parser-compliance probes (markup that regex sanitizers
misparse).*

## 12. Security Considerations

### 12.1 Assets and adversaries

**Assets.** (1) Medialet authenticity and integrity — "this author wrote
these bytes." (2) Media integrity — "this blob is its URN." (3) Recipient
resources — storage, bandwidth, attention. (4) Content confidentiality.
(5) Metadata — who communicates with whom, when, how much. (6)
Availability.

**Adversaries.** (A1) a network attacker between nodes; (A2) a malicious
sender with a valid domain; (A3) a malicious or compromised *origin*
operator; (A4) a malicious or compromised *receiving* operator; (A5) a
malicious relay in a hop chain; (A6) a probing attacker using protocol
features as oracles; (A7) a bulk metadata observer.

### 12.2 The outbound-connection inventory

*(D-97, refining the Stage 1 single-sentence claim, which predated the
negotiation and delegation surfaces.)* A conformant MLP server initiates
outbound connections in exactly five classes, and no others:

| # | Connection | Constraint | Defined in |
|---|---|---|---|
| 1 | Discovery fetches | GET-only, hardened profile: 443, 64 KiB cap, ≤3 HTTPS redirects, special-purpose-IP blocking with resolve-then-pin | §5.4 |
| 2 | Dispatch POSTs | To SN endpoints obtained from authoritative Discovery only | §5, §7.3 |
| 3 | Verdict-update POSTs | To the already-signature-verified negotiation counterparty's discovered SN, referencing only material it dispatched | §7.6 |
| 4 | Delegation requests | To chain members' discovered SNs, credentialed by Hop Attestations | §9.3–9.4 |
| 5 | Reservation pushes | To `target_url`s from signed Reservations/requests, HTTPS-only, IP-safety per D-72, redirects refused | §7.5, §8 |

The claims that follow from the inventory, each auditable: **no server
ever pulls payload bytes from an untrusted origin** (the media path is
pure push, D-11); **no URL originating in message content is ever fetched
by any server** (the Body cannot cause a connection — §11.1, §11.4); and
**no connection is ever made to an unverified party except the hardened
discovery fetch itself** (which is the bootstrap and is constrained
accordingly).

### 12.3 Defended claims

| Threat | Mechanism | Sections |
|---|---|---|
| Wire tampering, downgrade (A1) | Mandatory HTTPS/WebPKI, no plaintext mode; per-document and per-request signatures | §5–§6, D-14 |
| Storage exhaustion by strangers (A2) | The reservation economy: no byte without a scoped, expiring, size-capped, token-bearing grant; junk weighs kilobytes | §7, §10.6, D-15/D-19 |
| Backscatter | Structurally impossible: rejection is synchronous; the only receiver-initiated contact targets the verified counterparty about its own dispatch | §7.3, §7.6, D-17 |
| Cross-domain author forgery (A3) | Domain-attested signatures; authoritative HTTPS key discovery; DNS-disagreement hard fail; exact-domain key authority | §5–§6, D-09/D-13/D-63 |
| Media substitution | Content addressing; verify-before-visible; quarantined partials | §8.4–8.6, D-25/D-27 |
| Recipient-set or content substitution on a signed dispatch | Hop Signature covers the Envelope transitively including the Medialet and `envelope_to` | §3.4.3, D-20 |
| Replay | Envelope/verdict/request identifiers with dedup scopes; ±48 h skew; single-use tokens; RFC 9421 `created` window; retry-vs-replay distinction | §3.4.4, §7.3, §8.2, §9.4, D-20/D-66/D-74 |
| SSRF (both directions) | Hardened discovery profile; pusher-side IP-safety on remote-supplied `target_url`s | §5.4, §7.5, D-11/D-72 |
| Dedup possession probing (A6) | `have` masking for non-correspondents; the internal oracle closed for a domain's own users | §7.5, §10.7, D-29/D-90 |
| Attestation splicing | `medialet_ca` binding checked against the source's own records — its own alarm code | §9.5, D-83 |
| Algorithm confusion | Signed `protected` blocks; label context matching; multicodec-bound kids | §6.2, §6.4, D-62/D-64 |
| Cross-protocol signature reuse | In-band domain-separation labels; dedicated-key recommendation | §6.4, D-44/D-63 |
| Content-borne code and tracking (mXSS, XSS, pixels) | The allowlist profile: HTML5-parser requirement, no-functional-notation CSS, URN-only embeds, dual sanitization, idempotence, degradation to text | §11, D-31/D-91–94 |
| DOM clobbering | `name` permitted nowhere; constrained `id`; isolated-DOM rendering floor | §11.2, §11.7 |

### 12.4 Malicious-operator walkthroughs

**Malicious origin (A3)** can: forge Medialets from *its own* users
(§12.5, the inherited DKIM limit, D-13/D-34); spam within receivers'
tiers and rate limits, burning its domain reputation as collateral; lie
in negotiation and verdict updates about its own dispatches. It cannot
forge other domains' users, alter a forwarded third-party Medialet
(Author Signature, D-02), push bytes that fail their URN (§8.4), or
extend a delegation past its own published promises — `available-until`
and budgets are checked against records it would only be corrupting for
itself (§9.5).

**Malicious receiver (A4)** reads its own users' mail (§12.6), can lie
to its users about verdicts, can silently drop, and can mint hostile
Reservations — which is why pushers treat `target_url` as attacker
input (D-72). It cannot alter received Medialets undetectably
(re-verification against stored bytes fails; the render form is derived,
never authoritative — §11.5), and a Reservation invites only bytes the
origin already offered.

**Malicious relay (A5)** — the postal sorting office: it can lose the
parcel, read the postcard, decline to forward, and observe everything
that transits it. It cannot rewrite the letter (immutability + Author
Signature), forge upstream hop signatures, or — under delegated
fulfillment — even touch the payload. Its genuine powers are bounded
nuisances: replaying old Medialets under fresh Envelopes (visible via
(author, id) dedup, D-46); burning an object's delegation budget with
self-serving requests, degrading later requesters to "request a resend"
(§9.6); omitting or falsifying `fulfillment_sources` — but never the
chain itself without becoming non-conformant and detectable at the
sources it misrepresents (§9.2, D-84); and directing delegated pushes
only at *its own* BS, since requests are signed and Reservations name
the requester's infrastructure (§9.6).

### 12.5 Key compromise, per role

| Stolen key | Attacker gains | Bounded by |
|---|---|---|
| `sn` | Forge Envelopes and hop signatures as the domain; mint verdicts and Reservations; sign delegation requests, harvesting content from the domain's dissemination paths to attacker infrastructure | Rotation within the 24 h cache ceiling (D-33); delegation budgets; harvest limited to what the domain could legitimately receive anyway (§9.6) |
| `bs` | Sign pushes | Content addressing caps damage at bandwidth waste: garbage cannot verify, and can never impersonate legitimate content (§8.4) |
| `author` | Forge the domain's users' mail — the worst case | Rotation; verification-at-ingest means past mail keeps its verdicts (D-32); the v2 personal-key profile is the designed escape (D-13/D-34) |

Revocation is honest-soft: removal from the Domain Document, effective
within the 24-hour cache ceiling, stated as a bound rather than hidden
(D-33, §6.3).

### 12.6 What v1 explicitly does not defend

The candor list, normative by decision (D-34), unchanged by Stage 2:

1. **No end-to-end confidentiality.** Operators see everything their
   servers touch, on both sides. Deliberate for v1 (zero client key
   management, D-13); a v2 E2E profile slot is reserved — blobs are
   already opaque content-addressed objects, so per-recipient HPKE
   changes what a URN names, not how bytes move.
2. **A domain can forge its own users** (DKIM's limit, inherited
   knowingly; §12.5).
3. **Metadata is visible to operators**, and delegation reveals
   downstream forwarding to the fulfillment source (§13.3). Traffic
   analysis (A7) is out of scope, as it is for email and nearly
   everything that is not a mixnet.
4. **No censorship resistance.** Availability is at operator mercy;
   federation's remedy is exit, not cryptography.

### 12.7 Resource exhaustion

Consolidated residue, each at its enforcement point: negotiation floods
die at rate limits before expensive work — and Ed25519 verification, the
costliest cheap-layer step, runs in tens of microseconds (§7.2, D-20);
reservation squatting is bounded by 72 h expiry, pending caps, and
headroom holds (§7.5, §10.6); slow-loris pushes meet minimum-throughput
termination with checkpoints preserved (§8.6); oversize lies meet the
mid-stream abort (§8.4); sanitizer complexity attacks meet the caps and
the text-extraction degradation — a hostile Body earns a boring one,
never a new failure mode (§11.5); delegation-budget burning is a bounded
nuisance with a graceful floor (§9.6); discovery hammering meets negative
caching (§5.5).

### 12.8 Cryptographic notes

Agility lives in the multiformats layer (multihash, multicodec) and the
`alg`/label fields — an algorithm retirement is a registry action, not a
namespace break (D-25, D-61–64). JCS risk is contained by construction:
the integers-only rule deletes number-formatting edge cases, and
byte-immutable storage plus verification-at-ingest means canonicalization
is a robustness layer, not the security backbone (D-43/D-44, §3.3.2).
No mechanism assumes Ed25519 signature uniqueness (§6.4). BLAKE3-256's
security margin answers collision concerns; multihash is the contingency
(D-25). Implementations use audited libraries and never implement
primitives (§6.4).

## 13. Privacy Considerations

### 13.1 The ledger

Who learns what, updated for the drafted protocol (Stage 2 additions
marked •):

| Party | Learns |
|---|---|
| Origin SN | Content; the full actual recipient set including Bcc; timing; • per-recipient verdicts and reason codes (§7.4); • acceptance events and their timing (§13.3) |
| Target SN | Content; sender; timing; the hop chain's domains and the optional `forwarded_by` mailbox |
| Each BS | Blob contents; the domain-level transfer graph |
| A relay | Everything transiting it — content and both endpoints of its hop |
| A fulfillment source (delegation) | The requesting *domain*, the object, timing — a downstream forwarding event (§9.6); never intermediate recipient lists (attestations carry domains and envelope identities only, D-51) |
| A discovery observer | That domain A resolved domain B around time T |
| A `resolve` endpoint (if enabled) | Compose-time interest of the querying domain in an address — rate-limited, minimal-response, disableable (D-60) |

No party learns more than its email-world analogue except delegation's
disclosure — new functionality email lacks, priced in disclosed metadata
(D-23) — and the acceptance-timing signal below.

### 13.2 Recipient-set privacy

Bcc exists as omission: no field ever stores it (D-03); per-Bcc
Envelopes never name two hidden recipients together; Hop Attestations
exclude recipient lists entirely, so co-recipients and Bcc structure
never travel downstream (D-51) — bought openly at the price that third
parties cannot verify prior hops (§9.6). Displayed recipients are
authored claims, actual recipients are routing, and their sanctioned
divergence is enumerated rather than denied (D-04). A recipient can
verify authorship and hop integrity, never "intendedness" — inherent to
Bcc existing at all.

### 13.3 Behavioral signals

*(D-98, documented here for the first time.)* Upgrading a deferred
object (§7.6) or exercising delegation (§9.3) necessarily reveals **the
act and time of acceptance** to the fulfillment source — bytes cannot
flow otherwise. This is the designed "files accepted" professional
status (D-37), and it is metadata the recipient's action emits; clients
SHOULD present the accept affordance so this is unsurprising ("download
from sender"). It is categorically distinct from read receipts, which
remain structurally impossible: *rendering* a Medialet touches no
network any sender can observe (§11.7, D-31) — opening, reading,
re-reading, and viewing already-transferred media emit nothing. The line
the protocol draws: transfers are visible acts, reading is not.

`forwarded_by` is an optional disclosure the forwarder controls (D-50).
Subaddress tags travel only where senders already wrote them.
Delivery Records never leave the receiving domain (D-04, D-53).

### 13.4 Deduplication and possession

The `have` side channel is masked to non-correspondents externally
(D-29, §7.5) and closed internally: a domain's own users cannot probe
domain-level presence, since resolution answers `absent` outside the
caller's own references (D-90, §10.7). Operators wanting stricter scoping run per-mailbox or per-store
dedup at a storage cost (§10.7, Annex B.6).

### 13.5 Data lifetime

The Medialet is permanent per mailbox policy; Media are quota-bound with
pin/GC and honest tombstones (D-21, §10). Recipient deletion is always
available and tombstoned; tombstone metadata (name, size, type, cause,
time) is the recipient's own record, purgeable by mailbox policy.
Sender-side `promised` records live through their declared windows and
then expire (§10.5). Verification verdicts outlive keys by design
(D-32) — an auditable past without eternal key archives.

### 13.6 Operator knobs for privacy-conscious deployments

Collected: disable `resolve` entirely (D-60); mask `have` universally or
scope dedup per-mailbox (D-29, §10.7); omit `forwarded_by` by policy
(D-50); prefer custody forwarding domain-wide, trading storage for
non-disclosure to origins (§9.7); shorten retention classes and Delivery
Record lifetimes (§13.5). None of these knobs affects interoperability;
all are local policy over normative mechanics.

## 14. Extensibility and Registries

### 14.1 Administration

All registries below are self-administered by the specification editor
under the MEP process (D-40): during MLP/0.x, every addition, change, or
deprecation requires an accepted MEP — the RFC 8126 "Specification
Required" analogue with the MEP as the specification (D-100). Entries are
never reused with changed semantics; retirement is by deprecation mark,
not deletion.

MLP's JSON documents deliberately have **no runtime member registry**:
the unknown-members-ignored rule (§2.3) is the wire-level extension
mechanism, and member additions are governed directly by this
specification via MEP (D-100).

### 14.2 The registries

**Signature labels** (§6.4; grammar `name "/" version`): `author/1`,
`hop/1`, `verdict/1`, `delegation/1`. A change to a label's payload
semantics is a new version of that label, never a mutation.

**Reason codes** (§7.8, §8.5, §9.5), consolidated — 28 initial entries:

| Category | Codes |
|---|---|
| Transport/validation | `malformed` `envelope-too-large` `version-unsupported` `signature-invalid` `timestamp-skew` `replay` `rate-limited` `discovery-failed` `unknown-envelope` `invalid-transition` |
| Recipient-level | `unknown-recipient` `mailbox-disabled` `policy` `suspected-junk` |
| Media-level | `pending-acceptance` `quota` `type-forbidden` `size-exceeds-policy` `hash-blocklist` `delegation-budget` |
| Transfer | `offset-mismatch` `digest-mismatch` `hash-mismatch` `reservation-expired` `reservation-invalid` `stalled` `bao-verify-failed` |
| Delegation | `not-available` `medialet-mismatch` |

**Capability tokens** (§5.2; grammar `name "/" version`):
`bao-stream/1` (MEP-003).

**Key roles** (§5.2, §6.3): `sn`, `bs`, `author`.

**Verdicts** (§7.4): message-level `accepted` / `rejected` /
`quarantined`; per-URN `grant` / `have` / `defer` / `deny`.

**Media types** (§7.2, §9.4): `application/mlp-envelope+json`,
`application/mlp-verdict+json`, `application/mlp-delegation+json`,
`application/mlp-bao` (§8.9, Annex D) —
unregistered with IANA at this time; standards-tree registration is
intended at the Independent-Submission stage (D-40), stated plainly
rather than implied (D-100).

**Body profiles** (§3.2.1, §11): `mlp-html/1`.

**MLP HTTP fields** (§8.2): `MLP-Reservation`, `MLP-Object-State` —
IANA HTTP Field Name registration likewise deferred and disclosed.

**DNS record parameters**: `_medialet` TXT — `v` (required, `MLP1`),
`url`; `_medialetkey` TXT — `v` (required, `MLP1`), `alg`, `key`,
`roles` (§5.3, §6.5).

**Tombstone causes** (§10.3): `expired-remote`, `expired-local`,
`declined`, `failed`, `deleted`.

**Multiformats profile** (the external multiformats tables are not
MLP's to administer; this registry records which codes MLP *requires or
permits*): multibase `b` (base32-lower, the only permitted base);
multihash `0x1e` BLAKE3-256 (mandatory-to-implement), `0x12` SHA2-256
(permitted for interop, D-25); multicodec `0xed01` ed25519-pub
(mandatory-to-implement, D-61).

### 14.3 Versioning policy

(D-101.) The wire version is the `mlp` member; peers interoperate on the
intersection of their Domain Documents' `mlp` arrays (§5.2).
**Additive, non-breaking** — permissible within a wire version: new
optional members (unknown-member tolerance), new registry entries via
MEP, new SHOULD-grade behaviors. **Breaking** — requiring a wire-version
bump: changed semantics or grammar of existing members, removed or
repurposed registry entries, changed signature payload construction
(alternatively expressed as a new label version where confined to one
document type). MLP/0.x is declared unstable: breaking changes may occur
between 0.x versions with MEP record; MLP/1.0 freezes under the D-40
exit criteria (two independent interoperable implementations passing the
conformance suite; an addressed external security audit; six months
without breaking wire changes).

**The deferred register** (carried from the Stage 1 Closing Document,
unmodified): E2E encryption profile (per-recipient HPKE over blobs);
personal client-held author keys; consent-based read receipts;
internationalized local parts; per-object parallel transfer (tus
concatenation / IETF resumable-uploads migration); mailing-list profile;
compliance verticals; Bao verified streaming.
---
## Annex A (informative): Guest Delivery

*(Commissioned by D-36: a product pattern of nodes and clients — never
part of core federation. This annex exists so independent
implementations converge on UX and security posture; nothing here is
required for protocol conformance.)*

**Trigger.** The sender addresses a recipient that is not a resolvable
MLP Address (Discovery fails, or the input is a bare email address). The
sender's node then delivers *locally*: no Envelope, no verdicts, no
federation.

**Recommended pattern.**

1. The Medialet is composed and signed exactly as normal — the artifact
   is a genuine Signed Medialet. (This is what makes claim conversion
   trivial later.)
2. The node mints a **capability URL**: HTTPS, containing an
   unguessable token of at least 128 bits of entropy in the path;
   optionally protected by a short **PIN** communicated out of band for
   sensitive deliveries; **expiring** in alignment with the Manifest's
   `available-until` windows; revocable by the sender.
3. The guest view renders the **sanitized render form** (§11.5) under
   the full client floor of §11.7 — the guest page is a Medialet client,
   not an exception to one. Downloads stream from the sender's own BS
   over HTTPS with Range support.
4. The recipient is notified by plain email. The notification SHOULD
   contain enough context to be verifiable — sender identity, subject,
   file names and sizes, the expiry date — never a bare link; guest
   delivery must not train recipients to click naked URLs
   (anti-phishing posture).
5. **Claim conversion** — the adoption funnel: the guest view offers
   "get a permanent address." Upon the recipient registering anywhere
   (the flagship or any provider), the sender's node re-dispatches **the
   same Signed Medialet** to the new Address as a genuine MLP dispatch —
   immutability means the artifact was federation-ready all along — and
   retires the guest link.

**Honest notes.** The notification email travels outside MLP's privacy
envelope: SMTP metadata semantics apply to it. Guest bandwidth is the
sender's operator's cost; operators meter accordingly. Access logging on
guest views is operator policy and SHOULD be disclosed in the operator's
terms; guest views SHOULD NOT include tracking beyond operational logs —
a protocol that structurally killed the tracking pixel (§11.7) should
not reintroduce it at its front door.

## Annex B (informative): Deployment Topologies

*(SN and BS are roles, not boxes — D-01; the intra-domain interface
between them is deliberately unspecified — D-79.)*

**B.1 Single binary.** One process implementing both roles against
SQLite — the reference implementation's default and the "personal mail
server" story: one static Go binary, one file of state, one domain.

**B.2 Split roles.** SN on a small VPS (signaling is kilobytes); BS on
or near the storage — a NAS at home, a machine with big disks. The
Domain Document advertises the SN; `target_url`s point wherever the BS
lives.

**B.3 Object-storage-backed BS.** The BS role as a thin layer fronting
S3-compatible storage: ingest maps tus PATCHes onto multipart uploads,
hasher-state checkpoints persist outside the object (a small database or
sidecar objects), serving is ranged reads. **One rule stated firmly even
in an informative annex: raw object storage is never the `target_url`.**
The reservation-enforcing layer — token validation, mid-stream size
abort, transactional verification (§8) — MUST front it; a bare bucket
URL would bypass every duty the BS role exists to perform.

**B.4 Hosted provider.** The customer domain serves (or redirects to)
its per-domain Domain Document at the provider (§5.2), optionally with
the DNS hint (§5.3); identity stays with the domain, operations with the
provider — the MX-record economics of email, reconstructed.

**B.5 Proxies and CDNs.** Reverse proxies and CDNs are unremarkable on
discovery and serving paths. Push ingest paths need streaming-friendly
configuration: request buffering limits above the deployment's PATCH
sizes, timeouts above its slow-link expectations, and no body
transformation.

**B.6 Partitioned storage.** (D-107.) A domain runs several BS
instances — by media type (photos here, video masters there), by
temperature (fast object storage for previews, NAS for archives), or by
theme (a healthcare store, a business store), each with its own
`bs`-role keys for per-store compromise isolation (§6.3). Externally
invisible by construction: only reservations disclose storage, and the
SN mints each one (D-10, D-105). Routing signals are
**recipient-controlled**, never sender-declared — a Manifest carries no
"theme," and letting strangers choose which compliance boundary their
bytes land in would be the wrong trust model. The signals that work:
the Manifest's declared media `type`; the subaddress tag the recipient
themselves issued (`igor+health@…` to the doctor, D-55); correspondent
policy (everything from `hospital.example` → the healthcare store),
riding the D-19 tier machinery; the accept-time decision point that
deferred transfer already provides (the accept affordance can carry a
store selector); and free post-hoc migration — URNs are
location-independent (D-28), so moving an object between one's own
stores repoints references and changes nothing observable. Honest
limits, per the D-34 discipline: the SN still sees all signaling —
bodies, subjects, manifests, correspondents — whichever store holds the
blobs; partitioning is at-rest separation, compliance boundary-drawing,
and blast-radius isolation, never confidentiality from one's own
operator (for a self-hosted single-user domain, the operator is you,
and the isolation is fully real). Strict isolation also implies
per-store deduplication scope (§10.7): the boundary is only as real as
the dedup that respects it.

## Annex C (informative): Conformance Overview

*(Commissioned by D-39/D-41: the conformance suite is a first-class
deliverable, and passing it is the objective test behind the
"Medialet-compatible" trademark claim.)*

**Vector families**, all machine-readable JSON with committed
generators, all values recomputable from RFC 8032 test keys:

| Family | Fixture | Exercises |
|---|---|---|
| 1 — Serialization & signing | TV-001 | JCS agreement; URN and content-address computation; kid self-verification (positive and corrupted); multicodec/`alg` cross-check; label context mismatch; cap accounting |
| 2 — Negotiation | TV-002 | `verdict/1` generation/verification; per-recipient and per-URN completeness; snapshot supersession; the transition table; retry idempotency |
| 3 — Transfer | TV-003 | RFC 9421 bases for both request shapes; checkpoint/rollback; resumption after lost replies; completion, token consumption; the §8.5 failure taxonomy |
| 4 — Forwarding & delegation | TV-004 | Chain construction and integrity; credential selection per source; the §9.5 validation ladder; budgets and refunds; request deduplication |
| 5 — Content profile | TV-005 | The sanitization corpus under parsed-tree equality; idempotence including fuzzed inputs; cap degradation; drop-vs-unwrap |
| 6 — Behavioral | *(harness, no fixed vectors)* | The §10.3 state machine and §10.9 probes; discovery precedence and hard-fail; hardened-fetch behavior against hostile endpoints; malformed-input corpora (grown continuously); throughput termination |

**Harness posture.** Implementations are tested black-box through their
server-to-server surfaces — the same interfaces peers see. The suite
quality bar, adopted as an aspiration to be enforced during Stage 4:
**every MUST in this specification maps to at least one test whose input
violates it** — a MUST without a failing-input test is a MUST on the
honor system.

**Compatibility claims.** "Medialet-compatible" is a trademark-governed
claim (D-39) whose objective content is: the implementation passes the
current conformance suite for the wire versions it advertises. Nothing
else confers or denies it.

## Annex D (normative): The application/mlp-bao Encoding

The encoding is the bao verified-streaming format applied to MLP's
existing BLAKE3 identifiers, profiled to one fixed geometry. It adds
nothing to the trust model: every chaining value it carries is already
committed to by the `urn:mlet:` root; the encoding merely transports
the tree so a receiver can verify incrementally.

### D.1 Geometry

The **chunk group** is **16,384 bytes** — 16 contiguous BLAKE3 chunks.
Encoders MUST emit full 16,384-byte groups except the final group,
which carries the remainder (an empty object is one empty group).
Rationale, recorded for the pin (D-273): parent overhead is 64 bytes
per ~16 KiB ≈ 0.4 % of the stream; the first verifiable rejection
point is 16 KiB; and 16 KiB divides the §8.6 segment grid exactly
(one 16 MiB segment = 1,024 groups).

### D.2 Chaining values

The **group CV** is the standard BLAKE3 subtree chaining value over
the group's chunks *at their original chunk counters* — the same
values the URN computation already produces internally. No flags
beyond BLAKE3's own; a group CV is never root-finalized unless the
whole object is a single group. The **parent node** is the 64-byte
concatenation `left CV || right CV`; its own CV is the BLAKE3 parent
compression of those bytes. Tree shape is BLAKE3's: the left subtree
spans the largest power of two of groups strictly less than the total.
The topmost value, root-finalized, MUST equal the URN's digest —
that equality is the object verification.

### D.3 Combined form

`encoding = length || tree`, where `length` is the **content** length
as an 8-byte little-endian unsigned integer, and `tree` serializes the
root subtree **pre-order**: a parent serializes as its 64-byte node
followed by its left then right subtrees; a leaf serializes as the
group's content bytes. Encoded size is therefore
`8 + 64·(groups − 1) + content_length`. A verifier consumes the stream
in the same order, checking each parent node against its expected CV
before descending and each group against its expected CV before
accepting its bytes; verifiers MUST NOT release or act on any byte of
a group that has not verified.

### D.4 Slice form

A slice for the content range `[offset, offset+len)` is the combined
form with every subtree that does not intersect the range omitted —
its CV is already present in its parent's node, which is retained.
Group alignment is inherent: the slice carries whole groups. Slices
answer ranged, seekable, verified reads. **Consumption surfaces are
deployment territory**: MLP has no cross-domain read (transfer is
pure push, D-11), so slices bind the client-API and guest read
surfaces, informative per D-68/D-79/D-86; the byte format defined
here is normative wherever the encoding is used.

### D.5 Conformance

TV-008 pins the encoding: a rule-generated object, its group and
parent chaining values, the full combined form and one boundary-
crossing slice (each pinned by BLAKE3 digest), and one corrupted
slice with its expected `bao-verify-failed` rejection point.
