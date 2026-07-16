package search

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode"
)

// Indexer maintains the search_fts index. It is deliberately
// self-healing: object rows are written at the OnVerified moment
// (and by Reindex sweeps), medialet rows are synced lazily before
// every query — so a node that never ran an indexer still searches
// correctly the first time someone asks.
type Indexer struct {
	DB *sql.DB
	// Open returns the bytes of a live object; wired to the blob
	// store's ObjectPath in the node composition. nil disables media
	// extraction (medialet text still indexes).
	Open func(urn string) (io.ReadCloser, error)
	// Extractors defaults to Builtin() when nil.
	Extractors []Extractor
	Now        func() time.Time // defaults to time.Now
}

func (ix *Indexer) extractors() []Extractor {
	if ix.Extractors != nil {
		return ix.Extractors
	}
	return Builtin()
}

func (ix *Indexer) now() time.Time {
	if ix.Now != nil {
		return ix.Now()
	}
	return time.Now()
}

// IndexObject extracts text from one live object and (re)writes its
// index rows. The extraction result is cached per URN in object_text —
// content-addressed, so one extraction serves every mailbox that holds
// the object; scoping happens at query time through refs/messages.
// A media type is taken from the refs rows (the declared Manifest
// contract); the first extractor that claims any declared (type, name)
// wins. No claim is recorded as extractor='none' so sweeps don't
// rework the object.
func (ix *Indexer) IndexObject(ctx context.Context, urn string) error {
	if ix.Open == nil {
		return nil
	}
	rows, err := ix.DB.QueryContext(ctx,
		`SELECT DISTINCT type, COALESCE(name,'') FROM refs WHERE urn=?`, urn)
	if err != nil {
		return err
	}
	type tn struct{ typ, name string }
	var cands []tn
	for rows.Next() {
		var c tn
		if err := rows.Scan(&c.typ, &c.name); err != nil {
			rows.Close()
			return err
		}
		cands = append(cands, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	if len(cands) == 0 {
		// Not referenced by any mailbox yet (a sender-side upload
		// verifies before the send creates its promise rows): write
		// nothing — SyncObjects picks the object up once refs exist.
		return nil
	}

	var chosen Extractor
	for _, e := range ix.extractors() {
		for _, c := range cands {
			if e.Claims(c.typ, c.name) {
				chosen = e
				break
			}
		}
		if chosen != nil {
			break
		}
	}

	name, text := "none", ""
	if chosen != nil {
		rc, err := ix.Open(urn)
		if err != nil {
			return fmt.Errorf("open %s: %w", urn, err)
		}
		data, err := io.ReadAll(io.LimitReader(rc, MaxInput))
		rc.Close()
		if err != nil {
			return fmt.Errorf("read %s: %w", urn, err)
		}
		name = chosen.Name()
		if text, err = chosen.Extract(data); err != nil {
			// A declared type the bytes don't honor is not an
			// indexing failure — record the attempt, index nothing.
			name, text = name+":error", ""
		}
	}

	names := make([]string, 0, len(cands))
	for _, c := range cands {
		if c.name != "" {
			names = append(names, c.name)
		}
	}
	title := strings.Join(names, " ")

	tx, err := ix.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO object_text (urn, extractor, text, extracted_at) VALUES (?,?,?,?)
		 ON CONFLICT(urn) DO UPDATE SET extractor=excluded.extractor,
		   text=excluded.text, extracted_at=excluded.extracted_at`,
		urn, name, text, ix.now().UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM search_fts WHERE kind='object' AND key=?`, urn); err != nil {
		return err
	}
	if text != "" || title != "" {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO search_fts (kind, key, title, content) VALUES ('object',?,?,?)`,
			urn, title, text); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SyncObjects extracts live, referenced objects the index has not
// attempted yet. With IndexObject writing a row for every attempt
// (extractor='none' when nothing claims the declared type), this
// sweep converges: it is the self-heal that catches sender-side
// copies (referenced only after upload verified) and restores.
func (ix *Indexer) SyncObjects(ctx context.Context) error {
	if ix.Open == nil {
		return nil
	}
	rows, err := ix.DB.QueryContext(ctx,
		`SELECT o.urn FROM objects o WHERE o.state='live'
		   AND EXISTS (SELECT 1 FROM refs r WHERE r.urn=o.urn)
		   AND NOT EXISTS (SELECT 1 FROM object_text x WHERE x.urn=o.urn)`)
	if err != nil {
		return err
	}
	var urns []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			rows.Close()
			return err
		}
		urns = append(urns, u)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, u := range urns {
		if err := ix.IndexObject(ctx, u); err != nil {
			return err
		}
	}
	return nil
}

// medialetDoc is the slice of the Signed Medialet the index needs.
type medialetDoc struct {
	Medialet struct {
		Subject  string `json:"subject"`
		Manifest []struct {
			Name string `json:"name"`
		} `json:"manifest"`
	} `json:"medialet"`
}

// SyncMedialets mirrors medialets rows the index has not seen yet:
// title=subject, content=derived_text plus media names (so a filename
// query finds the message). Runs before every query — cheap, and it
// makes the index self-healing after restores or Reindex.
func (ix *Indexer) SyncMedialets(ctx context.Context) error {
	rows, err := ix.DB.QueryContext(ctx,
		`SELECT content_address, raw, COALESCE(derived_text,'') FROM medialets
		 WHERE content_address NOT IN (SELECT key FROM search_fts WHERE kind='medialet')`)
	if err != nil {
		return err
	}
	type row struct{ ca, title, content string }
	var todo []row
	for rows.Next() {
		var ca, derived string
		var raw []byte
		if err := rows.Scan(&ca, &raw, &derived); err != nil {
			rows.Close()
			return err
		}
		var doc medialetDoc
		_ = json.Unmarshal(raw, &doc) // absent fields index as empty
		parts := []string{derived}
		for _, m := range doc.Medialet.Manifest {
			if m.Name != "" {
				parts = append(parts, m.Name)
			}
		}
		todo = append(todo, row{ca, doc.Medialet.Subject, strings.TrimSpace(strings.Join(parts, "\n"))})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range todo {
		if _, err := ix.DB.ExecContext(ctx,
			`INSERT INTO search_fts (kind, key, title, content) VALUES ('medialet',?,?,?)`,
			r.ca, r.title, r.content); err != nil {
			return err
		}
	}
	return nil
}

// Reindex rebuilds everything: the FTS table is dropped to empty, the
// object_text cache is cleared, medialets resync, and every live
// object with at least one reference is extracted again.
func (ix *Indexer) Reindex(ctx context.Context) error {
	if _, err := ix.DB.ExecContext(ctx, `DELETE FROM search_fts`); err != nil {
		return err
	}
	if _, err := ix.DB.ExecContext(ctx, `DELETE FROM object_text`); err != nil {
		return err
	}
	if err := ix.SyncMedialets(ctx); err != nil {
		return err
	}
	rows, err := ix.DB.QueryContext(ctx,
		`SELECT o.urn FROM objects o WHERE o.state='live'
		   AND EXISTS (SELECT 1 FROM refs r WHERE r.urn=o.urn)`)
	if err != nil {
		return err
	}
	var urns []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			rows.Close()
			return err
		}
		urns = append(urns, u)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, u := range urns {
		if err := ix.IndexObject(ctx, u); err != nil {
			return err
		}
	}
	return nil
}

// MatchExpr sanitizes a user query into FTS4 MATCH syntax: lowercased
// bare terms (so OR/NOT/NEAR stay words, not operators), punctuation
// stripped, implicit AND, with a trailing * preserved as a prefix
// query. Returns "" when nothing searchable remains.
func MatchExpr(q string) string {
	var terms []string
	for _, f := range strings.Fields(strings.ToLower(q)) {
		prefix := strings.HasSuffix(f, "*")
		var b strings.Builder
		for _, r := range f {
			if isWord(r) {
				b.WriteRune(r)
			}
		}
		t := b.String()
		if t == "" {
			continue
		}
		if prefix {
			t += "*"
		}
		terms = append(terms, t)
	}
	return strings.Join(terms, " ")
}

// isWord approximates unicode61's alphanumeric class: unicode61
// tokenizes on non-alphanumerics, so query terms keep letters and
// digits and drop everything else.
func isWord(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsNumber(r)
}
