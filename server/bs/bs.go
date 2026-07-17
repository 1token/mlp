package bs

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zeebo/blake3"
	"medialet.org/mlp/bao"
	"medialet.org/mlp/core"
	"medialet.org/mlp/discovery"
)

// FreshnessWindow is the RECOMMENDED bound on the signature `created`
// parameter (§6.6 rule 3).
const FreshnessWindow = 300 * time.Second

// Problem mirrors the §8.5 failure taxonomy.
type Problem struct {
	Status int
	Code   string
	Detail string
}

func (p *Problem) Error() string { return fmt.Sprintf("%d %s: %s", p.Status, p.Code, p.Detail) }

func problemf(status int, code, format string, a ...any) *Problem {
	return &Problem{Status: status, Code: code, Detail: fmt.Sprintf(format, a...)}
}

// BS is one domain's Blob Store ingestion surface.
type BS struct {
	DB       *sql.DB
	Resolver *discovery.Resolver
	// Root holds quarantine/ (partials keyed by token hash, D-27)
	// and objects/ (live, keyed by URN digest).
	Root string
	// PublicBase reconstructs @target-uri: scheme://authority as the
	// pusher addressed us (the path comes from the request).
	PublicBase string
	Now        func() time.Time
	// OnVerified fires after an object goes live (both doors); the
	// API layer flips expected references to available (§10.3) and
	// notifies. Runs outside the finalize transaction.
	OnVerified func(urn string)

	mu     sync.Mutex
	active map[string]*resource
}

// resource is the per-reservation single-stream lock (D-30/D-76) and
// the in-memory BLAKE3 checkpoint. A nil hasher means the checkpoint
// must be re-derived from the partial — the partial file, truncated
// exactly at the durable offset, fully determines the hasher state,
// which is how checkpoints survive process restarts without a
// serializable hasher (D-27; zeebo/blake3 exposes Clone but no
// marshaling — the hasher_state column stays reserved).
type resource struct {
	mu      sync.Mutex
	hasher  *blake3.Hasher
	decoder *bao.Decoder // the §8.9 checkpoint twin of hasher
}

func (b *BS) now() time.Time {
	if b.Now != nil {
		return b.Now().UTC()
	}
	return time.Now().UTC()
}

func (b *BS) resource(tokenHash string) *resource {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.active == nil {
		b.active = map[string]*resource{}
	}
	r, ok := b.active[tokenHash]
	if !ok {
		r = &resource{}
		b.active[tokenHash] = r
	}
	return r
}

func (b *BS) quarantinePath(tokenHash string) string {
	return filepath.Join(b.Root, "quarantine", tokenHash)
}

// ObjectPath locates a live object's bytes (the intra-domain serving
// and owner-delete paths need it).
func (b *BS) ObjectPath(urn string) string {
	return filepath.Join(b.Root, "objects", strings.TrimPrefix(urn, "urn:mlet:"))
}

type reservation struct {
	tokenHash    string
	urn          string
	maxSize      int64
	pusherDomain string
	expires      time.Time
	state        string
	offset       int64
	encoding     string // 'raw' (§8.4) or 'mlp-bao' (§8.9); fixed at first PATCH
}

func tokenHash(token string) string {
	sum := blake3.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// loadReservation performs §8.4 step-1 token validity and expiry.
// effTotal is the resource's Upload-Length: content bytes for raw,
// the Annex D.3 encoded size for mlp-bao (§8.9: offsets and lengths
// are encoded-stream bytes).
func effTotal(res *reservation) int64 {
	if res.encoding == "mlp-bao" {
		return bao.EncodedSize(res.maxSize)
	}
	return res.maxSize
}

// encodingOf maps a PATCH Content-Type to a resource encoding.
func encodingOf(ct string) (string, bool) {
	switch ct {
	case "application/offset+octet-stream":
		return "raw", true
	case "application/mlp-bao":
		return "mlp-bao", true
	}
	return "", false
}

func (b *BS) loadReservation(ctx context.Context, token string, now time.Time) (*reservation, *Problem) {
	if token == "" {
		return nil, problemf(http.StatusUnauthorized, "reservation-invalid", "missing MLP-Reservation header (§8.2)")
	}
	r := &reservation{tokenHash: tokenHash(token)}
	var expires string
	err := b.DB.QueryRowContext(ctx,
		`SELECT urn, max_size, pusher_domain, expires, state, upload_offset, encoding
		 FROM reservations_in WHERE token_hash=?`, r.tokenHash).
		Scan(&r.urn, &r.maxSize, &r.pusherDomain, &expires, &r.state, &r.offset, &r.encoding)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, problemf(http.StatusUnauthorized, "reservation-invalid", "unknown reservation token (D-18)")
	case err != nil:
		return nil, problemf(http.StatusInternalServerError, "reservation-invalid", "store: %v", err)
	}
	if r.state == "consumed" {
		return nil, problemf(http.StatusGone, "reservation-invalid", "token already consumed (D-18)")
	}
	r.expires, err = time.Parse(time.RFC3339, expires)
	if err != nil {
		return nil, problemf(http.StatusInternalServerError, "reservation-invalid", "stored expiry malformed")
	}
	if r.state == "expired" || now.After(r.expires) {
		if r.state != "expired" {
			b.DB.ExecContext(ctx, `UPDATE reservations_in SET state='expired' WHERE token_hash=? AND state='pending'`, r.tokenHash)
			os.Remove(b.quarantinePath(r.tokenHash)) // GC at expiry (D-27)
		}
		return nil, problemf(http.StatusGone, "reservation-expired", "reservation expired %s (§8.5)", expires)
	}
	return r, nil
}

// verifySignature performs §8.4 step-1 header-only verification: the
// §6.6 profile against the pusher domain's bs-role keys (D-22 binds
// the reservation to the pusher identity).
func (b *BS) verifySignature(ctx context.Context, res *reservation, method, targetURI string, header func(string) string, hasBody bool, now time.Time) *Problem {
	si, err := ParseSignatureInput(header("signature-input"))
	if err != nil {
		return problemf(http.StatusUnauthorized, "signature-invalid", "%v", err)
	}
	want := headComponents
	if hasBody {
		want = bodyComponents
	}
	if !componentsEqual(si.Components, want) {
		return problemf(http.StatusUnauthorized, "signature-invalid",
			"covered components differ from the D-66 profile")
	}
	if d := now.Unix() - si.Created; d > int64(FreshnessWindow/time.Second) || d < -int64(FreshnessWindow/time.Second) {
		return problemf(http.StatusUnauthorized, "signature-invalid",
			"created=%d outside the %s window (§6.6)", si.Created, FreshnessWindow)
	}
	sig, err := ParseSignature(header("signature"))
	if err != nil {
		return problemf(http.StatusUnauthorized, "signature-invalid", "%v", err)
	}
	base, err := BuildBase(method, targetURI, header, si)
	if err != nil {
		return problemf(http.StatusUnauthorized, "signature-invalid", "%v", err)
	}
	entry, err := b.Resolver.ResolveKID(ctx, res.pusherDomain, si.KeyID)
	if err != nil {
		if errors.Is(err, discovery.ErrUnknownKID) {
			return problemf(http.StatusUnauthorized, "signature-invalid", "%v", err)
		}
		return problemf(http.StatusBadGateway, "signature-invalid", "discovery: %v", err)
	}
	if !entry.HasRole("bs") || !entry.ValidAt(now) {
		return problemf(http.StatusUnauthorized, "signature-invalid",
			"key %s of %s lacks the bs role or is outside its window (§6.6)", si.KeyID, res.pusherDomain)
	}
	pub, err := entry.Public()
	if err != nil {
		return problemf(http.StatusUnauthorized, "signature-invalid", "%v", err)
	}
	if !verifyEd25519(pub, base, sig) {
		return problemf(http.StatusUnauthorized, "signature-invalid", "RFC 9421 verification failed (§6.6)")
	}
	return nil
}

// Head implements §8.3: the durable checkpoint, never a state change.
func (b *BS) Head(ctx context.Context, token, targetURI string, header func(string) string) (offset, length int64, expires time.Time, prob *Problem) {
	now := b.now()
	res, prob := b.loadReservation(ctx, token, now)
	if prob != nil {
		return 0, 0, time.Time{}, prob
	}
	if prob := b.verifySignature(ctx, res, "HEAD", targetURI, header, false, now); prob != nil {
		return 0, 0, time.Time{}, prob
	}
	return res.offset, effTotal(res), res.expires, nil
}

// Patch implements the §8.4 transactional pipeline. On success it
// returns the new durable offset and whether completion verified the
// object.
func (b *BS) Patch(ctx context.Context, token, targetURI string, header func(string) string, body io.Reader) (offset int64, verified bool, prob *Problem) {
	now := b.now()
	res, prob := b.loadReservation(ctx, token, now)
	if prob != nil {
		return 0, false, prob
	}
	// Step 1 continued: header-only signature verification —
	// possible before any body byte because the signature covers the
	// *claimed* Content-Digest (D-77). Failures here consume nothing.
	if prob := b.verifySignature(ctx, res, "PATCH", targetURI, header, true, now); prob != nil {
		return 0, false, prob
	}
	claimed, prob := parseContentDigest(header("content-digest"))
	if prob != nil {
		return 0, false, prob
	}
	reqOffset, err := strconv.ParseInt(header("upload-offset"), 10, 64)
	if err != nil || reqOffset < 0 {
		return 0, false, problemf(http.StatusConflict, "offset-mismatch", "malformed Upload-Offset (§8.4)")
	}
	// §8.9: the first PATCH fixes the resource's encoding; every
	// later PATCH must match (offsets are not comparable across
	// encodings). Unknown types were already 415'd at the HTTP layer.
	if enc, ok := encodingOf(header("Content-Type")); ok && enc != res.encoding {
		if res.offset != 0 {
			return res.offset, false, problemf(http.StatusUnsupportedMediaType, "malformed",
				"encoding switch mid-push: resource is %s (§8.9)", res.encoding)
		}
		if _, err := b.DB.ExecContext(ctx,
			`UPDATE reservations_in SET encoding=? WHERE token_hash=?`, enc, res.tokenHash); err != nil {
			return 0, false, problemf(http.StatusInternalServerError, "reservation-invalid", "store: %v", err)
		}
		res.encoding = enc
	}
	return b.patchCore(ctx, res, claimed, reqOffset, body)
}

// LocalPatch is the intra-domain ingestion door (D-79/D-135: one
// code path): the same transactional pipeline as Patch, minus the
// RFC 9421 layer — authentication is the caller's business (the
// Client API's session). The reservation still gates everything.
func (b *BS) LocalPatch(ctx context.Context, token string, claimed []byte, reqOffset int64, body io.Reader) (offset int64, verified bool, prob *Problem) {
	res, prob := b.loadReservation(ctx, token, b.now())
	if prob != nil {
		return 0, false, prob
	}
	return b.patchCore(ctx, res, claimed, reqOffset, body)
}

// LocalHead mirrors Head for the intra-domain door.
func (b *BS) LocalHead(ctx context.Context, token string) (offset, length int64, prob *Problem) {
	res, prob := b.loadReservation(ctx, token, b.now())
	if prob != nil {
		return 0, 0, prob
	}
	return res.offset, res.maxSize, nil
}

// patchCore is the §8.4 step 2+ transactional pipeline shared by the
// federated and intra-domain doors.
func (b *BS) patchCore(ctx context.Context, res *reservation, claimed []byte, reqOffset int64, body io.Reader) (offset int64, verified bool, prob *Problem) {
	// One PATCH in flight per resource (D-30/D-76).
	rsc := b.resource(res.tokenHash)
	if !rsc.mu.TryLock() {
		return 0, false, problemf(http.StatusConflict, "offset-mismatch", "overlapping PATCH (§8.2 rule 5)")
	}
	defer rsc.mu.Unlock()

	if reqOffset != res.offset {
		return res.offset, false, problemf(http.StatusConflict, "offset-mismatch",
			"Upload-Offset %d, resource at %d (§8.4)", reqOffset, res.offset)
	}

	// Open the partial at the checkpoint; reconcile crash residue.
	f, hasher, prob := b.openCheckpoint(res, rsc)
	if prob != nil {
		return res.offset, false, prob
	}
	defer f.Close()

	// Step 2: stream to quarantine, advancing both digests; a
	// cumulative size beyond the effective total aborts mid-request
	// (D-18/D-27). The declared size is exact (§8.2 rule 3), so
	// overrun is object-level wrongness: the bytes cannot match the
	// URN. For mlp-bao (§8.9) the verifier is the incremental
	// decoder: every parent node and chunk group is checked the
	// moment it completes, and a failure is the source-wrong taxon
	// detected early — reset to zero, 422 bao-verify-failed.
	var workingH *blake3.Hasher
	var workingD *bao.Decoder
	var verifySink io.Writer
	if res.encoding == "mlp-bao" {
		workingD = rsc.decoder.Clone() // the checkpoint itself stays untouched
		verifySink = workingD
	} else {
		workingH = hasher.Clone()
		verifySink = workingH
	}
	total := effTotal(res)
	sha := sha256.New()
	remaining := total - res.offset
	n, err := io.Copy(io.MultiWriter(f, sha, verifySink), io.LimitReader(body, remaining))
	if err != nil {
		if errors.Is(err, bao.ErrVerify) {
			f.Truncate(0)
			b.resetToZero(ctx, res, rsc)
			return 0, false, problemf(http.StatusUnprocessableEntity, "bao-verify-failed",
				"%v — re-push from 0 (§8.9)", err)
		}
		f.Truncate(res.offset) // roll back to the checkpoint
		return res.offset, false, problemf(http.StatusBadRequest, "digest-mismatch", "body read: %v", err)
	}
	if extra, _ := io.Copy(io.Discard, io.LimitReader(body, 1)); extra > 0 {
		f.Truncate(0)
		b.resetToZero(ctx, res, rsc)
		return 0, false, problemf(http.StatusUnprocessableEntity, "hash-mismatch",
			"body exceeds the exact declared size %d (§8.4 step 2)", total)
	}

	// Step 3: request-level digest.
	if got := sha.Sum(nil); !bytes.Equal(got, claimed) {
		f.Truncate(res.offset) // partial truncates, checkpoint stands (D-77)
		return res.offset, false, problemf(http.StatusUnprocessableEntity, "digest-mismatch",
			"Content-Digest mismatch — retry this offset (§8.5)")
	}

	// Durable checkpoint: bytes first, then the offset record.
	if err := f.Sync(); err != nil {
		f.Truncate(res.offset)
		return res.offset, false, problemf(http.StatusInternalServerError, "digest-mismatch", "fsync: %v", err)
	}
	newOffset := res.offset + n
	if _, err := b.DB.ExecContext(ctx,
		`UPDATE reservations_in SET upload_offset=? WHERE token_hash=?`, newOffset, res.tokenHash); err != nil {
		f.Truncate(res.offset)
		return res.offset, false, problemf(http.StatusInternalServerError, "digest-mismatch", "store: %v", err)
	}
	rsc.hasher, rsc.decoder = workingH, workingD // offset and verifier state advance together

	if newOffset < total {
		return newOffset, false, nil
	}

	// Completion. Raw (§8.4): finalize BLAKE3 against the URN.
	// mlp-bao (§8.9): the walk already checked the topmost node
	// root-finalized against the URN's digest — Complete() IS the
	// URN comparison; an incomplete walk at full length is a
	// malformed encoding, the same source-wrong taxon.
	if res.encoding == "mlp-bao" {
		if !workingD.Complete() {
			f.Truncate(0)
			b.resetToZero(ctx, res, rsc)
			return 0, false, problemf(http.StatusUnprocessableEntity, "bao-verify-failed",
				"encoded stream ends unverified — re-push from 0 (§8.9)")
		}
	} else {
		wantDigest, err := core.ParseURNMlet(res.urn)
		if err != nil {
			return newOffset, false, problemf(http.StatusInternalServerError, "hash-mismatch", "stored urn: %v", err)
		}
		if got := workingH.Sum(nil); !bytes.Equal(got, wantDigest) {
			f.Truncate(0)
			b.resetToZero(ctx, res, rsc)
			return 0, false, problemf(http.StatusUnprocessableEntity, "hash-mismatch",
				"object does not verify against %s — re-push from 0 (§8.5, D-27)", res.urn)
		}
	}
	// Windows cannot rename a file that holds an open handle (POSIX
	// renames open files routinely): release ours before the promote.
	// The bytes are already durable — f.Sync() ran at the checkpoint
	// above — so the early close loses nothing; the handler's
	// deferred Close becomes a harmless double-close.
	if err := f.Close(); err != nil {
		return newOffset, false, problemf(http.StatusInternalServerError, "hash-mismatch", "close: %v", err)
	}
	srcPath := b.quarantinePath(res.tokenHash)
	if res.encoding == "mlp-bao" {
		// Decode the verified encoded partial into the raw object —
		// re-verifying as a side effect — then promote the raw file.
		// Close-before-promote per D-257 throughout.
		rawPath := srcPath + ".raw"
		if prob := b.decodeToRaw(res, srcPath, rawPath); prob != nil {
			b.resetToZero(ctx, res, rsc)
			return 0, false, prob
		}
		srcPath = rawPath
	}
	if prob := b.finalize(ctx, res, b.now(), srcPath); prob != nil {
		return newOffset, false, prob
	}
	if res.encoding == "mlp-bao" {
		os.Remove(b.quarantinePath(res.tokenHash)) // encoded partial: tidiness
	}
	rsc.hasher, rsc.decoder = nil, nil
	return newOffset, true, nil
}

// decodeToRaw turns a fully verified mlp-bao partial into the raw
// object bytes, verifying again on the way (the decoder releases
// only verified groups — Annex D.3). The output file is closed
// before return so finalize can rename it under Windows semantics
// (D-257).
func (b *BS) decodeToRaw(res *reservation, encPath, rawPath string) *Problem {
	digest, err := core.ParseURNMlet(res.urn)
	if err != nil {
		return problemf(http.StatusInternalServerError, "bao-verify-failed", "stored urn: %v", err)
	}
	var root [32]byte
	copy(root[:], digest)
	enc, err := os.Open(encPath)
	if err != nil {
		return problemf(http.StatusInternalServerError, "bao-verify-failed", "open: %v", err)
	}
	defer enc.Close()
	out, err := os.OpenFile(rawPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return problemf(http.StatusInternalServerError, "bao-verify-failed", "raw: %v", err)
	}
	d := bao.NewDecoder(root, res.maxSize)
	d.Groups = func(g []byte) error { _, werr := out.Write(g); return werr }
	if _, err := io.Copy(d, enc); err != nil || !d.Complete() {
		out.Close()
		os.Remove(rawPath)
		return problemf(http.StatusUnprocessableEntity, "bao-verify-failed", "decode: %v", err)
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return problemf(http.StatusInternalServerError, "bao-verify-failed", "fsync: %v", err)
	}
	if err := out.Close(); err != nil {
		return problemf(http.StatusInternalServerError, "bao-verify-failed", "close: %v", err)
	}
	return nil
}

// openCheckpoint opens the partial, reconciles it with the durable
// offset (crash tolerance), and ensures the in-memory hasher matches
// — re-deriving it from the partial when absent.
func (b *BS) openCheckpoint(res *reservation, rsc *resource) (*os.File, *blake3.Hasher, *Problem) {
	path := b.quarantinePath(res.tokenHash)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, nil, problemf(http.StatusInternalServerError, "digest-mismatch", "quarantine: %v", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, nil, problemf(http.StatusInternalServerError, "digest-mismatch", "quarantine: %v", err)
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, problemf(http.StatusInternalServerError, "digest-mismatch", "quarantine: %v", err)
	}
	switch {
	case st.Size() > res.offset:
		// Rolled-back or torn bytes beyond the checkpoint: drop them.
		if err := f.Truncate(res.offset); err != nil {
			f.Close()
			return nil, nil, problemf(http.StatusInternalServerError, "digest-mismatch", "truncate: %v", err)
		}
	case st.Size() < res.offset:
		// The durable record outran the file (lost storage): degrade
		// gracefully to zero — never wrong answers (§8.6).
		f.Truncate(0)
		res.offset = 0
		rsc.hasher = nil
		b.DB.Exec(`UPDATE reservations_in SET upload_offset=0 WHERE token_hash=?`, res.tokenHash)
	}
	if res.encoding == "mlp-bao" {
		// The decoder checkpoint re-derives exactly like the hasher:
		// the partial's verified prefix fully determines it (D-27
		// discipline; the state is a small CV stack, not a marshaled
		// hasher). A prefix that no longer parses is lost storage —
		// degrade to zero, never wrong answers (§8.6).
		if rsc.decoder == nil {
			digest, err := core.ParseURNMlet(res.urn)
			if err != nil {
				f.Close()
				return nil, nil, problemf(http.StatusInternalServerError, "digest-mismatch", "stored urn: %v", err)
			}
			var root [32]byte
			copy(root[:], digest)
			d := bao.NewDecoder(root, res.maxSize)
			if res.offset > 0 {
				if _, err := io.Copy(d, io.NewSectionReader(f, 0, res.offset)); err != nil {
					f.Truncate(0)
					res.offset = 0
					b.DB.Exec(`UPDATE reservations_in SET upload_offset=0 WHERE token_hash=?`, res.tokenHash)
					d = bao.NewDecoder(root, res.maxSize)
				}
			}
			rsc.decoder = d
		}
	} else if rsc.hasher == nil {
		h := blake3.New()
		if res.offset > 0 {
			if _, err := io.Copy(h, io.NewSectionReader(f, 0, res.offset)); err != nil {
				f.Close()
				return nil, nil, problemf(http.StatusInternalServerError, "digest-mismatch", "re-derive: %v", err)
			}
		}
		rsc.hasher = h
	}
	if _, err := f.Seek(res.offset, io.SeekStart); err != nil {
		f.Close()
		return nil, nil, problemf(http.StatusInternalServerError, "digest-mismatch", "seek: %v", err)
	}
	return f, rsc.hasher, nil
}

// resetToZero implements the hash-mismatch reset (§8.4/§8.5): the
// partial is discarded; the unexpired Reservation survives at offset
// 0 for a clean re-push.
func (b *BS) resetToZero(ctx context.Context, res *reservation, rsc *resource) {
	// Both call sites Truncate(0) the open partial first, so this
	// Remove is tidiness, not correctness: on Windows it fails while
	// the handle is open (error discarded) and the empty file is
	// simply reused by the next attempt's O_CREATE|O_RDWR open.
	os.Remove(b.quarantinePath(res.tokenHash))
	rsc.hasher = nil
	rsc.decoder = nil
	res.offset = 0
	b.DB.ExecContext(ctx, `UPDATE reservations_in SET upload_offset=0 WHERE token_hash=?`, res.tokenHash)
}

// finalize moves the verified object out of quarantine, records it
// live, and consumes the token (single use, D-18 — the trigger makes
// consumed terminal).
func (b *BS) finalize(ctx context.Context, res *reservation, now time.Time, src string) *Problem {
	obj := b.ObjectPath(res.urn)
	if err := os.MkdirAll(filepath.Dir(obj), 0o700); err != nil {
		return problemf(http.StatusInternalServerError, "hash-mismatch", "objects: %v", err)
	}
	if err := os.Rename(src, obj); err != nil {
		// Content-addressed idempotency: if the object already sits
		// at its address (a racing push of the same URN finished
		// first), the rename's work is done. POSIX replace-renames
		// atomically; Windows refuses — but both sources verified
		// against the URN's BLAKE3, so the bytes are identical by
		// construction and the loser's partial can simply go.
		if _, statErr := os.Stat(obj); statErr != nil {
			return problemf(http.StatusInternalServerError, "hash-mismatch", "promote: %v", err)
		}
		os.Remove(src)
	}
	nowS := now.Format(time.RFC3339)
	tx, err := b.DB.BeginTx(ctx, nil)
	if err != nil {
		return problemf(http.StatusInternalServerError, "hash-mismatch", "store: %v", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO objects (urn, size, state, store_id, created_at, verified_at)
		 VALUES (?,?,'live',1,?,?)
		 ON CONFLICT(urn) DO UPDATE SET state='live', verified_at=excluded.verified_at`,
		res.urn, res.maxSize, nowS, nowS); err != nil {
		return problemf(http.StatusInternalServerError, "hash-mismatch", "store: %v", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE reservations_in SET state='consumed' WHERE token_hash=?`, res.tokenHash); err != nil {
		return problemf(http.StatusInternalServerError, "hash-mismatch", "store: %v", err)
	}
	if err := tx.Commit(); err != nil {
		return problemf(http.StatusInternalServerError, "hash-mismatch", "store: %v", err)
	}
	if b.OnVerified != nil {
		b.OnVerified(res.urn)
	}
	return nil
}

// parseContentDigest extracts the sha-256 byte sequence from an
// RFC 9530 Content-Digest header.
func parseContentDigest(h string) ([]byte, *Problem) {
	v, ok := strings.CutPrefix(strings.TrimSpace(h), "sha-256=:")
	if !ok || !strings.HasSuffix(v, ":") {
		return nil, problemf(http.StatusBadRequest, "digest-mismatch",
			"Content-Digest must be sha-256 (§6.6 rule 4)")
	}
	raw, err := stdBase64(v[:len(v)-1])
	if err != nil || len(raw) != sha256.Size {
		return nil, problemf(http.StatusBadRequest, "digest-mismatch", "malformed Content-Digest value")
	}
	return raw, nil
}

func stdBase64(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(s) }
func toBase64(b []byte) string           { return base64.StdEncoding.EncodeToString(b) }

func verifyEd25519(pub ed25519.PublicKey, base string, sig []byte) bool {
	return len(pub) == ed25519.PublicKeySize && len(sig) == ed25519.SignatureSize &&
		ed25519.Verify(pub, []byte(base), sig)
}
