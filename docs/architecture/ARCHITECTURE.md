# Architecture — the MLP reference implementation

Companion to the specification: the spec says what the wire demands;
this document says how this codebase is shaped to meet it. Diagrams
are PlantUML, renderable in VS Code or via plantuml.com.

## 1. System context — two domains, one delivery

The unit of the system is a **domain**: one `mlpd` process owning
one SQLite database and one content-addressed object root. Domains
peer directly over HTTPS; there is no central anything.

```plantuml
@startuml system-context
!theme plain
skinparam componentStyle rectangle

actor "Petra\n(author)" as petra
actor "Novák\n(reader)" as novak
actor "Friend\n(guest)" as guest

node "origin.example — mlpd" as origin {
  component "Client API\n/api/v1" as capi1
  component "Signaling Node (SN)\n/dispatch /fulfill /verdict" as sn1
  component "Body Store (BS)\n/ingest" as bs1
  database "SQLite +\nobject root" as db1
}

node "target.example — mlpd" as target {
  component "Client API" as capi2
  component "SN" as sn2
  component "BS" as bs2
  database "SQLite +\nobject root" as db2
}

petra --> capi1 : web client\n(compose, hash-first upload)
novak --> capi2 : web client\n(inbox, accept, library)
guest --> capi1 : /g/{token}\n(view, download, claim)

sn1 --> sn2 : POST /dispatch\nSigned Envelope
sn2 --> sn1 : signed Verdict\n(sync + async updates)
bs1 --> bs2 : PATCH /ingest/{res}\nresumable, RFC 9421-signed
sn2 <--> sn1 : /fulfill\ndelegation (§9)
sn1 ..> target : GET /.well-known/medialet.json\nhardened discovery (§5.4)

capi1 -- db1
sn1 -- db1
bs1 -- db1
capi2 -- db2
sn2 -- db2
bs2 -- db2
@enduml
```

**The load-bearing asymmetry**: signaling (envelopes, verdicts,
delegation) is small, synchronous, and always available; payload
moves only after a verdict grants a scoped, expiring, size-capped
reservation — and then as a resumable push from the holder to the
receiver's ingest door. No byte before its reservation.

## 2. Module structure

### 2.1 Server (Go)

```plantuml
@startuml server-modules
!theme plain
skinparam componentStyle rectangle

package "server" {
  [cmd/mlpd] as mlpd
  [clientapi] as capi
  [sn] as sn
  [bs] as bs
  [render] as render
  [webauthn] as wa
  [discovery] as disc
  [store] as store
  [core] as core
}

mlpd --> capi
mlpd --> sn
mlpd --> bs
mlpd --> disc
mlpd --> store

capi --> sn : Send, Redispatch,\nExpireOffers, RequestFulfillment
capi --> bs : ObjectPath, upload door
capi --> wa : ceremonies
sn --> render : §11 derivation at ingest
sn --> disc : Resolver (docs, keys)
sn --> core : JCS, signatures, URNs
bs --> disc : transfer-key resolution
bs --> core
disc --> core
render --> core

note right of core
  stdlib-crypto only (M041):
  JCS canonicalization, Ed25519
  doc signatures, BLAKE3 URNs,
  multiformats, dialect parsing
end note
note bottom of store
  migrations 0001–0005;
  §10.3 state machine
  enforced by DB trigger
end note
@enduml
```

Dependency rule: `core` depends on nothing internal; `store` is
schema only; nothing imports `cmd/mlpd`. The `sn`/`bs`/`clientapi`
triangle communicates through the shared database and narrow
exported seams (hooks like `OnVerified`, `GuestNotifier`,
`DispatchEndpoint`) — which is exactly what lets every test wire a
domain in-process and lets `mlpd` be thin.

### 2.2 Client (browser, no build step)

```plantuml
@startuml client-modules
!theme plain
skinparam componentStyle rectangle

package "client" {
  [index.html / guest.html] as shells
  package "app" {
    [mlp-app] as approot
    [mlp-inbox] as inbox
    [mlp-thread] as thread
    [mlp-composer] as composer
    [mlp-deliveries] as deliveries
    [mlp-media] as media
    [mlp-guest] as guestjs
    [mlp-body-viewer] as viewer
  }
  package "store" {
    [store.js] as st
    [api.js] as api
  }
  package "lib" {
    [html.js] as html
    [sanitize.js] as sanitize
    [mlet-urn.js] as urn
    [vendor/noble (blake3)] as noble
  }
}

shells --> approot
shells --> guestjs
approot --> inbox
approot --> thread
approot --> composer
approot --> deliveries
approot --> media
thread --> viewer
guestjs --> viewer : one Body viewer,\ntwo hosts (D-151)
viewer --> sanitize : render-time\nre-sanitization (D-31)
composer --> urn : hash-first\nfile door (D-135)
urn --> noble
inbox --> st
composer --> api
guestjs ..> api : sessionless\nguest endpoints
@enduml
```

## 3. Deployment view

```plantuml
@startuml deployment
!theme plain
node "operator host" {
  node "reverse proxy (TLS :443)" as proxy
  node "mlpd (127.0.0.1:8441)" as mlpd
  folder "/var/lib/mlpd" {
    file "mlp.db"
    folder "objects/"
  }
}
cloud "peers" as peers
actor users

users --> proxy : https
peers --> proxy : https\n(.well-known, /dispatch,\n/fulfill, /verdict, /ingest)
proxy --> mlpd : http, paths verbatim\n(NEVER rewrite /ingest/ —\nRFC 9421 signs the URI)
mlpd --> "mlp.db"
mlpd --> "objects/"
@enduml
```

Operational contract: back up `mlp.db` and `objects/` as one unit;
rotate keys additively; pass no `-peer` flags in production
(docs/OPERATOR.md).

## 4. The three background loops

`mlpd` runs three periodic workers over durable state — restart-safe
by construction because the database rows *are* the work queue:

1. **Push loop** (300 ms): `reservations_out` rows in
   `pending`/`pushing` → resumable `Pusher.Push` from the receiver's
   confirmed offset (§8.7).
2. **Offer expiry** (lazy, on list reads): `ExpireOffers` fires the
   §10.3 `offered → unavailable(expired-remote)` transition at the
   MEP-001 effective deadline.
3. **GC sweep** (hourly): `CollectGarbage` under the §10.5
   invariants — ephemeral class only (D-251).
```
