package clientapi

// S4.11 server-side acceptance: the render form is derived at ingest
// and served in thread payloads with the D-132 snippet in rollups
// (the D-223 deferral resolved); the D-21 classifier demotes on
// derived text; the media lifecycle walks §10.3 end to end —
// offered → expected (accept) → available (verified upload via the
// OnVerified seam) → pinned → available → unavailable(deleted) with
// the object served then gone; the raw Signed Medialet endpoint
// answers recipients only; junk release records allow and block
// records block (D-165).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRenderFormAndSnippet(t *testing.T) {
	clock := time.Date(2026, 7, 4, 10, 0, 6, 0, time.UTC)
	s := directDeliveryAPI(t, &clock)

	var renderForm, derivedText string
	var degraded int
	if err := s.DB.QueryRow(
		`SELECT COALESCE(render_form,''), COALESCE(derived_text,''), render_degraded FROM medialets`).
		Scan(&renderForm, &derivedText, &degraded); err != nil {
		t.Fatal(err)
	}
	if renderForm == "" || derivedText == "" || degraded != 0 {
		t.Fatalf("ingest must derive: rf=%q dt=%q deg=%d", renderForm, derivedText, degraded)
	}
	var rollupJSON string
	s.DB.QueryRow(`SELECT rollup_json FROM threads`).Scan(&rollupJSON)
	var rollup map[string]any
	json.Unmarshal([]byte(rollupJSON), &rollup)
	snippet, _ := rollup["snippet"].(string)
	if snippet == "" || !strings.Contains(derivedText, strings.TrimSuffix(strings.Split(snippet, " ")[0], "…")) {
		t.Fatalf("rollup snippet missing or foreign: %q vs %q", snippet, derivedText)
	}

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	do := login(t, ts, "novak@target.example")
	resp := do(http.MethodGet, "/api/v1/threads/1", "", nil)
	var thread struct {
		Messages []struct {
			Body map[string]any `json:"body"`
		} `json:"messages"`
	}
	json.NewDecoder(resp.Body).Decode(&thread)
	resp.Body.Close()
	if got, _ := thread.Messages[0].Body["content"].(string); got != renderForm {
		t.Fatalf("thread payload must serve the render form:\n got %q\nwant %q", got, renderForm)
	}
}

func TestClassifierDemotesOnDerivedText(t *testing.T) {
	clock := time.Date(2026, 7, 4, 10, 0, 6, 0, time.UTC)
	tv1 := loadJSON(t, tv001Path)
	s := newAPI(t, "target.example", "novak", seedT3, &clock,
		map[string]json.RawMessage{"origin.example": tv1["domain_document"]})
	s.SN.Classifier = func(derived string) bool {
		return strings.Contains(derived, "sample") // fires on TV-001's body
	}
	if _, prob := s.SN.ProcessDispatch(context.Background(), tv1["signed_envelope"]); prob != nil {
		t.Fatalf("ingest: %v", prob)
	}
	var junk int
	if err := s.DB.QueryRow(`SELECT junk FROM threads`).Scan(&junk); err != nil || junk != 1 {
		t.Fatalf("classifier hit must quarantine (D-21): junk=%d err=%v", junk, err)
	}
}

func TestMediaLifecycle(t *testing.T) {
	clock := time.Date(2026, 7, 4, 10, 0, 6, 0, time.UTC)
	s := directDeliveryAPI(t, &clock)
	clock = time.Date(2026, 7, 4, 12, 30, 0, 0, time.UTC)
	s.PostVerdict = func(ctx context.Context, origin string, doc []byte) error { return nil }
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	do := login(t, ts, "novak@target.example")

	// offered → expected via accept.
	resp := do(http.MethodPost, "/api/v1/o/"+mediaURN+"/accept", "{}", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("accept: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// expected → available: the object arrives through the local
	// door (simulating the origin's push landing) — OnVerified flips.
	object := "MLP test vector 001: media object A\n"
	resp = do(http.MethodPost, "/api/v1/uploads",
		fmt.Sprintf(`{"urn":%q,"size":36}`, mediaURN), nil)
	var lane struct {
		Upload string `json:"upload"`
	}
	json.NewDecoder(resp.Body).Decode(&lane)
	resp.Body.Close()
	resp = do(http.MethodPatch, lane.Upload, object, map[string]string{"Upload-Offset": "0"})
	if resp.Header.Get("MLP-Object-State") != "verified" {
		t.Fatalf("upload: %d %+v", resp.StatusCode, resp.Header)
	}
	resp.Body.Close()
	var state string
	s.DB.QueryRow(`SELECT state FROM refs WHERE urn=? AND mailbox_id=1`, mediaURN).Scan(&state)
	if state != "available" {
		t.Fatalf("verification must flip expected→available (§10.3): %q", state)
	}

	// The library card.
	resp = do(http.MethodGet, "/api/v1/media", "", nil)
	var lib struct {
		Media []struct {
			URN    string         `json:"urn"`
			Held   bool           `json:"held"`
			Pinned bool           `json:"pinned"`
			States map[string]int `json:"states"`
		} `json:"media"`
	}
	json.NewDecoder(resp.Body).Decode(&lib)
	resp.Body.Close()
	if len(lib.Media) != 1 || !lib.Media[0].Held || lib.Media[0].Pinned ||
		lib.Media[0].States["available"] != 1 {
		t.Fatalf("library: %+v", lib.Media)
	}

	// available → pinned → available.
	resp = do(http.MethodPost, "/api/v1/o/"+mediaURN+"/pin", "{}", nil)
	if resp.StatusCode != 204 {
		t.Fatalf("pin: %d", resp.StatusCode)
	}
	resp.Body.Close()
	resp = do(http.MethodPost, "/api/v1/o/"+mediaURN+"/pin", "{}", nil)
	if resp.StatusCode != 409 {
		t.Fatalf("double pin must be invalid-transition: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Serving: bytes + the hardening headers.
	resp = do(http.MethodGet, "/api/v1/o/"+mediaURN, "", nil)
	body := readAll(t, resp)
	if resp.StatusCode != 200 || body != object ||
		resp.Header.Get("Content-Security-Policy") != "sandbox" ||
		resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("object serving: %d %q %+v", resp.StatusCode, body, resp.Header)
	}

	// The raw Signed Medialet, recipients only.
	var ca string
	s.DB.QueryRow(`SELECT medialet_ca FROM messages WHERE mailbox_id=1`).Scan(&ca)
	resp = do(http.MethodGet, "/api/v1/m/"+ca, "", nil)
	raw := readAll(t, resp)
	var storedRaw []byte
	s.DB.QueryRow(`SELECT raw FROM medialets WHERE content_address=?`, ca).Scan(&storedRaw)
	if resp.StatusCode != 200 || raw != string(storedRaw) {
		t.Fatalf("raw medialet: %d", resp.StatusCode)
	}

	// The owner's delete (D-88): pinned goes unavailable(deleted),
	// bytes leave, serving 404s.
	resp = do(http.MethodDelete, "/api/v1/o/"+mediaURN, "", nil)
	if resp.StatusCode != 204 {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	resp.Body.Close()
	var cause string
	s.DB.QueryRow(`SELECT state, COALESCE(cause,'') FROM refs WHERE urn=? AND mailbox_id=1`, mediaURN).Scan(&state, &cause)
	if state != "unavailable" || cause != "deleted" {
		t.Fatalf("delete transition: %q %q", state, cause)
	}
	resp = do(http.MethodGet, "/api/v1/o/"+mediaURN, "", nil)
	if resp.StatusCode != 404 {
		t.Fatalf("deleted object must 404: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func readAll(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			return b.String()
		}
	}
}

func TestJunkReleaseAndBlock(t *testing.T) {
	clock := time.Date(2026, 7, 4, 10, 0, 6, 0, time.UTC)
	tv1 := loadJSON(t, tv001Path)
	s := newAPI(t, "target.example", "novak", seedT3, &clock,
		map[string]json.RawMessage{"origin.example": tv1["domain_document"]})
	s.SN.Classifier = func(string) bool { return true } // everything quarantines
	if _, prob := s.SN.ProcessDispatch(context.Background(), tv1["signed_envelope"]); prob != nil {
		t.Fatalf("ingest: %v", prob)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	do := login(t, ts, "novak@target.example")

	// Release: junk=0, sender allowed (D-165's strongest signal).
	resp := do(http.MethodPost, "/api/v1/threads/1/release", "{}", nil)
	if resp.StatusCode != 204 {
		t.Fatalf("release: %d", resp.StatusCode)
	}
	resp.Body.Close()
	var junk int
	var override string
	s.DB.QueryRow(`SELECT junk FROM threads WHERE id=1`).Scan(&junk)
	s.DB.QueryRow(`SELECT tier_override FROM correspondents WHERE addr='petra@origin.example'`).Scan(&override)
	if junk != 0 || override != "allow" {
		t.Fatalf("release: junk=%d override=%q", junk, override)
	}
	// The allow override now outranks the classifier: a fresh
	// dispatch from the same author lands in the inbox.
	reply := signEnvelope(t, "e-second-1", "m-second-1", "", "2026-07-04T11:00:00Z")
	clock = time.Date(2026, 7, 4, 11, 0, 1, 0, time.UTC)
	if _, prob := s.SN.ProcessDispatch(context.Background(), reply); prob != nil {
		t.Fatalf("second ingest: %v", prob)
	}
	var junkThreads int
	s.DB.QueryRow(`SELECT COUNT(*) FROM threads WHERE junk=1`).Scan(&junkThreads)
	if junkThreads != 0 {
		t.Fatalf("allowed sender must land in the inbox: %d junk threads", junkThreads)
	}

	// Block: the correspondents ledger flips; the thread is done.
	resp = do(http.MethodPost, "/api/v1/threads/1/block", "{}", nil)
	if resp.StatusCode != 204 {
		t.Fatalf("block: %d", resp.StatusCode)
	}
	resp.Body.Close()
	s.DB.QueryRow(`SELECT tier_override FROM correspondents WHERE addr='petra@origin.example'`).Scan(&override)
	var done int
	s.DB.QueryRow(`SELECT done FROM threads WHERE id=1`).Scan(&done)
	if override != "block" || done != 1 {
		t.Fatalf("block: override=%q done=%d", override, done)
	}
	// Correspondents listing reflects it.
	resp = do(http.MethodGet, "/api/v1/correspondents", "", nil)
	var list struct {
		Correspondents []map[string]any `json:"correspondents"`
	}
	json.NewDecoder(resp.Body).Decode(&list)
	resp.Body.Close()
	if len(list.Correspondents) != 1 || list.Correspondents[0]["tier_override"] != "block" {
		t.Fatalf("correspondents: %+v", list.Correspondents)
	}
}
