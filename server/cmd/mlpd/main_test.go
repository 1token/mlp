package main

// The minimum credible demo (Stage 3 Closing §5, D-41 extended), as
// a deterministic test over two REAL domains on real TCP sockets:
//
//   1. two domains, discovery through the demo fetcher;
//   2. a delivery composed with a job tag;
//   3. Tier-2 deferral with the small preview auto-granted (D-139)
//      and paired to its master by preview_of (MEP-002);
//   4. a large object accepted, the transfer KILLED mid-flight and
//      resumed to completion with zero redundant bytes (§8);
//   5. `have` answering a resend (the D-241 instant short-circuit);
//   6. a reply threading back at the sender, the thread swept done;
//   7. the guest → claim → instant-have funnel end to end.
//
// demo/walk.sh performs the same journey against live mlpd processes
// for the on-camera run; this test is the record CI keeps.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"medialet.org/mlp/core"
)

type demoNode struct {
	*node
	base string
	ts   *httptest.Server
}

// reserveListener grabs a socket first so both domains' base URLs
// exist before either node builds — the demo fetcher needs the full
// peer map at composition time.
func reserveListener(t *testing.T) (net.Listener, string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return listener, "http://" + listener.Addr().String()
}

func startNode(t *testing.T, listener net.Listener, base, domain, user string, peers map[string]string) *demoNode {
	t.Helper()
	n, err := buildNode(config{
		Domain: domain, SelfBase: base, DataDir: t.TempDir(),
		InitUser: user, Password: "correct horse", Origin: base, Peers: peers,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	ts := &httptest.Server{Listener: listener, Config: &http.Server{Handler: n.mux}}
	ts.Start()
	t.Cleanup(ts.Close)
	return &demoNode{node: n, base: base, ts: ts}
}

type client struct {
	t      *testing.T
	base   string
	cookie *http.Cookie
}

func (c *client) do(method, path, body string, hdr map[string]string) *http.Response {
	c.t.Helper()
	req, _ := http.NewRequest(method, c.base+path, strings.NewReader(body))
	req.Header.Set("X-MLP-Client", "demo")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	return resp
}

func (c *client) json(method, path, body string, out any) int {
	c.t.Helper()
	resp := c.do(method, path, body, nil)
	defer resp.Body.Close()
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func loginDemo(t *testing.T, base, address string) *client {
	t.Helper()
	c := &client{t: t, base: base}
	resp := c.do(http.MethodPost, "/api/v1/auth/password",
		`{"address":"`+address+`","password":"correct horse"}`, nil)
	if resp.StatusCode != 200 {
		t.Fatalf("login %s: %d", address, resp.StatusCode)
	}
	for _, ck := range resp.Cookies() {
		if ck.Name == "mlp_session" {
			c.cookie = ck
		}
	}
	resp.Body.Close()
	if c.cookie == nil {
		t.Fatal("no session cookie")
	}
	return c
}

// upload drives the D-135 door, optionally "dying" after the first
// chunk and resuming from the receiver's confirmed offset.
func upload(t *testing.T, c *client, data []byte, interrupt bool) string {
	t.Helper()
	urn := core.URNMlet(data)
	var created struct {
		Have   bool   `json:"have"`
		Upload string `json:"upload"`
	}
	code := c.json(http.MethodPost, "/api/v1/uploads",
		fmt.Sprintf(`{"urn":%q,"size":%d}`, urn, len(data)), &created)
	if code == 200 && created.Have {
		return urn // the have short-circuit: nothing to upload
	}
	if code != 201 {
		t.Fatalf("upload create: %d", code)
	}
	send := func(offset int64, chunk []byte) {
		resp := c.do(http.MethodPatch, created.Upload, string(chunk), map[string]string{
			"Upload-Offset": fmt.Sprint(offset),
			"Content-Type":  "application/offset+octet-stream",
		})
		if resp.StatusCode != 204 {
			t.Fatalf("upload patch @%d: %d", offset, resp.StatusCode)
		}
		resp.Body.Close()
	}
	if interrupt {
		half := len(data) / 2
		send(0, data[:half])
		// The "crash": the client vanishes. The resume starts with
		// HEAD — the receiver's confirmed offset is the truth.
		resp := c.do(http.MethodHead, created.Upload, "", nil)
		offset := resp.Header.Get("Upload-Offset")
		resp.Body.Close()
		if offset != fmt.Sprint(half) {
			t.Fatalf("resume offset: %q want %d", offset, half)
		}
		send(int64(half), data[half:]) // zero redundant bytes
	} else {
		send(0, data)
	}
	return urn
}

func TestTwoDomainDemo(t *testing.T) {
	// ---- 1. Two real domains -----------------------------------------
	originL, originBase := reserveListener(t)
	targetL, targetBase := reserveListener(t)
	peers := map[string]string{"origin.demo": originBase, "target.demo": targetBase}
	origin := startNode(t, originL, originBase, "origin.demo", "petra", peers)
	target := startNode(t, targetL, targetBase, "target.demo", "novak", peers)

	petra := loginDemo(t, origin.base, "petra@origin.demo")
	novak := loginDemo(t, target.base, "novak@target.demo")

	// ---- 4a. The large object arrives at origin over the resumable
	// door: killed after half, resumed from the confirmed offset.
	master := make([]byte, 6<<20) // 6 MiB: above the D-139 auto-grant line
	rand.Read(master)
	masterURN := upload(t, petra, master, true)
	preview := []byte("tiny preview stand-in — well under the D-139 line")
	previewURN := upload(t, petra, preview, false)

	// ---- 2+3. Compose: job tag; the preview paired by preview_of. ----
	draft := map[string]any{
		"subject":      "Wedding shoot — full set",
		"body_content": fmt.Sprintf(`<p>Full set attached. Preview: <a href=%q>preview</a></p>`, previewURN),
		"recipients":   []string{"novak@target.demo"},
		"job_tag":      "novak wedding",
		"manifest": []map[string]any{
			{"urn": masterURN, "size": len(master), "type": "application/octet-stream",
				"name": "set.raw", "available_until": "2026-12-01T00:00:00Z"},
			{"urn": previewURN, "size": len(preview), "type": "text/plain",
				"name": "preview.txt", "available_until": "2026-12-01T00:00:00Z",
				"preview_of": masterURN},
		},
	}
	draftJSON, _ := json.Marshal(draft)
	var created struct {
		ID string `json:"id"`
	}
	if code := petra.json(http.MethodPost, "/api/v1/drafts", string(draftJSON), &created); code != 201 {
		t.Fatalf("draft: %d", code)
	}
	var sent struct {
		MedialetCA string `json:"medialet_ca"`
		Targets    []struct{ Message string }
	}
	if code := petra.json(http.MethodPost, "/api/v1/drafts/"+created.ID+"/send", "{}", &sent); code != 200 {
		t.Fatalf("send: %d", code)
	}
	t.Logf("send targets: %+v", sent.Targets)

	// Tier-2 verdict at the target: strangers defer the master; the
	// small preview auto-grants (D-139), pairing via preview_of.
	var masterState, previewState string
	target.DB.QueryRow(`SELECT state FROM refs WHERE urn=?`, masterURN).Scan(&masterState)
	target.DB.QueryRow(`SELECT state, COALESCE(preview_of,'') FROM refs WHERE urn=?`, previewURN).
		Scan(&previewState, new(string))
	if masterState != "offered" {
		t.Fatalf("Tier-2 master must sit offered (deferred): %q", masterState)
	}
	var pairedTo string
	target.DB.QueryRow(`SELECT COALESCE(preview_of,'') FROM refs WHERE urn=?`, previewURN).Scan(&pairedTo)
	if pairedTo != masterURN {
		t.Fatalf("preview_of must survive to the recipient's refs: %q", pairedTo)
	}

	// The auto-granted preview's bytes flow: drive the origin's push.
	origin.pushOnce(context.Background())
	var previewObjState string
	target.DB.QueryRow(`SELECT state FROM objects WHERE urn=?`, previewURN).Scan(&previewObjState)
	if previewObjState != "live" {
		t.Fatalf("the auto-granted preview must arrive: %q", previewObjState)
	}
	target.DB.QueryRow(`SELECT state FROM refs WHERE urn=?`, previewURN).Scan(&previewState)
	if previewState != "available" {
		t.Fatalf("preview ref after arrival: %q", previewState)
	}

	// ---- 4b. Novak accepts the master; the federation push is
	// KILLED mid-flight (the pusher's client dies), then resumed —
	// the receiver's confirmed offset guarantees zero redundant
	// bytes (§8).
	var accept struct {
		State  string `json:"state"`
		Detail string `json:"detail"`
	}
	if code := novak.json(http.MethodPost, "/api/v1/o/"+masterURN+"/accept", "{}", &accept); code != 200 {
		t.Fatalf("accept master: %d %+v", code, accept)
	}
	// The crash: one 2 MiB chunk lands and is checkpointed, then the
	// process "dies" — the source vanishes mid-read, Push returns
	// without any state transition, and the row sits at 'pushing'
	// with the receiver's confirmed offset. Exactly kill -9.
	//
	// S4.22: the crash push runs with capabilities dark (a truncated
	// SOURCE under mlp-bao dies at encode time, before any byte
	// moves — the encoded form's root node needs every group CV up
	// front, so the mid-read death only exists on the raw path).
	// The resume below runs with capabilities restored, which is
	// itself an assertion: the §8.9 adoption rule keeps a raw-bound
	// resource raw even when the pusher has since learned bao.
	savedCaps := origin.pusher.Caps
	origin.pusher.Caps = nil
	origin.pusher.Chunk = 2 << 20
	var resID int64
	origin.DB.QueryRow(`SELECT id FROM reservations_out WHERE urn=?`, masterURN).Scan(&resID)
	if err := origin.pusher.Push(context.Background(), resID,
		bytes.NewReader(master[:3<<20])); err == nil {
		t.Fatal("the truncated source must kill the push mid-flight")
	}
	var offsetAfterKill int64
	var stateAfterKill string
	origin.DB.QueryRow(`SELECT offset_confirmed, state FROM reservations_out WHERE id=?`, resID).
		Scan(&offsetAfterKill, &stateAfterKill)
	if offsetAfterKill != 2<<20 || stateAfterKill != "pushing" {
		t.Fatalf("the kill must land mid-flight: offset %d state %q", offsetAfterKill, stateAfterKill)
	}
	origin.pusher.Caps = savedCaps        // the "restart" learns bao again
	origin.pushOnce(context.Background()) // the restarted process resumes
	var masterObjState string
	target.DB.QueryRow(`SELECT state FROM objects WHERE urn=?`, masterURN).Scan(&masterObjState)
	if masterObjState != "live" {
		t.Fatalf("resumed transfer must complete: %q", masterObjState)
	}
	// Byte-integrity is absolute: the URN verified at completion.
	got, err := readObject(target, masterURN)
	if err != nil || !bytes.Equal(got, master) {
		t.Fatalf("master bytes: %v (equal=%v)", err, err == nil && bytes.Equal(got, master))
	}

	// ---- 5. The reply threads back; the thread sweeps done. And it
	// does protocol work beyond threading: novak's outbound send
	// makes petra a CORRESPONDENT (Tier 1), which is what §7.5
	// possession disclosure keys on.
	var novakThreads struct {
		Threads []struct {
			ID int64 `json:"id"`
		} `json:"threads"`
	}
	novak.json(http.MethodGet, "/api/v1/threads?view=inbox", "", &novakThreads)
	reply := map[string]any{
		"subject": "Re: Wedding shoot", "body_content": "<p>Gorgeous — thank you!</p>",
		"recipients": []string{"petra@origin.demo"}, "in_reply_to": sent.MedialetCA,
	}
	replyJSON, _ := json.Marshal(reply)
	novak.json(http.MethodPost, "/api/v1/drafts", string(replyJSON), &created)
	if code := novak.json(http.MethodPost, "/api/v1/drafts/"+created.ID+"/send", "{}", &sent); code != 200 {
		t.Fatalf("reply send: %d", code)
	}
	var petraThreads struct {
		Threads []struct {
			ID       int64 `json:"id"`
			Messages int   `json:"messages"`
		} `json:"threads"`
	}
	petra.json(http.MethodGet, "/api/v1/threads?view=inbox", "", &petraThreads)
	if len(petraThreads.Threads) == 0 {
		t.Fatal("the reply must thread back at petra")
	}
	threadID := petraThreads.Threads[0].ID
	var msgs int
	origin.DB.QueryRow(`SELECT COUNT(*) FROM messages WHERE thread_id=?`, threadID).Scan(&msgs)
	if msgs < 2 {
		t.Fatalf("reply threading (D-110): %d messages in the topic thread", msgs)
	}
	if code := petra.json(http.MethodPost, fmt.Sprintf("/api/v1/threads/%d/done", threadID), "{}", nil); code != 200 {
		t.Fatalf("sweep done: %d", code)
	}

	// ---- 6. `have` answering a resend: petra — now a correspondent,
	// thanks to the reply — resends the preview novak already holds.
	// Tier 1 discloses possession (§7.5): the verdict answers `have`,
	// nothing is pushed, and the accept completes instantly.
	resend := map[string]any{
		"subject": "Resend: the preview again", "body_content": "<p>again</p>",
		"recipients": []string{"novak@target.demo"},
		"manifest": []map[string]any{
			{"urn": previewURN, "size": len(preview), "type": "text/plain",
				"available_until": "2026-12-01T00:00:00Z"},
		},
	}
	resendJSON, _ := json.Marshal(resend)
	petra.json(http.MethodPost, "/api/v1/drafts", string(resendJSON), &created)
	petra.json(http.MethodPost, "/api/v1/drafts/"+created.ID+"/send", "{}", &sent)
	var instant struct {
		State   string `json:"state"`
		Instant bool   `json:"instant"`
	}
	var haveVerdict string
	target.DB.QueryRow(`SELECT vm.verdict FROM verdict_media vm JOIN verdicts v ON v.id=vm.verdict_row
		 WHERE v.direction='out' AND vm.urn=? ORDER BY v.id DESC LIMIT 1`, previewURN).Scan(&haveVerdict)
	if haveVerdict != "have" {
		t.Fatalf("a correspondent's resend of a held object must answer have (§7.5): %q", haveVerdict)
	}
	if code := novak.json(http.MethodPost, "/api/v1/o/"+previewURN+"/accept", "{}", &instant); code != 200 ||
		!instant.Instant || instant.State != "available" {
		t.Fatalf("have must answer the resend instantly: %d %+v", 200, instant)
	}

	// ---- 7. The guest funnel: link → PIN → claim → instant-have. -----
	guestDraft := map[string]any{
		"subject": "For you — no account needed", "body_content": "<p>Enjoy!</p>",
		"recipients": []string{}, "guests": []string{"friend@example.net"}, "guest_pin": true,
		"manifest": []map[string]any{
			{"urn": previewURN, "size": len(preview), "type": "text/plain",
				"available_until": "2026-12-01T00:00:00Z"},
		},
	}
	gJSON, _ := json.Marshal(guestDraft)
	petra.json(http.MethodPost, "/api/v1/drafts", string(gJSON), &created)
	var gSent struct {
		Guests []struct {
			Path string `json:"path"`
			PIN  string `json:"pin"`
		} `json:"guests"`
	}
	petra.json(http.MethodPost, "/api/v1/drafts/"+created.ID+"/send", "{}", &gSent)
	if len(gSent.Guests) != 1 {
		t.Fatalf("guest link: %+v", gSent)
	}
	token := strings.TrimPrefix(gSent.Guests[0].Path, "/g/")
	pin := gSent.Guests[0].PIN
	guest := &client{t: t, base: origin.base}
	claimResp := guest.do(http.MethodPost, "/api/v1/guest/"+token+"/claim",
		`{"local_part":"friend"}`, map[string]string{"X-MLP-Guest-PIN": pin})
	if claimResp.StatusCode != 200 {
		t.Fatalf("claim: %d", claimResp.StatusCode)
	}
	for _, ck := range claimResp.Cookies() {
		if ck.Name == "mlp_session" {
			guest.cookie = ck
		}
	}
	claimResp.Body.Close()
	if code := guest.json(http.MethodPost, "/api/v1/o/"+previewURN+"/accept", "{}", &instant); code != 200 ||
		!instant.Instant {
		t.Fatalf("the funnel's instant-have: %d %+v", code, instant)
	}
}

func readObject(n *demoNode, urn string) ([]byte, error) {
	f, err := io.ReadAll(mustOpen(n, urn))
	return f, err
}

func mustOpen(n *demoNode, urn string) io.Reader {
	f, err := os.Open(n.BS.ObjectPath(urn))
	if err != nil {
		return strings.NewReader("")
	}
	return f
}
