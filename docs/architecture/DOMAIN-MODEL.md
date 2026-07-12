# Domain model — the entities and their loyalties

The protocol's nouns, as one picture. The split that everything else
follows from (D-01–D-04): the **Medialet** is the authored,
immutable, signed artifact; the **Envelope** is disposable routing
around it. Subject belongs to the author; `envelope_to` belongs to
the postal system; and Bcc exists nowhere as data (it is per-copy
Envelope omission).

```plantuml
@startuml domain-model
!theme plain
skinparam classAttributeIconSize 0
hide circle

class Medialet <<authored, immutable>> {
  content_address : urn:mlet (of canonical bytes)
  author : Address
  medialet_id : uuid  (author-scoped uniqueness)
  created : rfc3339
  subject?
  displayed_to[]  (honesty, not routing — D-03)
  in_reply_to? : content_address
  body : mlp-html/1
}

class ManifestEntry {
  urn : urn:mlet
  size : int
  type : mime
  name?  (display only, never a path)
  available_until : rfc3339  (the author's window, D-19)
  preview_of? : urn  (MEP-002)
}

class AuthorSignature {
  kid, created, value  (author/1 — survives forwarding)
}

class SignedEnvelope <<disposable routing>> {
  envelope_id : uuidv7
  created : rfc3339
  origin : domain
  envelope_to[] : Address  (single domain)
  forwarded_by? : Address
  fulfillment_sources[]?  (domain, urns?, until? — MEP-001)
  hops[] : HopAttestation
}

class HopSignature {
  kid, created, value  (hop/1 — current hop fully verified)
}

class Verdict <<signed, per dispatch>> {
  verdict_id, created
  recipients[] : accepted | quarantined | unknown | refused
  media[] : grant | defer | have | deny
}

class Reservation <<scoped grant>> {
  token  (bearer, hash-stored)
  target_url : https ingest door
  max_size, expires
}

class MediaObject <<content-addressed bytes>> {
  urn : urn:mlet
  size, state : pending | live
}

class Reference <<one mailbox's claim on one urn>> {
  state : offered | expected | available |\n pinned | unavailable | promised
  cause? : expired-remote | expired-local |\n declined | failed | deleted
  ephemeral : bool  (D-139 class)
  available_until : EFFECTIVE deadline (MEP-001)
  preview_of?
}

class Thread {
  root_ca, done, flagged, junk, rollup
}
class Message {
  read, received_at
}
class Delivery <<the sender's job view>> {
  job_tag, delegation_budget
}
class TimelineEvent {
  kind : protocol facts only (D-147/D-149)
}
class GuestLink {
  token_hash, pin_hash?, expires
  pin_failures  (locks at 5)
  claimed_addr?  (link survives claim)
}
class Mailbox {
  local_part
}
class Correspondent {
  tier_override? : allow | ask-first | block
  first_outbound_at?  (the legible tier reason, D-162)
}
class WebAuthnCredential {
  credential_id, cose_key, alg, sign_count
}

Medialet "1" *-- "0..*" ManifestEntry : manifest
Medialet "1" *-- "1" AuthorSignature
SignedEnvelope "1" o-- "1" Medialet : carries verbatim
SignedEnvelope "1" *-- "1" HopSignature
SignedEnvelope "1" *-- "0..*" HopAttestation : hops (§3.4.2)
Verdict "1" --> "1" SignedEnvelope : answers
Verdict "1" *-- "0..*" Reservation : grants mint
Reservation --> MediaObject : admits bytes of
ManifestEntry --> MediaObject : addresses
Reference --> MediaObject : claims
Mailbox "1" o-- "0..*" Reference
Mailbox "1" o-- "0..*" Thread
Thread "1" *-- "1..*" Message
Message --> Medialet
Delivery --> Medialet : dispatched
Delivery "1" *-- "0..*" TimelineEvent
Delivery "1" *-- "0..*" GuestLink
Mailbox "1" o-- "0..*" Correspondent
Mailbox "1" o-- "0..*" WebAuthnCredential
ManifestEntry "0..1" --> "0..1" ManifestEntry : preview_of
@enduml
```

## The loyalties (what belongs to whom)

| Entity | Loyal to | Consequence |
|---|---|---|
| Medialet + Manifest + AuthorSignature | the author | byte-immutable; forwarding and re-dispatch carry it verbatim (TV-004/TV-006 byte-identity) |
| SignedEnvelope + HopSignature | the dispatching hop | fresh per dispatch; the guest claim mints a new one around the old Medialet (D-154) |
| Verdict + Reservation | the receiving domain | policy lives here (tiers, D-139 knob); terminal verdicts immutable (§7.6) |
| Reference | one mailbox | the §10.3 state machine, trigger-enforced; the tombstone is the row itself (§10.4) |
| MediaObject | the domain's store | one copy per urn per domain, however many references claim it |
| GuestLink | the delivery | capability, hash-stored; possession + PIN is the claim ceremony (D-240) |
| Correspondent | one mailbox | tiers are per-relationship, never global (D-162) |
```
