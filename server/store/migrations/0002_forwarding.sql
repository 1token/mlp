-- 0002: S4.6 repair (D-209). §9.3 step 2 and §3.4.2 require the
-- Delivery Record to retain the received Envelope's `created` and its
-- Hop Signature *value* — D-53 promised "everything needed" to
-- construct the Hop Attestation of the received Envelope (forwarding
-- appends it; a requester presents it when the source is the received
-- Envelope's own origin). 0001 kept only the kid. Existing rows
-- backfill NULL: their envelopes cannot seed attestations, which is
-- the honest statement of what was recorded.
ALTER TABLE envelopes_in ADD COLUMN envelope_created TEXT;
ALTER TABLE envelopes_in ADD COLUMN hop_sig_value TEXT;
