-- S4.19: local search (D-261). Everything in this migration is
-- node-local derived data in the §11.6 family: never authoritative,
-- rebuildable at will from medialets.raw and the object store, and it
-- never crosses the wire — search is a client-API surface only;
-- envelope privacy (D-04) forbids any cross-domain search protocol.

CREATE TABLE object_text (              -- extracted media text, per URN
  urn TEXT PRIMARY KEY,                 -- content-addressed: extract once,
                                        -- shared across mailboxes; safe
                                        -- because results are scoped
                                        -- through refs/messages at query
  extractor TEXT NOT NULL,              -- registry name; 'none' = no
                                        -- extractor claimed it (negative
                                        -- cache so sweeps don't rework)
  text TEXT NOT NULL,                   -- capped plain text
  extracted_at TEXT NOT NULL
);

-- The full-text index. FTS4, not FTS5: the pinned mattn/go-sqlite3
-- default build ships FTS4 but requires a build tag for FTS5, and the
-- project's build/test commands stay tag-free on both Ubuntu and
-- Windows. unicode61 folds case and diacritics ('zilina' finds
-- 'Žilina'). kind/key are lookup columns, excluded from tokenization.
-- Rows: kind='medialet' (key=content_address, title=subject,
-- content=derived_text+media names) and kind='object' (key=urn,
-- title=media names, content=extracted text).
CREATE VIRTUAL TABLE search_fts USING fts4(
  kind, key, title, content,
  notindexed=kind, notindexed=key,
  tokenize=unicode61 "remove_diacritics=1"
);
