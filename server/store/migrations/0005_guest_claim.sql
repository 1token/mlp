-- 0005: S4.12 — guest + claim (S3.6, D-151–D-155) and passkey-first
-- identity (S3.8, D-161; WebAuthn joined this substage per D-233).
-- The 0001 schema already provisioned guest_links and
-- guest_downloads with the D-147/D-152/D-153 semantics; this adds
-- only the D-155 PIN-failure lock counter and the WebAuthn tables.

ALTER TABLE guest_links ADD COLUMN pin_failures INTEGER NOT NULL DEFAULT 0;

-- Passkeys (D-161). public_key is the COSE-encoded credential key;
-- alg is the COSE identifier (-7 ES256, -8 Ed25519). sign_count is
-- recorded for clone detection; a regression is logged, not fatal.
CREATE TABLE webauthn_credentials (
  credential_id TEXT PRIMARY KEY,     -- base64url
  mailbox_id INTEGER NOT NULL REFERENCES mailboxes(id),
  public_key BLOB NOT NULL,
  alg INTEGER NOT NULL,
  sign_count INTEGER NOT NULL DEFAULT 0,
  created TEXT NOT NULL
);

CREATE INDEX webauthn_credentials_mailbox ON webauthn_credentials(mailbox_id);

-- Single-use challenges, short TTL.
CREATE TABLE webauthn_challenges (
  challenge TEXT PRIMARY KEY,         -- base64url
  purpose TEXT NOT NULL CHECK (purpose IN ('register','login')),
  mailbox_id INTEGER REFERENCES mailboxes(id),
  created TEXT NOT NULL,
  expires_at TEXT NOT NULL
);
