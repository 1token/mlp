-- MLP reference implementation — migration 0001 (S4.2)
-- Conventions (D-192): timestamps are RFC 3339 UTC TEXT (fixed-width,
-- lexicographically sortable); JSON columns are TEXT in the D-43
-- dialect; capability secrets we MINT (inbound reservation tokens,
-- guest tokens, PINs, session ids) are stored as *_hash (SHA-256 hex);
-- tokens we must PRESENT (reservations granted to us) are plaintext by
-- necessity. Comments trace to spec sections and decisions.

PRAGMA foreign_keys = ON;

------------------------------------------------------------------
-- Storage layer: stores, objects, references (spec §10, D-105)
------------------------------------------------------------------

CREATE TABLE stores (                 -- D-105 multi-instance BS
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  backend TEXT NOT NULL DEFAULT 'fs',
  quota_bytes INTEGER
);

CREATE TABLE objects (                -- domain-level: absent = no row (§10.2)
  urn TEXT PRIMARY KEY,               -- urn:mlet: (D-25)
  size INTEGER NOT NULL CHECK (size >= 0),
  state TEXT NOT NULL CHECK (state IN ('pending','live')),
  store_id INTEGER NOT NULL REFERENCES stores(id),
  created_at TEXT NOT NULL,
  verified_at TEXT                    -- set when live (§8.4)
);

-- The per-mailbox reference state machine (§10.3, D-87).
-- Named `refs`: REFERENCES is an SQL keyword.
CREATE TABLE refs (
  id INTEGER PRIMARY KEY,
  mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id),
  urn TEXT NOT NULL,
  medialet_ca TEXT NOT NULL,          -- provenance (D-156: one row per delivery)
  direction TEXT NOT NULL CHECK (direction IN ('in','out')),
  state TEXT NOT NULL CHECK (state IN
    ('offered','expected','available','pinned','unavailable','promised')),
  cause TEXT CHECK (cause IN
    ('expired-remote','expired-local','declined','failed','deleted')),
  -- tombstone minimum record (§10.4, D-87):
  name TEXT, size INTEGER NOT NULL, type TEXT NOT NULL,
  available_until TEXT NOT NULL,      -- Manifest window (D-19); MEP-001 will widen
  ephemeral INTEGER NOT NULL DEFAULT 0, -- D-139 GC-first class
  updated_at TEXT NOT NULL,
  CHECK ((state = 'unavailable') = (cause IS NOT NULL)),
  CHECK ((direction = 'out') = (state = 'promised')),  -- §10.5 outbound promises
  UNIQUE (mailbox_id, urn, medialet_ca)
);
CREATE INDEX refs_by_state ON refs(mailbox_id, state);
CREATE INDEX refs_by_urn ON refs(urn);

-- D-87's transition table, enforced: anything not listed aborts.
-- pinned -> unavailable(expired-local) is deliberately absent (D-88:
-- pin protects from GC, never from the owner); unavailable is terminal.
CREATE TRIGGER refs_transitions
BEFORE UPDATE OF state, cause ON refs
FOR EACH ROW
WHEN NOT (
     (OLD.state = NEW.state AND OLD.cause IS NEW.cause)                                   -- non-state edits
  OR (OLD.state='offered'   AND NEW.state='expected')                                      -- accept (§7.6/§9.3)
  OR (OLD.state='offered'   AND NEW.state='unavailable' AND NEW.cause IN ('expired-remote','declined'))
  OR (OLD.state='expected'  AND NEW.state='available')                                     -- verified ingest / have
  OR (OLD.state='expected'  AND NEW.state='offered')                                       -- reservation lapsed, window live
  OR (OLD.state='expected'  AND NEW.state='unavailable' AND NEW.cause IN ('failed','expired-remote'))
  OR (OLD.state='available' AND NEW.state='pinned')
  OR (OLD.state='pinned'    AND NEW.state='available')                                     -- unpin
  OR (OLD.state='available' AND NEW.state='unavailable' AND NEW.cause IN ('expired-local','deleted'))
  OR (OLD.state='pinned'    AND NEW.state='unavailable' AND NEW.cause='deleted')           -- owner only
)
BEGIN
  SELECT RAISE(ABORT, 'invalid-transition');
END;

------------------------------------------------------------------
-- Medialets and inbound envelopes (spec §3, D-28/D-53)
------------------------------------------------------------------

CREATE TABLE medialets (
  content_address TEXT PRIMARY KEY,   -- §3.3.3
  author TEXT NOT NULL,
  medialet_id TEXT NOT NULL,
  created TEXT NOT NULL,
  raw BLOB NOT NULL,                  -- verbatim signed artifact (D-28)
  render_form TEXT,                   -- receiver-derived (D-94); never authoritative
  derived_text TEXT,                  -- §11.6: snippets, search, classifier
  UNIQUE (author, medialet_id)        -- (author, id) dedup scope (D-46)
);

CREATE TABLE envelopes_in (           -- Delivery Records (D-53 + D-55)
  id INTEGER PRIMARY KEY,
  origin TEXT NOT NULL,
  envelope_id TEXT NOT NULL,
  medialet_ca TEXT NOT NULL REFERENCES medialets(content_address),
  received_at TEXT NOT NULL,
  forwarded_by TEXT,
  hops_json TEXT,                     -- retained for delegation requests (D-53/§9.3)
  fulfillment_sources_json TEXT,
  author_sig_result TEXT NOT NULL, author_sig_kid TEXT NOT NULL, author_verified_at TEXT NOT NULL,
  hop_sig_result TEXT NOT NULL,    hop_sig_kid TEXT NOT NULL,    hop_verified_at TEXT NOT NULL,
  UNIQUE (origin, envelope_id)        -- replay dedup (D-20); retry via CA match (D-74)
);

------------------------------------------------------------------
-- Outbound: dispatches (the §9.5 credential store), delegation, verdicts
------------------------------------------------------------------

CREATE TABLE dispatches (
  envelope_id TEXT PRIMARY KEY,       -- we are origin
  target_domain TEXT NOT NULL,
  medialet_ca TEXT NOT NULL REFERENCES medialets(content_address),
  created TEXT NOT NULL,
  envelope_canonical BLOB NOT NULL,   -- what we signed: validates root attestations byte-for-byte (§9.5.2)
  hop_sig_value TEXT NOT NULL,
  hop_kid TEXT NOT NULL,
  delivery_id INTEGER REFERENCES deliveries(id)
);
CREATE TABLE dispatch_recipients (
  envelope_id TEXT NOT NULL REFERENCES dispatches(envelope_id),
  addr TEXT NOT NULL,
  PRIMARY KEY (envelope_id, addr)
);

CREATE TABLE delegations (            -- budget = accepted rows per (envelope, urn) (D-83)
  request_id TEXT NOT NULL,
  requester TEXT NOT NULL,
  envelope_id TEXT NOT NULL REFERENCES dispatches(envelope_id),
  urn TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('accepted','refused','refunded')),
  reason TEXT,
  created TEXT NOT NULL,
  PRIMARY KEY (requester, request_id, urn)   -- request dedup (D-83)
);

CREATE TABLE verdicts (               -- full history kept: D-71 snapshots + D-149 timeline
  id INTEGER PRIMARY KEY,
  direction TEXT NOT NULL CHECK (direction IN ('in','out')),
  verdict_id TEXT NOT NULL,
  created TEXT NOT NULL,              -- supersession order (D-71)
  issuer TEXT NOT NULL,
  envelope_origin TEXT NOT NULL,
  envelope_id TEXT NOT NULL,
  message TEXT NOT NULL CHECK (message IN ('accepted','rejected','quarantined')),
  doc BLOB NOT NULL,                  -- the signed document, verbatim
  UNIQUE (direction, issuer, verdict_id)
);
CREATE TABLE verdict_media (
  verdict_row INTEGER NOT NULL REFERENCES verdicts(id),
  urn TEXT NOT NULL,
  verdict TEXT NOT NULL CHECK (verdict IN ('grant','have','defer','deny')),
  reason TEXT,
  reservation_json TEXT,
  PRIMARY KEY (verdict_row, urn)
);

------------------------------------------------------------------
-- Reservations (§7.5, §8; D-18/D-27)
------------------------------------------------------------------

CREATE TABLE reservations_in (        -- we minted; BS enforces (D-18)
  token_hash TEXT PRIMARY KEY,        -- capability never stored plaintext (D-192)
  urn TEXT NOT NULL,
  max_size INTEGER NOT NULL,
  pusher_domain TEXT NOT NULL,        -- reservation binds to pusher identity (D-22)
  expires TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','consumed','expired')),
  upload_offset INTEGER NOT NULL DEFAULT 0,
  hasher_state BLOB,                  -- persisted BLAKE3 checkpoint (D-27/D-77)
  store_id INTEGER NOT NULL REFERENCES stores(id),
  created TEXT NOT NULL
);
CREATE TRIGGER reservations_single_use     -- D-18: consumed is terminal
BEFORE UPDATE OF state ON reservations_in
FOR EACH ROW WHEN OLD.state = 'consumed'
BEGIN SELECT RAISE(ABORT, 'reservation-invalid'); END;

CREATE TABLE reservations_out (       -- granted to us; we push (§8)
  id INTEGER PRIMARY KEY,
  urn TEXT NOT NULL,
  target_url TEXT NOT NULL,
  token TEXT NOT NULL,                -- must be presented; plaintext by necessity
  max_size INTEGER NOT NULL,
  expires TEXT NOT NULL,
  envelope_id TEXT NOT NULL,          -- ours (direct) or the delegation context
  state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','pushing','done','failed','expired')),
  offset_confirmed INTEGER NOT NULL DEFAULT 0
);

------------------------------------------------------------------
-- Discovery cache and our keys (§5–§6; D-33)
------------------------------------------------------------------

CREATE TABLE domain_docs (
  domain TEXT PRIMARY KEY,
  doc TEXT NOT NULL,
  fetched_at TEXT NOT NULL,
  expires_at TEXT NOT NULL            -- ceiling 24 h (D-33)
);
CREATE TABLE domain_keys (
  domain TEXT NOT NULL,
  kid TEXT NOT NULL,
  key TEXT NOT NULL,                  -- kid self-verified on load (D-62)
  roles TEXT NOT NULL,                -- JSON array
  not_before TEXT, not_after TEXT,
  PRIMARY KEY (domain, kid)
);
CREATE TABLE own_keys (
  kid TEXT PRIMARY KEY,
  seed BLOB NOT NULL,                 -- operator guide: protect the database file
  roles TEXT NOT NULL,
  not_before TEXT, not_after TEXT
);

------------------------------------------------------------------
-- Mailboxes, identity, sessions (S3.8 / D-161)
------------------------------------------------------------------

CREATE TABLE mailboxes (
  id INTEGER PRIMARY KEY,
  local_part TEXT NOT NULL UNIQUE,    -- routing form (D-55)
  display_name TEXT,
  created TEXT NOT NULL
);
CREATE TABLE credentials (            -- WebAuthn
  credential_id BLOB PRIMARY KEY,
  mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id),
  public_key BLOB NOT NULL,
  sign_count INTEGER NOT NULL DEFAULT 0,
  label TEXT, created TEXT NOT NULL
);
CREATE TABLE password_fallback (mailbox_id INTEGER PRIMARY KEY REFERENCES mailboxes(id), hash TEXT NOT NULL);
CREATE TABLE recovery_codes (mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id), code_hash TEXT NOT NULL, used_at TEXT, PRIMARY KEY (mailbox_id, code_hash));
CREATE TABLE recovery_email (mailbox_id INTEGER PRIMARY KEY REFERENCES mailboxes(id), email TEXT NOT NULL, verified INTEGER NOT NULL DEFAULT 0, consented_at TEXT);
CREATE TABLE sessions (
  session_hash TEXT PRIMARY KEY,      -- D-192
  mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id),
  created TEXT NOT NULL, last_seen TEXT NOT NULL, user_agent TEXT
);

------------------------------------------------------------------
-- Messages, threads, labels, triage (S3.2 / D-119–D-132)
------------------------------------------------------------------

CREATE TABLE messages (               -- per-mailbox instance of a medialet
  id INTEGER PRIMARY KEY,
  mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id),
  medialet_ca TEXT NOT NULL REFERENCES medialets(content_address),
  envelope_in INTEGER REFERENCES envelopes_in(id),   -- NULL for own/sent copies
  delivered_to TEXT,                  -- full tagged address (D-55)
  tag TEXT,                           -- subaddress tag → label mapping (D-119)
  thread_id INTEGER NOT NULL REFERENCES threads(id),
  read INTEGER NOT NULL DEFAULT 0,
  received_at TEXT NOT NULL,
  UNIQUE (mailbox_id, medialet_ca)
);
CREATE TABLE threads (
  id INTEGER PRIMARY KEY,
  mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id),
  root_ca TEXT NOT NULL,              -- in_reply_to tree root (D-110)
  done INTEGER NOT NULL DEFAULT 0,    -- triage, never retention (D-120)
  flagged INTEGER NOT NULL DEFAULT 0,
  junk INTEGER NOT NULL DEFAULT 0,
  last_activity TEXT NOT NULL,
  rollup_json TEXT                    -- D-132 precomputed: participants, snippet, chips, deadline
);
CREATE INDEX threads_inbox ON threads(mailbox_id, done, last_activity DESC);

CREATE TABLE labels (
  id INTEGER PRIMARY KEY,
  mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id),
  name TEXT NOT NULL, color TEXT,
  bundled INTEGER NOT NULL DEFAULT 0, -- topics are bundled labels (D-119)
  notify TEXT NOT NULL DEFAULT 'instant' CHECK (notify IN ('instant','digest','silent')),
  UNIQUE (mailbox_id, name)
);
CREATE TABLE thread_labels (thread_id INTEGER NOT NULL REFERENCES threads(id), label_id INTEGER NOT NULL REFERENCES labels(id), PRIMARY KEY (thread_id, label_id));
CREATE TABLE ref_labels (ref_id INTEGER NOT NULL REFERENCES refs(id), label_id INTEGER NOT NULL REFERENCES labels(id), PRIMARY KEY (ref_id, label_id));  -- media labels live at reference level (D-111/D-156)

CREATE TABLE rules (                  -- one rules table, two doors (D-160/D-162)
  id INTEGER PRIMARY KEY,
  mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id),
  kind TEXT NOT NULL CHECK (kind IN ('tag','from','domain','type')),
  pattern TEXT NOT NULL,
  store_id INTEGER REFERENCES stores(id),
  label_id INTEGER REFERENCES labels(id),
  priority INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE correspondents (
  mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id),
  addr TEXT NOT NULL,                 -- mailbox-key form (D-55)
  display_name TEXT,
  tier_override TEXT CHECK (tier_override IN ('allow','ask-first','block')),  -- D-162/D-163
  first_outbound_at TEXT,             -- the legible tier reason (D-162)
  PRIMARY KEY (mailbox_id, addr)
);

------------------------------------------------------------------
-- Studio: deliveries, guest links, timeline (S3.5–S3.6)
------------------------------------------------------------------

CREATE TABLE deliveries (
  id INTEGER PRIMARY KEY,
  mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id),
  medialet_ca TEXT NOT NULL REFERENCES medialets(content_address),
  job_tag TEXT,
  delegation_budget INTEGER,          -- NULL=default 10, 0=off (D-148)
  created TEXT NOT NULL
);
CREATE TABLE guest_links (
  id INTEGER PRIMARY KEY,
  delivery_id INTEGER NOT NULL REFERENCES deliveries(id),
  recipient_hint TEXT,                -- the notified email (D-153)
  token_hash TEXT NOT NULL UNIQUE,    -- ≥128-bit capability (D-152/D-192)
  pin_hash TEXT,
  expires TEXT NOT NULL,
  revoked_at TEXT, claimed_addr TEXT, claimed_at TEXT
);
CREATE TABLE guest_downloads (link_id INTEGER NOT NULL REFERENCES guest_links(id), urn TEXT NOT NULL, at TEXT NOT NULL);  -- downloads shown, opens never stored (D-147)
CREATE TABLE timeline_events (        -- D-149: the protocol-fact feed
  id INTEGER PRIMARY KEY,
  delivery_id INTEGER NOT NULL REFERENCES deliveries(id),
  at TEXT NOT NULL, kind TEXT NOT NULL, data_json TEXT NOT NULL
);

------------------------------------------------------------------
-- Client-API plumbing (D-170) and live feed (D-132)
------------------------------------------------------------------

CREATE TABLE drafts (id TEXT PRIMARY KEY, mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id), doc_json TEXT NOT NULL, updated TEXT NOT NULL);
CREATE TABLE undo_journal (token TEXT PRIMARY KEY, mailbox_id INTEGER NOT NULL, inverse_json TEXT NOT NULL, expires TEXT NOT NULL);
CREATE TABLE idempotency (mailbox_id INTEGER NOT NULL, key TEXT NOT NULL, response_json TEXT NOT NULL, created TEXT NOT NULL, PRIMARY KEY (mailbox_id, key));
CREATE TABLE settings (mailbox_id INTEGER NOT NULL, key TEXT NOT NULL, value_json TEXT NOT NULL, PRIMARY KEY (mailbox_id, key));
CREATE TABLE events (                 -- SSE with Last-Event-ID resume (D-132/D-170)
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  mailbox_id INTEGER NOT NULL,
  type TEXT NOT NULL, data_json TEXT NOT NULL, at TEXT NOT NULL
);
