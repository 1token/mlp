# Data model — the reference schema, by subsystem

Generated from `server/store/migrations/` (0001–0006) by
`docs/architecture/gen-er.py`; regenerate after any migration. The
§10.3 reference state machine on `refs` is enforced by a database
trigger (see 0001), so illegal transitions fail at the storage
layer, not by convention. Sensitive columns (`seed`, `token_hash`,
`pin_hash`, `session_hash`) hold seeds or hashes exactly as the
decisions demand — a database leak must not mint working
capabilities (D-155). The search index itself (`search_fts`, an FTS4
virtual table from 0006) is not an entity: it is rebuildable derived
data over `medialets.derived_text` and `object_text` (D-261).

## Federation core (dispatch, verdicts, delegation, discovery)

```plantuml
@startuml er-federation-core-dispatch-verdicts-delegation-discovery
!theme plain
hide circle
skinparam linetype ortho

entity medialets {
  *content_address : TEXT
  author : TEXT
  medialet_id : TEXT
  created : TEXT
  raw : BLOB
  render_form : TEXT
  derived_text : TEXT
  classifier : UNIQUE
  render_degraded : INTEGER
}
entity envelopes_in {
  *id : INTEGER
  origin : TEXT
  envelope_id : TEXT
  medialet_ca : TEXT
  received_at : TEXT
  forwarded_by : TEXT
  hops_json : TEXT
  fulfillment_sources_json : TEXT
  author_sig_result : TEXT
  author_sig_kid : TEXT
  author_verified_at : TEXT
  hop_sig_result : TEXT
  hop_sig_kid : TEXT
  hop_verified_at : TEXT
  envelope_created : TEXT
  hop_sig_value : TEXT
}
entity dispatches {
  *envelope_id : TEXT
  target_domain : TEXT
  medialet_ca : TEXT
  created : TEXT
  envelope_canonical : BLOB
  hop_sig_value : TEXT
  hop_kid : TEXT
  delivery_id : INTEGER
}
entity dispatch_recipients {
  envelope_id : TEXT
  addr : TEXT
}
entity verdicts {
  *id : INTEGER
  direction : TEXT
  verdict_id : TEXT
  created : TEXT
  issuer : TEXT
  envelope_origin : TEXT
  envelope_id : TEXT
  message : TEXT
  doc : BLOB
  verbatim : UNIQUE
}
entity verdict_media {
  verdict_row : INTEGER
  urn : TEXT
  verdict : TEXT
  reason : TEXT
  reservation_json : TEXT
}
entity delegations {
  request_id : TEXT
  requester : TEXT
  envelope_id : TEXT
  urn : TEXT
  status : TEXT
  reason : TEXT
  created : TEXT
}
entity domain_docs {
  *domain : TEXT
  doc : TEXT
  fetched_at : TEXT
  expires_at : TEXT
}
entity domain_keys {
  domain : TEXT
  kid : TEXT
  key : TEXT
  roles : TEXT
  not_before : TEXT
  not_after : TEXT
}
entity own_keys {
  *kid : TEXT
  seed : BLOB
  roles : TEXT
  not_before : TEXT
  not_after : TEXT
}
envelopes_in }o--|| medialets : medialet_ca
dispatches }o--|| medialets : medialet_ca
dispatch_recipients }o--|| dispatches : envelope_id
verdict_media }o--|| verdicts : verdict_row
delegations }o--|| dispatches : envelope_id
@enduml
```

Cross-subsystem references: `dispatches.delivery_id` → `deliveries`.

## Mailbox & threading

```plantuml
@startuml er-mailbox-threading
!theme plain
hide circle
skinparam linetype ortho

entity mailboxes {
  *id : INTEGER
  local_part : TEXT
  display_name : TEXT
  created : TEXT
}
entity threads {
  *id : INTEGER
  mailbox_id : INTEGER
  root_ca : TEXT
  done : INTEGER
  never : retention
  junk : INTEGER
  last_activity : TEXT
  rollup_json : TEXT
}
entity messages {
  *id : INTEGER
  mailbox_id : INTEGER
  medialet_ca : TEXT
  envelope_in : INTEGER
  delivered_to : TEXT
  tag : TEXT
  thread_id : INTEGER
  read : INTEGER
  received_at : TEXT
}
entity refs {
  *id : INTEGER
  mailbox_id : INTEGER
  urn : TEXT
  medialet_ca : TEXT
  direction : TEXT
  state : TEXT
  cause : TEXT
  name : TEXT
  size : INTEGER
  type : TEXT
  available_until : TEXT
  ephemeral : INTEGER
  updated_at : TEXT
  preview_of : TEXT
}
entity correspondents {
  mailbox_id : INTEGER
  addr : TEXT
  display_name : TEXT
  tier_override : TEXT
  first_outbound_at : TEXT
}
entity labels {
  *id : INTEGER
  mailbox_id : INTEGER
  name : TEXT
  color : TEXT
  bundled : INTEGER
  notify : TEXT
}
entity thread_labels {
  thread_id : INTEGER
  label_id : INTEGER
}
entity ref_labels {
  ref_id : INTEGER
  label_id : INTEGER
}
entity rules {
  *two : doors
  mailbox_id : INTEGER
  kind : TEXT
  pattern : TEXT
  store_id : INTEGER
  label_id : INTEGER
  priority : INTEGER
}
entity drafts {
  *id : TEXT
  mailbox_id : INTEGER
  doc_json : TEXT
  updated : TEXT
}
entity undo_journal {
  *token : TEXT
  mailbox_id : INTEGER
  inverse_json : TEXT
  expires : TEXT
}
entity settings {
  mailbox_id : INTEGER
  key : TEXT
  value_json : TEXT
}
entity events {
  *id : INTEGER
  mailbox_id : INTEGER
  type : TEXT
  data_json : TEXT
  at : TEXT
}
entity idempotency {
  mailbox_id : INTEGER
  key : TEXT
  response_json : TEXT
  created : TEXT
}
threads }o--|| mailboxes : mailbox_id
messages }o--|| mailboxes : mailbox_id
messages }o--|| threads : thread_id
refs }o--|| mailboxes : mailbox_id
correspondents }o--|| mailboxes : mailbox_id
labels }o--|| mailboxes : mailbox_id
thread_labels }o--|| threads : thread_id
thread_labels }o--|| labels : label_id
ref_labels }o--|| refs : ref_id
ref_labels }o--|| labels : label_id
rules }o--|| mailboxes : mailbox_id
rules }o--|| labels : label_id
drafts }o--|| mailboxes : mailbox_id
@enduml
```

Cross-subsystem references: `messages.medialet_ca` → `medialets`; `messages.envelope_in` → `envelopes_in`; `rules.store_id` → `stores`.

## Transfer & storage

```plantuml
@startuml er-transfer-storage
!theme plain
hide circle
skinparam linetype ortho

entity stores {
  *id : INTEGER
  name : TEXT
  backend : TEXT
  quota_bytes : INTEGER
}
entity objects {
  *urn : TEXT
  size : INTEGER
  state : TEXT
  store_id : INTEGER
  created_at : TEXT
  verified_at : TEXT
}
entity reservations_in {
  *token_hash : TEXT
  urn : TEXT
  max_size : INTEGER
  pusher_domain : TEXT
  expires : TEXT
  state : TEXT
  upload_offset : INTEGER
  hasher_state : BLOB
  store_id : INTEGER
  created : TEXT
  encoding : TEXT
}
entity reservations_out {
  *id : INTEGER
  urn : TEXT
  target_url : TEXT
  token : TEXT
  max_size : INTEGER
  expires : TEXT
  envelope_id : TEXT
  state : TEXT
  offset_confirmed : INTEGER
  target_domain : TEXT
}
objects }o--|| stores : store_id
reservations_in }o--|| stores : store_id
@enduml
```

## Search (node-local derived, S4.19)

```plantuml
@startuml er-search-node-local-derived-s
!theme plain
hide circle
skinparam linetype ortho

entity object_text {
  *per : URN
  extractor : TEXT
  text : TEXT
  extracted_at : TEXT
}
@enduml
```

## Deliveries & guests

```plantuml
@startuml er-deliveries-guests
!theme plain
hide circle
skinparam linetype ortho

entity deliveries {
  *id : INTEGER
  mailbox_id : INTEGER
  medialet_ca : TEXT
  job_tag : TEXT
  delegation_budget : INTEGER
}
entity timeline_events {
  *id : INTEGER
  delivery_id : INTEGER
  at : TEXT
  kind : TEXT
  data_json : TEXT
}
entity guest_links {
  *id : INTEGER
  delivery_id : INTEGER
  recipient_hint : TEXT
  token_hash : TEXT
  pin_hash : TEXT
  expires : TEXT
  revoked_at : TEXT
  claimed_addr : TEXT
  claimed_at : TEXT
  pin_failures : INTEGER
}
entity guest_downloads {
  link_id : INTEGER
  urn : TEXT
  at : TEXT
}
timeline_events }o--|| deliveries : delivery_id
guest_links }o--|| deliveries : delivery_id
guest_downloads }o--|| guest_links : link_id
@enduml
```

Cross-subsystem references: `deliveries.mailbox_id` → `mailboxes`; `deliveries.medialet_ca` → `medialets`.

## Identity & sessions

```plantuml
@startuml er-identity-sessions
!theme plain
hide circle
skinparam linetype ortho

entity credentials {
  *credential_id : BLOB
  mailbox_id : INTEGER
  public_key : BLOB
  sign_count : INTEGER
  label : TEXT
  created : TEXT
}
entity password_fallback {
  *mailbox_id : INTEGER
  hash : TEXT
}
entity recovery_codes {
  mailbox_id : INTEGER
  code_hash : TEXT
  used_at : TEXT
}
entity recovery_email {
  *mailbox_id : INTEGER
  email : TEXT
  verified : INTEGER
  consented_at : TEXT
}
entity sessions {
  *session_hash : TEXT
  mailbox_id : INTEGER
  created : TEXT
  last_seen : TEXT
  user_agent : TEXT
}
entity webauthn_credentials {
  *credential_id : TEXT
  mailbox_id : INTEGER
  public_key : BLOB
  alg : INTEGER
  sign_count : INTEGER
  created : TEXT
}
entity webauthn_challenges {
  *challenge : TEXT
  purpose : TEXT
  mailbox_id : INTEGER
  created : TEXT
  expires_at : TEXT
}
@enduml
```

Cross-subsystem references: `credentials.mailbox_id` → `mailboxes`; `password_fallback.mailbox_id` → `mailboxes`; `recovery_codes.mailbox_id` → `mailboxes`; `recovery_email.mailbox_id` → `mailboxes`; `sessions.mailbox_id` → `mailboxes`; `webauthn_credentials.mailbox_id` → `mailboxes`; `webauthn_challenges.mailbox_id` → `mailboxes`.

