package clientapi

// S4.9 server-side acceptance: TV-001 ingest materializes the
// mailbox view (thread with rollup, unread message, offered refs);
// replies join their parent's thread (D-110) while unknown parents
// root their own; quarantined senders land in the junk view, not the
// inbox; the triage trio mints working undo tokens (D-129) that
// expire; re-delivered Medialets dedup to one message instance;
// accept is refused for a mailbox that does not hold the delivery
// and flips the accepted ref offered→expected (§10.3).

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"medialet.org/mlp/core"
)

const seedOriginSN = "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb"
const seedAuthor = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"

func TestMaterializationOnIngest(t *testing.T) {
	clock := time.Date(2026, 7, 4, 10, 0, 6, 0, time.UTC)
	s := directDeliveryAPI(t, &clock)

	var threadID int64
	var junk int
	var rollupJSON string
	if err := s.DB.QueryRow(
		`SELECT id, junk, rollup_json FROM threads WHERE mailbox_id=1`).Scan(&threadID, &junk, &rollupJSON); err != nil {
		t.Fatalf("thread: %v", err)
	}
	var rollup map[string]any
	json.Unmarshal([]byte(rollupJSON), &rollup)
	if junk != 0 || rollup["subject"] != "TV-001 sample delivery" || rollup["last_author"] != "petra@origin.example" {
		t.Fatalf("rollup: junk=%d %v", junk, rollup)
	}
	var read int
	var deliveredTo string
	if err := s.DB.QueryRow(
		`SELECT read, delivered_to FROM messages WHERE mailbox_id=1 AND thread_id=?`, threadID).
		Scan(&read, &deliveredTo); err != nil || read != 0 || deliveredTo != "novak@target.example" {
		t.Fatalf("message: %v read=%d to=%q", err, read, deliveredTo)
	}
	var state string
	if err := s.DB.QueryRow(
		`SELECT state FROM refs WHERE mailbox_id=1 AND urn=?`, mediaURN).Scan(&state); err != nil || state != "offered" {
		t.Fatalf("ref: %v %q", err, state)
	}
}

// signEnvelope crafts a fresh valid dispatch from origin.example with
// the TV-001 keys (mirrors the sn-package helper).
func signEnvelope(t *testing.T, envelopeID, medialetID, inReplyTo, created string) []byte {
	t.Helper()
	aSeed, _ := hex.DecodeString(seedAuthor)
	sSeed, _ := hex.DecodeString(seedOriginSN)
	aPriv, sPriv := ed25519.NewKeyFromSeed(aSeed), ed25519.NewKeyFromSeed(sSeed)
	medialet := map[string]any{
		"mlp": "0.1", "id": medialetID, "author": "petra@origin.example",
		"subject": "re: TV-001", "created": created,
		"body": map[string]any{"profile": "mlp-html/1", "content": "<p>reply</p>"},
	}
	if inReplyTo != "" {
		medialet["in_reply_to"] = inReplyTo
	}
	aSig, _, err := core.SignDoc(aPriv, "author/1", core.KID(aPriv.Public().(ed25519.PublicKey)), created, medialet)
	if err != nil {
		t.Fatal(err)
	}
	envelope := map[string]any{
		"mlp": "0.1", "envelope_id": envelopeID, "created": created,
		"origin": "origin.example", "envelope_to": []any{"novak@target.example"},
		"medialet": map[string]any{"medialet": medialet, "signature": aSig},
	}
	hSig, _, err := core.SignDoc(sPriv, "hop/1", core.KID(sPriv.Public().(ed25519.PublicKey)), created, envelope)
	if err != nil {
		t.Fatal(err)
	}
	canon, err := core.CanonicalizeValue(map[string]any{"envelope": envelope, "signature": hSig})
	if err != nil {
		t.Fatal(err)
	}
	return canon
}

func TestReplyThreadingAndDedup(t *testing.T) {
	clock := time.Date(2026, 7, 4, 10, 0, 6, 0, time.UTC)
	s := directDeliveryAPI(t, &clock)
	tv1 := loadJSON(t, tv001Path)
	tv1CA := core.URNMlet(canonical(t, extractRaw(t, tv1, "signed_envelope", "envelope", "medialet")))

	// A reply to the TV-001 Medialet joins its thread (D-110).
	reply := signEnvelope(t, "e-reply-1", "m-reply-1", tv1CA, "2026-07-04T11:00:00Z")
	clock = time.Date(2026, 7, 4, 11, 0, 1, 0, time.UTC)
	if _, prob := s.SN.ProcessDispatch(context.Background(), reply); prob != nil {
		t.Fatalf("reply ingest: %v", prob)
	}
	var threads, msgs int
	s.DB.QueryRow(`SELECT COUNT(*) FROM threads WHERE mailbox_id=1`).Scan(&threads)
	s.DB.QueryRow(`SELECT COUNT(DISTINCT thread_id) FROM messages WHERE mailbox_id=1`).Scan(&msgs)
	if threads != 1 || msgs != 1 {
		t.Fatalf("reply must join the parent thread: threads=%d distinct=%d", threads, msgs)
	}

	// A reply to an unseen parent roots its own thread.
	orphan := signEnvelope(t, "e-orphan-1", "m-orphan-1",
		"urn:mlet:bdyqhpdu37yof4e5ka5bbamzdh7rl2w3wyocv6hfza67mwkvw6ge7j5y", "2026-07-04T11:05:00Z")
	if _, prob := s.SN.ProcessDispatch(context.Background(), orphan); prob != nil {
		t.Fatalf("orphan ingest: %v", prob)
	}
	s.DB.QueryRow(`SELECT COUNT(*) FROM threads WHERE mailbox_id=1`).Scan(&threads)
	if threads != 2 {
		t.Fatalf("orphan reply must root its own thread: %d", threads)
	}

	// Re-delivery of the same Medialet in a fresh envelope: one
	// message instance (UNIQUE mailbox+medialet).
	redeliver := signEnvelope(t, "e-again-1", "m-reply-1", tv1CA, "2026-07-04T11:00:00Z")
	if _, prob := s.SN.ProcessDispatch(context.Background(), redeliver); prob != nil {
		t.Fatalf("re-delivery ingest: %v", prob)
	}
	var count int
	s.DB.QueryRow(`SELECT COUNT(*) FROM messages WHERE mailbox_id=1`).Scan(&count)
	if count != 3 {
		t.Fatalf("re-delivered medialet must not duplicate: %d messages", count)
	}
}

func TestQuarantinedLandsInJunkView(t *testing.T) {
	clock := time.Date(2026, 7, 4, 10, 0, 6, 0, time.UTC)
	tv1 := loadJSON(t, tv001Path)
	s := newAPI(t, "target.example", "novak", seedT3, &clock,
		map[string]json.RawMessage{"origin.example": tv1["domain_document"]})
	mustExec(t, s.DB, `INSERT INTO correspondents (mailbox_id, addr, tier_override) VALUES (1, 'petra@origin.example', 'block')`)
	if _, prob := s.SN.ProcessDispatch(context.Background(), tv1["signed_envelope"]); prob != nil {
		t.Fatalf("ingest: %v", prob)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	do := login(t, ts, "novak@target.example")

	resp := do(http.MethodGet, "/api/v1/threads?view=inbox", "", nil)
	var inbox struct {
		Threads []any `json:"threads"`
	}
	json.NewDecoder(resp.Body).Decode(&inbox)
	resp.Body.Close()
	resp = do(http.MethodGet, "/api/v1/threads?view=junk", "", nil)
	var junk struct {
		Threads []any `json:"threads"`
	}
	json.NewDecoder(resp.Body).Decode(&junk)
	resp.Body.Close()
	if len(inbox.Threads) != 0 || len(junk.Threads) != 1 {
		t.Fatalf("quarantined thread placement: inbox=%d junk=%d", len(inbox.Threads), len(junk.Threads))
	}
}

func TestThreadsEndpointsAndTriageUndo(t *testing.T) {
	clock := time.Date(2026, 7, 4, 10, 0, 6, 0, time.UTC)
	s := directDeliveryAPI(t, &clock)
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	do := login(t, ts, "novak@target.example")

	// The rolled-up list: subject, unread, offered chip.
	resp := do(http.MethodGet, "/api/v1/threads?view=inbox", "", nil)
	var list struct {
		Threads []struct {
			ID     int64            `json:"id"`
			Unread int              `json:"unread"`
			Rollup map[string]any   `json:"rollup"`
			Media  map[string]int64 `json:"media"`
		} `json:"threads"`
	}
	json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list.Threads) != 1 || list.Threads[0].Unread != 1 ||
		list.Threads[0].Rollup["subject"] != "TV-001 sample delivery" ||
		list.Threads[0].Media["offered"] != 1 {
		t.Fatalf("thread list: %+v", list)
	}
	threadPath := "/api/v1/threads/1"

	// The full thread: verbatim body + refs states per urn.
	resp = do(http.MethodGet, threadPath, "", nil)
	var thread struct {
		Messages []struct {
			Author string                    `json:"author"`
			Body   map[string]string         `json:"body"`
			Refs   map[string]map[string]any `json:"refs"`
		} `json:"messages"`
	}
	json.NewDecoder(resp.Body).Decode(&thread)
	resp.Body.Close()
	if len(thread.Messages) != 1 || thread.Messages[0].Author != "petra@origin.example" ||
		thread.Messages[0].Body["profile"] != "mlp-html/1" ||
		thread.Messages[0].Refs[mediaURN]["state"] != "offered" {
		t.Fatalf("thread payload: %+v", thread.Messages)
	}

	// read → unread 0; undo → unread 1 again; token single-use.
	resp = do(http.MethodPost, threadPath+"/read", "{}", nil)
	var triage struct {
		UndoToken string `json:"undo_token"`
	}
	json.NewDecoder(resp.Body).Decode(&triage)
	resp.Body.Close()
	if triage.UndoToken == "" {
		t.Fatal("read must mint an undo token (D-129)")
	}
	var unread int
	s.DB.QueryRow(`SELECT COUNT(*) FROM messages WHERE thread_id=1 AND read=0`).Scan(&unread)
	if unread != 0 {
		t.Fatalf("read: %d unread", unread)
	}
	resp = do(http.MethodPost, "/api/v1/undo", `{"token":"`+triage.UndoToken+`"}`, nil)
	if resp.StatusCode != 204 {
		t.Fatalf("undo: %d", resp.StatusCode)
	}
	resp.Body.Close()
	s.DB.QueryRow(`SELECT COUNT(*) FROM messages WHERE thread_id=1 AND read=0`).Scan(&unread)
	if unread != 1 {
		t.Fatalf("undo must restore unread: %d", unread)
	}
	resp = do(http.MethodPost, "/api/v1/undo", `{"token":"`+triage.UndoToken+`"}`, nil)
	if resp.StatusCode != 404 {
		t.Fatalf("undo tokens are single-use: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// done, then an expired undo window (30 s TTL, D-129).
	resp = do(http.MethodPost, threadPath+"/done", "{}", nil)
	json.NewDecoder(resp.Body).Decode(&triage)
	resp.Body.Close()
	clock = clock.Add(time.Minute)
	resp = do(http.MethodPost, "/api/v1/undo", `{"token":"`+triage.UndoToken+`"}`, nil)
	if resp.StatusCode != 410 {
		t.Fatalf("elapsed undo window must be 410: %d", resp.StatusCode)
	}
	resp.Body.Close()
	// Done threads leave the inbox view (triage, never retention:
	// the row survives; new activity would resurface it).
	resp = do(http.MethodGet, "/api/v1/threads?view=inbox", "", nil)
	list.Threads = nil
	json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list.Threads) != 0 {
		t.Fatalf("done thread must leave the inbox: %+v", list.Threads)
	}
	var alive int
	s.DB.QueryRow(`SELECT COUNT(*) FROM threads WHERE id=1`).Scan(&alive)
	if alive != 1 {
		t.Fatalf("done is triage, not deletion (D-120)")
	}
}

func TestAcceptAuthorizationAndRefTransition(t *testing.T) {
	clock := time.Date(2026, 7, 4, 10, 0, 6, 0, time.UTC)
	s := directDeliveryAPI(t, &clock)
	// A second mailbox that was never a recipient.
	mustExec(t, s.DB, `INSERT INTO mailboxes (id, local_part, created) VALUES (2, 'intruder', '2026-01-01T00:00:00Z')`)
	h, _ := HashPassword("correct horse", 1000)
	mustExec(t, s.DB, `INSERT INTO password_fallback (mailbox_id, hash) VALUES (2, ?)`, h)

	clock = time.Date(2026, 7, 4, 12, 30, 0, 0, time.UTC)
	s.PostVerdict = func(ctx context.Context, origin string, doc []byte) error { return nil }
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	intruder := login(t, ts, "intruder@target.example")
	resp := intruder(http.MethodPost, "/api/v1/o/"+mediaURN+"/accept", "{}", nil)
	if resp.StatusCode != 404 {
		t.Fatalf("non-recipient accept must be 404: %d", resp.StatusCode)
	}
	resp.Body.Close()

	novak := login(t, ts, "novak@target.example")
	resp = novak(http.MethodPost, "/api/v1/o/"+mediaURN+"/accept", "{}", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("recipient accept: %d", resp.StatusCode)
	}
	resp.Body.Close()
	var state string
	s.DB.QueryRow(`SELECT state FROM refs WHERE mailbox_id=1 AND urn=?`, mediaURN).Scan(&state)
	if state != "expected" {
		t.Fatalf("accept must flip offered→expected (§10.3): %q", state)
	}
}
