package discovery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// CacheCeiling is the absolute Domain Document cache ceiling,
// regardless of served cache headers (§5.5, D-33). Revocation latency
// is bounded by it.
const CacheCeiling = 24 * time.Hour

// NegativeTTL is the RECOMMENDED brief negative cache for failed
// discovery (§5.5).
const NegativeTTL = 5 * time.Minute

// Resolver resolves domains to validated Domain Documents, caching in
// the domain_docs/domain_keys tables (store migration 0001) under the
// 24-hour ceiling. It is safe for concurrent use.
type Resolver struct {
	DB        *sql.DB
	Fetcher   *Fetcher
	Supported []string         // protocol versions; default ["0.1"]
	Now       func() time.Time // test hook; defaults to time.Now

	mu       sync.Mutex
	negative map[string]time.Time // domain -> negative-cache expiry
}

// NewResolver returns a Resolver over db using the production
// hardened Fetcher.
func NewResolver(db *sql.DB) *Resolver {
	return &Resolver{DB: db, Fetcher: NewFetcher(), Supported: []string{"0.1"}}
}

func (r *Resolver) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

// normalizeDomain lowercases and shape-checks a domain. Input is
// expected in A-label form (§5.1); IDN conversion happens at the
// client edge (§4.3).
func normalizeDomain(domain string) (string, error) {
	d := strings.ToLower(strings.TrimSpace(domain))
	if d == "" || strings.ContainsAny(d, "/:@ \t\r\n") ||
		strings.HasPrefix(d, ".") || strings.HasSuffix(d, ".") {
		return "", fmt.Errorf("mlp/discovery: malformed domain %q", domain)
	}
	return d, nil
}

// Resolve returns the validated Domain Document for domain, from
// cache when fresh (§5.5), otherwise via a hardened fetch.
func (r *Resolver) Resolve(ctx context.Context, domain string) (*Document, error) {
	doc, _, err := r.resolve(ctx, domain, false)
	return doc, err
}

// ResolveKID returns the key entry for (domain, kid). Encountering an
// unknown kid on a cached document forces a re-fetch (§5.5 MUST)
// before the kid is declared unknown. The returned entry has already
// passed kid self-verification (§6.2).
func (r *Resolver) ResolveKID(ctx context.Context, domain, kid string) (*KeyEntry, error) {
	doc, cached, err := r.resolve(ctx, domain, false)
	if err != nil {
		return nil, err
	}
	if e := findKID(doc, kid); e != nil {
		return e, nil
	}
	if cached {
		if doc, _, err = r.resolve(ctx, domain, true); err != nil {
			return nil, err
		}
		if e := findKID(doc, kid); e != nil {
			return e, nil
		}
	}
	return nil, fmt.Errorf("%w: %s for %s after re-fetch (§5.5)", ErrUnknownKID, kid, domain)
}

func findKID(doc *Document, kid string) *KeyEntry {
	for i := range doc.Keys {
		if doc.Keys[i].KID == kid {
			return &doc.Keys[i]
		}
	}
	return nil
}

// ErrUnknownKID reports a kid absent from a domain's key set even
// after the §5.5 forced re-fetch. Consumers map it to
// signature-invalid (§7.3), as opposed to resolution failures, which
// map to discovery-failed.
var ErrUnknownKID = errors.New("mlp/discovery: unknown kid")

// resolve is the single resolution path. force bypasses the positive
// cache (unknown-kid re-fetch); the negative cache is honored in all
// paths to keep re-fetches from hammering failing domains (§5.5).
// The second return reports whether the document came from cache.
func (r *Resolver) resolve(ctx context.Context, domain string, force bool) (*Document, bool, error) {
	d, err := normalizeDomain(domain)
	if err != nil {
		return nil, false, err
	}
	now := r.now()

	r.mu.Lock()
	until, negcached := r.negative[d]
	r.mu.Unlock()
	if negcached && now.Before(until) {
		return nil, false, fmt.Errorf("%w: %s until %s", errNegativeCached, d, until.Format(time.RFC3339))
	}

	if !force {
		var raw, expires string
		err := r.DB.QueryRowContext(ctx,
			`SELECT doc, expires_at FROM domain_docs WHERE domain = ?`, d).Scan(&raw, &expires)
		switch {
		case err == nil:
			if exp, perr := time.Parse(time.RFC3339, expires); perr == nil && now.Before(exp) {
				// Re-validate on load: the cache is data, not authority.
				doc, perr := ParseDocument([]byte(raw), d, r.supported())
				if perr == nil {
					return doc, true, nil
				}
				// A cached document that no longer validates falls
				// through to a fresh fetch.
			}
		case errors.Is(err, sql.ErrNoRows):
			// fall through to fetch
		default:
			return nil, false, fmt.Errorf("mlp/discovery: cache read: %w", err)
		}
	}

	body, hdr, err := r.Fetcher.FetchDomainDocument(ctx, d)
	if err != nil {
		r.setNegative(d, now)
		return nil, false, err
	}
	doc, err := ParseDocument(body, d, r.supported())
	if err != nil {
		r.setNegative(d, now)
		return nil, false, err
	}

	ttl := freshness(hdr, CacheCeiling)
	if ttl > CacheCeiling {
		ttl = CacheCeiling // D-33: absolute ceiling
	}
	if err := r.storeCache(ctx, d, body, doc, now, now.Add(ttl)); err != nil {
		return nil, false, err
	}
	r.mu.Lock()
	delete(r.negative, d)
	r.mu.Unlock()
	return doc, false, nil
}

func (r *Resolver) supported() []string {
	if len(r.Supported) == 0 {
		return []string{"0.1"}
	}
	return r.Supported
}

func (r *Resolver) setNegative(domain string, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.negative == nil {
		r.negative = map[string]time.Time{}
	}
	r.negative[domain] = now.Add(NegativeTTL)
}

// storeCache persists the fetched document and its surviving key set
// atomically (schema conventions per D-192: RFC 3339 TEXT timestamps,
// JSON-as-TEXT roles).
func (r *Resolver) storeCache(ctx context.Context, domain string, raw []byte, doc *Document, fetched, expires time.Time) error {
	tx, err := r.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("mlp/discovery: cache write: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO domain_docs (domain, doc, fetched_at, expires_at) VALUES (?,?,?,?)
		 ON CONFLICT(domain) DO UPDATE SET doc=excluded.doc,
		   fetched_at=excluded.fetched_at, expires_at=excluded.expires_at`,
		domain, string(raw),
		fetched.Format(time.RFC3339), expires.Format(time.RFC3339)); err != nil {
		return fmt.Errorf("mlp/discovery: cache write: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM domain_keys WHERE domain = ?`, domain); err != nil {
		return fmt.Errorf("mlp/discovery: cache write: %w", err)
	}
	for _, e := range doc.Keys {
		roles, err := json.Marshal(e.Roles)
		if err != nil {
			return fmt.Errorf("mlp/discovery: cache write: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO domain_keys (domain, kid, key, roles, not_before, not_after)
			 VALUES (?,?,?,?,?,?)`,
			domain, e.KID, e.Key, string(roles),
			nullable(e.NotBefore), nullable(e.NotAfter)); err != nil {
			return fmt.Errorf("mlp/discovery: cache write: %w", err)
		}
	}
	return tx.Commit()
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
