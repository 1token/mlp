# Sequence diagrams — the five dances

Each flow below is executed by a named test on every CI run; the
diagram is the narrative, the test is the proof.

## 1. Direct delivery: dispatch → verdict → push → available

Certified by `cmd/mlpd TestTwoDomainDemo` (bullets 2–3) and the TV-001/
TV-002/TV-003 vector suites.

```plantuml
@startuml seq-direct-delivery
!theme plain
autonumber
participant "Petra's client" as C
participant "origin SN" as OSN
participant "origin BS" as OBS
participant "target SN" as TSN
participant "target BS" as TBS
participant "Novák's client" as N

C -> OSN : POST /drafts/{id}/send
note right : pre-flight: possession of every\nManifest urn (D-135); first outbound\ncontact → correspondent (D-246)
OSN -> OSN : sign author/1 + hop/1;\nderive render form (§11)
OSN -> TSN : POST /dispatch (Signed Envelope)
TSN -> TSN : validate (§3.4.4 caps, locality,\nskew, signatures); tier the sender;\nmediaOutcomes (D-139 knob)
TSN -> TSN : materialize: thread, message,\nrefs offered (granted → expected,\nephemeral class)
TSN --> OSN : signed Verdict\n(grant small / defer large)
OSN -> OSN : applySnapshot →\nreservations_out (pending)
OBS -> TBS : HEAD /ingest/{res} → offset 0
OBS -> TBS : PATCH chunks (RFC 9421-signed,\ncontent-digest per chunk)
TBS -> TBS : verify by urn at completion →\nobject live; OnVerified flips\nrefs expected → available
TBS --> N : (SSE) media.changed
N -> TSN : GET /threads/{id} — renders alive
@enduml
```

## 2. Deferred accept → verdict upgrade → resumable push with a crash

Certified by `TestTwoDomainDemo` bullet 4 (kill at a checkpointed
2 MiB, resume, byte-verify) and the bs TV-003 torture suite.

```plantuml
@startuml seq-accept-resume
!theme plain
autonumber
participant "Novák's client" as N
participant "target Client API" as TAPI
participant "origin SN" as OSN
participant "origin push loop" as PUSH
participant "target BS" as TBS

N -> TAPI : POST /o/{urn}/accept
TAPI -> TAPI : possession short-circuit? (D-241)\nobject live → instant available — else:
TAPI -> TAPI : defer verdict found →\nmint Reservation; refs offered→expected
TAPI -> OSN : POST /verdict (upgrade snapshot,\napplication/mlp-verdict+json)
OSN -> OSN : terminal states immutable (§7.6);\ngrant → reservations_out pending
PUSH -> TBS : HEAD → offset 0; PATCH chunk 1 (2 MiB)
TBS --> PUSH : Upload-Offset: 2097152 (durable)
note over PUSH : ✂ process killed — row stays\n'pushing', checkpoint kept (D-248)
PUSH -> TBS : (restart) HEAD → offset 2097152
PUSH -> TBS : PATCH from 2 MiB — zero redundant bytes (§8.7)
TBS -> TBS : final chunk: BLAKE3 equals urn\nor the object is quarantined (§8.6)
TBS --> N : available
@enduml
```

## 3. Custody forwarding + delegated fulfillment (MEP-001)

Certified by `sn/mep_test.go` (TV-006 byte-identity, effective
deadline, the declarant-bound window) and
`TestNonChainSourceNeverContacted` (§9.2).

```plantuml
@startuml seq-custody-fulfillment
!theme plain
autonumber
participant "target SN\n(custody holder)" as T
participant "final SN\n(new recipient)" as F
participant "origin SN\n(root author domain)" as O

T -> T : Forward(Custody, until=Sept 1):\nfresh envelope, SAME Signed Medialet,\nsources = [self+until, origin];\nhops carry the root attestation (D-84)
T -> F : POST /dispatch
F -> F : verify CURRENT hop fully;\nattestations structurally (§3.4.2);\neffective deadline = max(manifest,\ncovering until) → refs.available_until
note right of F : the offer outlives the author's\nJuly window under the custodian's\nSeptember promise — MEP-001
F -> F : accept → candidates from\nfulfillment_sources, CHAIN MEMBERS\nONLY (§9.2 — interlopers discarded)
F -> T : POST /fulfill (signed request,\nreservation for the urn)
T -> T : §9.5: honor the until I MYSELF\nhop-signed (own dispatch records);\nnever another party's declaration
T --> F : will-push (inside the window)\n/ refused not-available (past it)
T -> F : resumable push (as flow 2)
F -> O : (only if T refuses and O is next\nin preference order)
@enduml
```

## 4. Guest delivery → claim → instant-have (D-151–D-155)

Certified by `clientapi TestGuestJourneyEndToEnd` and
`TestTwoDomainDemo` bullet 7.

```plantuml
@startuml seq-guest-claim
!theme plain
autonumber
actor Friend as G
participant "guest page\n(/g/{token})" as P
participant "origin Client API" as API
participant "origin SN" as SN

note over G : link via the D-153 notification\n(link only); PIN via Petra's\nown second channel (D-238)
G -> P : open /g/{token}
P -> API : GET /guest/{token}\nX-MLP-Guest-PIN
API -> API : token hash → link; expiry;\nlock ≥5 failures BEFORE PIN eval;\ncorrect PIN resets the counter
API --> P : payload: render form + files\n(NO view event — D-147)
G -> P : download
API -> API : guest_downloads + timeline fact
G -> P : claim "friend"
P -> API : POST /guest/{token}/claim
API -> API : one claim per link (409 after);\nmint mailbox
API -> SN : Redispatch(medialet_ca, friend@origin)
SN -> SN : fresh envelope around the ORIGINAL\nSigned Medialet (author sig intact);\nProcessDispatch on self — the real\ningest path (self-domain keys, D-240)
API --> G : session cookie; address
G -> API : POST /o/{urn}/accept
API --> G : {state: available, instant: true}\n— the bytes never moved (D-241)
note over G : the link still works,\nnow annotated claimed_as (D-154)
@enduml
```

## 5. Passkey ceremonies (D-161/D-242)

Certified by `webauthn` package tests (synthetic authenticators,
both algorithms) and `clientapi TestWebAuthnCeremonies`.

```plantuml
@startuml seq-webauthn
!theme plain
autonumber
actor User as U
participant Browser as B
participant "Client API" as API

== Registration (under a session — the claim lands here) ==
B -> API : POST /webauthn/register/begin
API --> B : challenge (single-use, 5 min TTL),\nrp, user, params [Ed25519, ES256]
B -> U : navigator.credentials.create()
U --> B : attestation (fmt "none")
B -> API : register/finish {clientDataJSON,\nattestationObject}
API -> API : consume challenge (delete-on-read);\nverify type/challenge/origin;\nCBOR strict-decode; rpIdHash; UP;\nstore COSE key
== Login (public, CSRF-headered) ==
B -> API : login/begin {address}
API --> B : challenge + allowCredentials\n(unknown address ⇒ empty list —\nindistinguishable)
B -> U : navigator.credentials.get()
B -> API : login/finish {authenticatorData,\nsignature, ...}
API -> API : verify sig over\nauthData ‖ SHA-256(clientDataJSON);\nsign-count regression logged, not fatal
API --> B : session cookie
@enduml
```
