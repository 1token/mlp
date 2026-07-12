package sn

// §10.5 invariants (D-88) + the D-139 class, tested as behaviors:
// a pinned reference retains its object absolutely (the MUST); the
// ephemeral sweep collects an auto-granted object and flips its
// references to unavailable(expired-local) atomically; a
// standard-class available reference is untouched by the sweep;
// only-unavailable objects are immediately collectable regardless
// of class (invariant 3).

import (
	"context"
	"testing"
	"time"
)

func gcFixture(t *testing.T) (*SN, time.Time) {
	t.Helper()
	clock := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	s := newSN(t, "target.example", &clock)
	mustExec(t, s, `INSERT INTO stores (id, name) VALUES (1, 'default')`)
	mustExec(t, s, `INSERT INTO mailboxes (id, local_part, created) VALUES (1, 'novak', '2026-01-01T00:00:00Z')`)
	return s, clock
}

func seedObjectWithRef(t *testing.T, s *SN, urn, state string, ephemeral int) {
	t.Helper()
	mustExec(t, s, `INSERT OR IGNORE INTO objects (urn, size, state, store_id, created_at, verified_at)
		VALUES (?, 10, 'live', 1, '2026-07-12T11:00:00Z', '2026-07-12T11:00:00Z')`, urn)
	cause := any(nil)
	if state == "unavailable" {
		cause = "expired-remote"
	}
	mustExec(t, s, `INSERT INTO refs (mailbox_id, urn, medialet_ca, direction, state, cause, size, type, available_until, ephemeral, updated_at)
		VALUES (1, ?, ?, 'in', ?, ?, 10, 'text/plain', '2026-12-01T00:00:00Z', ?, '2026-07-12T11:00:00Z')`,
		urn, "ca-"+urn+"-"+state, state, cause, ephemeral)
}

func objectState(t *testing.T, s *SN, urn string) string {
	t.Helper()
	var st string
	s.DB.QueryRow(`SELECT COALESCE((SELECT state FROM objects WHERE urn=?), 'gone')`, urn).Scan(&st)
	return st
}

func TestGCInvariants(t *testing.T) {
	s, now := gcFixture(t)

	pinnedURN := "urn:mlet:bpinned0000000000000000000000000000000000000000000000000"
	ephemeralURN := "urn:mlet:bephemeral00000000000000000000000000000000000000000000000"
	standardURN := "urn:mlet:bstandard000000000000000000000000000000000000000000000000"
	deadURN := "urn:mlet:bdead000000000000000000000000000000000000000000000000000"

	// (1) The MUST: a pinned ephemeral reference retains absolutely.
	seedObjectWithRef(t, s, pinnedURN, "pinned", 1)
	// (2) GC-first: ephemeral + available, unpinned → collected.
	seedObjectWithRef(t, s, ephemeralURN, "available", 1)
	// (3) Standard class untouched by the sweep.
	seedObjectWithRef(t, s, standardURN, "available", 0)
	// (4) Invariant 3: only-unavailable refs → collectable, any class.
	seedObjectWithRef(t, s, deadURN, "unavailable", 0)

	collected, prob := s.CollectGarbage(context.Background(), nil, now)
	if prob != nil {
		t.Fatal(prob)
	}
	got := map[string]bool{}
	for _, c := range collected {
		got[c.URN] = true
	}
	if got[pinnedURN] || objectState(t, s, pinnedURN) != "live" {
		t.Fatal("a pinned reference MUST retain its object (§10.5 invariant 1)")
	}
	if got[standardURN] || objectState(t, s, standardURN) != "live" {
		t.Fatal("the ephemeral sweep must not touch standard-class references (D-139 scope)")
	}
	if !got[ephemeralURN] || objectState(t, s, ephemeralURN) != "gone" {
		t.Fatal("an unpinned ephemeral object is GC-first (D-139)")
	}
	if !got[deadURN] || objectState(t, s, deadURN) != "gone" {
		t.Fatal("an only-unavailable object is immediately collectable (§10.5 invariant 3)")
	}

	// The atomic flip (invariant 2): the collected ephemeral's
	// reference is now the §10.4 tombstone with expired-local, and
	// the tombstone minimum record survives on the row.
	var state, cause, typ string
	var size int64
	if err := s.DB.QueryRow(
		`SELECT state, cause, size, type FROM refs WHERE urn=?`, ephemeralURN).
		Scan(&state, &cause, &size, &typ); err != nil {
		t.Fatal(err)
	}
	if state != "unavailable" || cause != "expired-local" || size != 10 || typ != "text/plain" {
		t.Fatalf("the atomic flip + tombstone record (§10.4/§10.5): %s %s %d %s", state, cause, size, typ)
	}

	// Idempotence: a second pass collects nothing.
	again, prob := s.CollectGarbage(context.Background(), nil, now)
	if prob != nil || len(again) != 0 {
		t.Fatalf("the sweep must be idempotent: %v %v", again, prob)
	}
}
