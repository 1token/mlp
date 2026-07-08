package store

// S4.2 acceptance: the migration applies cleanly, the D-87 reference
// state machine is enforced by the refs_transitions trigger (legal
// walk passes; each forbidden transition aborts with
// 'invalid-transition'), replay-dedup uniqueness holds (D-20), and
// consumed reservations are terminal (D-18).

import (
	"database/sql"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func openTest(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open("sqlite3", "file:"+t.TempDir()+"/mlp.db?_fk=1")
	if err != nil {
		t.Fatalf("open+migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	mustExec(t, db, `INSERT INTO stores(id,name) VALUES (1,'default')`)
	mustExec(t, db, `INSERT INTO mailboxes(id,local_part,created) VALUES (1,'novak','2026-07-04T10:00:00Z')`)
	return db
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func setState(db *sql.DB, id int, state string, cause any) error {
	_, err := db.Exec(`UPDATE refs SET state=?, cause=?, updated_at='2026-07-04T12:00:00Z' WHERE id=?`, state, cause, id)
	return err
}

func insertRef(t *testing.T, db *sql.DB, id int, state string) {
	t.Helper()
	mustExec(t, db, `INSERT INTO refs(id,mailbox_id,urn,medialet_ca,direction,state,name,size,type,available_until,updated_at)
		VALUES (?,1,'urn:mlet:bdyqx'||?, 'urn:mlet:bdyqca','in',?,'f',36,'text/plain','2026-07-11T10:00:00Z','2026-07-04T10:00:00Z')`,
		id, id, state)
}

func TestReferenceStateMachine(t *testing.T) {
	db := openTest(t)

	// Legal walk (D-87): offered→expected→available→pinned→available→unavailable(deleted)
	insertRef(t, db, 1, "offered")
	for _, step := range []struct {
		state string
		cause any
	}{
		{"expected", nil}, {"available", nil}, {"pinned", nil},
		{"available", nil}, {"unavailable", "deleted"},
	} {
		if err := setState(db, 1, step.state, step.cause); err != nil {
			t.Fatalf("legal transition to %s(%v): %v", step.state, step.cause, err)
		}
	}

	// Forbidden transitions, each must abort:
	forbidden := []struct {
		from, to string
		cause    any
	}{
		{"unavailable", "available", nil},          // terminal (D-87)
		{"pinned", "unavailable", "expired-local"}, // pin blocks GC (D-88)
		{"offered", "available", nil},              // strict two-step
		{"offered", "pinned", nil},
		{"available", "offered", nil},
		{"available", "unavailable", "declined"}, // wrong cause for this edge
	}
	for i, f := range forbidden {
		id := 10 + i
		if f.from == "unavailable" {
			// state='unavailable' requires a cause (CHECK), so this
			// starting point is inserted directly, not via insertRef.
			mustExec(t, db, `INSERT INTO refs(id,mailbox_id,urn,medialet_ca,direction,state,cause,name,size,type,available_until,updated_at)
				VALUES (?,1,'urn:mlet:bdyqt'||?, 'urn:mlet:bdyqca','in','unavailable','deleted','f',36,'text/plain','2026-07-11T10:00:00Z','x')`, id, id)
		} else {
			insertRef(t, db, id, f.from)
		}
		err := setState(db, id, f.to, f.cause)
		if err == nil || !strings.Contains(err.Error(), "invalid-transition") {
			t.Fatalf("forbidden %s→%s(%v) was not aborted: %v", f.from, f.to, f.cause, err)
		}
	}
}

func TestReplayUniqueAndReservationTerminal(t *testing.T) {
	db := openTest(t)
	mustExec(t, db, `INSERT INTO medialets(content_address,author,medialet_id,created,raw)
		VALUES ('urn:mlet:bdyqca','petra@origin.example','m1','2026-07-04T10:00:00Z',x'00')`)
	ins := `INSERT INTO envelopes_in(origin,envelope_id,medialet_ca,received_at,
		author_sig_result,author_sig_kid,author_verified_at,hop_sig_result,hop_sig_kid,hop_verified_at)
		VALUES ('origin.example','e1','urn:mlet:bdyqca','2026-07-04T10:00:06Z','ok','k','t','ok','k','t')`
	mustExec(t, db, ins)
	if _, err := db.Exec(ins); err == nil {
		t.Fatal("duplicate (origin, envelope_id) accepted — replay dedup broken (D-20)")
	}

	mustExec(t, db, `INSERT INTO reservations_in(token_hash,urn,max_size,pusher_domain,expires,state,store_id,created)
		VALUES ('h1','urn:mlet:bdyqca',36,'origin.example','2026-07-07T12:30:00Z','pending',1,'t')`)
	mustExec(t, db, `UPDATE reservations_in SET state='consumed' WHERE token_hash='h1'`)
	if _, err := db.Exec(`UPDATE reservations_in SET state='pending' WHERE token_hash='h1'`); err == nil ||
		!strings.Contains(err.Error(), "reservation-invalid") {
		t.Fatalf("consumed reservation was revivable (D-18): %v", err)
	}
}
