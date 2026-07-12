package sn

// Garbage collection under the §10.5 invariants (D-88), with the
// D-139 class as the sweep's policy: a live object is collected by
// this pass only when NO local reference is pinned (invariant 1/2)
// and either every reference is already unavailable (invariant 3 —
// immediately collectable) or every non-terminal reference carries
// the ephemeral class (D-139 GC-first: policy admitted these bytes,
// policy may reclaim them). Standard-class collection under quota
// pressure is operator policy, outside this sweep (OPERATOR.md).
//
// The collection is atomic per object: every `available` reference
// flips to unavailable(expired-local) in the same transaction that
// removes the object row; the file leaves the disk only after
// commit. A crash between commit and unlink leaves an orphan file —
// re-running the sweep is harmless, and the object row (the truth)
// is already gone.

import (
	"context"
	"net/http"
	"os"
	"time"
)

// Collected reports one GC pass's removals.
type Collected struct {
	URN  string
	Size int64
}

// CollectGarbage runs one D-139 sweep. The BS pointer supplies file
// paths; a nil bs skips unlinking (tests without object bytes).
func (s *SN) CollectGarbage(ctx context.Context, bs interface{ ObjectPath(string) string }, now time.Time) ([]Collected, *Problem) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT o.urn, o.size FROM objects o WHERE o.state='live'
		  AND NOT EXISTS (SELECT 1 FROM refs r WHERE r.urn=o.urn AND r.state='pinned')
		  AND (
		    NOT EXISTS (SELECT 1 FROM refs r WHERE r.urn=o.urn AND r.state <> 'unavailable')
		    OR NOT EXISTS (SELECT 1 FROM refs r WHERE r.urn=o.urn
		                   AND r.state <> 'unavailable' AND r.ephemeral = 0)
		  )`)
	if err != nil {
		return nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
	}
	var eligible []Collected
	for rows.Next() {
		var c Collected
		if rows.Scan(&c.URN, &c.Size) == nil {
			eligible = append(eligible, c)
		}
	}
	rows.Close()

	nowS := now.Format(time.RFC3339)
	var collected []Collected
	for _, c := range eligible {
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return collected, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
		// Invariant 2's atomic flip: available → expired-local with
		// the removal. (Other non-terminal states of ephemeral refs
		// — expected offers whose bytes we are discarding — expire
		// the same way; the tombstone record is the refs row.)
		if _, err := tx.ExecContext(ctx,
			`UPDATE refs SET state='unavailable', cause='expired-local', updated_at=?
			 WHERE urn=? AND state IN ('available','expected','offered')`, nowS, c.URN); err != nil {
			tx.Rollback()
			return collected, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM objects WHERE urn=? AND state='live'`, c.URN); err != nil {
			tx.Rollback()
			return collected, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
		if err := tx.Commit(); err != nil {
			return collected, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
		if bs != nil {
			os.Remove(bs.ObjectPath(c.URN))
		}
		collected = append(collected, c)
	}
	return collected, nil
}
