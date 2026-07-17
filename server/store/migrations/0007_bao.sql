-- S4.22: MEP-003/MEP-004 implementation plumbing.

-- §8.9: a resource's transfer encoding is fixed by its first PATCH
-- and persists across resumption (offsets are encoded-stream bytes
-- for mlp-bao). 'raw' is the §8.4 baseline.
ALTER TABLE reservations_in ADD COLUMN encoding TEXT NOT NULL DEFAULT 'raw'
  CHECK (encoding IN ('raw','mlp-bao'));

-- The pusher decides raw-vs-bao by the RECEIVING domain's §5.2
-- capability advertisement — so the grant's domain identity must
-- survive into the push queue (it was previously implicit in
-- target_url, which is an endpoint, not an identity).
ALTER TABLE reservations_out ADD COLUMN target_domain TEXT NOT NULL DEFAULT '';
