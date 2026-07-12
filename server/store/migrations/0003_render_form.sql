-- 0003: S4.11 — the server-side sanitization duty lands (D-94; the
-- 0001 schema already provisioned medialets.render_form and
-- .derived_text for it). This adds only the degradation marker: a
-- Body that failed the §11.5 fixpoint or caps renders as its derived
-- text everywhere, and readers must be able to tell that state from
-- merely not-yet-derived (both have render_form NULL).
ALTER TABLE medialets ADD COLUMN render_degraded INTEGER NOT NULL DEFAULT 0;
