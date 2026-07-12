-- 0004: MEP-002 accepted (2026-07-12). The preview pairing becomes
-- structural: refs carry the Manifest's preview_of so the library can
-- fold cards without the D-158 markup heuristic. Descriptive only —
-- never a policy input (D-111/D-107; auto-grant keys on size, D-139).
ALTER TABLE refs ADD COLUMN preview_of TEXT;
