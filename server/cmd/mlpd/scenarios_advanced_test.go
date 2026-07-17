package main

// Advanced scenarios — forwarding in both modes, custody surviving
// the origin's death, loop prevention, guest-link edges under a
// moving clock, and GC over sockets. Catalog: demo/SCENARIOS.md.
//
// The harness calls SN.Forward directly (no client API endpoint
// exists for forwarding yet — it is client backlog); the forwarded
// envelope then travels over the real socket to the receiving
// domain's /dispatch, exactly as a client-initiated forward would.

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"medialet.org/mlp/sn"
)

// forwardOverSocket runs Forward at the holder and dispatches the
// result to the recipient domain over HTTP.
func forwardOverSocket(t *testing.T, w *world, holder *demoNode, origin, envID string,
	to []string, forwardedBy string, mode sn.ForwardMode, automatic bool, until string) *http.Response {
	t.Helper()
	w.advance(time.Second)
	canon, err := holder.SN.Forward(context.Background(), origin, envID, to, forwardedBy, "", mode, automatic, until)
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	domain := to[0][strings.LastIndex(to[0], "@")+1:]
	resp, err := http.Post(w.node(domain).base+"/dispatch",
		"application/mlp-envelope+json", bytes.NewReader(canon))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// receivedEnvelopeID finds the envelope a domain received for a CA.
func receivedEnvelopeID(t *testing.T, n *demoNode, ca string) (origin, envID string) {
	t.Helper()
	if err := n.DB.QueryRow(`SELECT origin, envelope_id FROM envelopes_in WHERE medialet_ca=? ORDER BY id DESC LIMIT 1`,
		ca).Scan(&origin, &envID); err != nil {
		t.Fatalf("received envelope for %s: %v", ca[:24], err)
	}
	return
}

// TestScenarioDelegatedForwarding: petra → novak; novak forwards
// (Delegated) to carol on a third domain. Carol's domain fulfills
// from the CHAIN — novak listed the sources he knew (himself not a
// custodian in Delegated mode, so the origin) — and the bytes
// arrive at final.demo from petra's domain. The author signature
// and the content address survive both hops untouched (§9.2, D-84).
func TestScenarioDelegatedForwarding(t *testing.T) {
	w := newWorld(t, map[string]string{
		"origin.demo": "petra", "target.demo": "novak", "final.demo": "carol"})
	petra := w.login("petra@origin.demo")
	carol := w.login("carol@final.demo")

	data := bytesOf(5<<20, 'D') // above the D-139 line: defers, so accept exercises delegation
	urn := upload(t, petra, data, false)
	out := w.send(petra, draftSpec{Subject: "Original", Body: "<p>for whoever needs it</p>",
		Recipients: []string{"novak@target.demo"},
		Manifest:   []map[string]any{entrySpec(urn, len(data), "d.bin")}}, true)

	origin, envID := receivedEnvelopeID(t, w.node("target.demo"), out.MedialetCA)
	resp := forwardOverSocket(t, w, w.node("target.demo"), origin, envID,
		[]string{"carol@final.demo"}, "novak@target.demo", sn.Delegated, false, "")
	if resp.StatusCode != 200 {
		t.Fatalf("forwarded dispatch: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Carol sees petra's authorship, not novak's.
	th := threads(t, carol, "inbox")
	if len(th) != 1 {
		t.Fatalf("carol's inbox: %d threads", len(th))
	}
	var author, ca string
	w.node("final.demo").DB.QueryRow(
		`SELECT author, content_address FROM medialets LIMIT 1`).Scan(&author, &ca)
	if author != "petra@origin.demo" || ca != out.MedialetCA {
		t.Fatalf("the Medialet must survive forwarding byte-identical: author=%s ca match=%v",
			author, ca == out.MedialetCA)
	}

	// Accept → delegation walks the chain → origin will-push.
	if res := w.accept(carol, urn); res.Mode != "delegated" {
		t.Fatalf("a forwarded delivery accepts through §9.3 delegation: %+v", res)
	}
	w.pushAll()
	if !objectLive(w.node("final.demo"), urn) {
		t.Fatal("the bytes must arrive at final.demo from the chain")
	}
	if state, _ := refState(t, w.node("final.demo"), urn); state != "available" {
		t.Fatalf("carol's reference: %q", state)
	}
}

// TestScenarioCustodySurvivesOriginDeath: novak takes the bytes,
// custody-forwards to carol with an `until` REACHING PAST petra's
// manifest window (MEP-001), and then petra's whole domain goes
// dark. Carol accepts anyway: fulfillment lands on novak's domain —
// the custodian honoring the window he himself hop-signed (§9.5) —
// and the bytes arrive from target.demo while origin.demo is a
// closed socket.
func TestScenarioCustodySurvivesOriginDeath(t *testing.T) {
	w := newWorld(t, map[string]string{
		"origin.demo": "petra", "target.demo": "novak", "final.demo": "carol"})
	petra := w.login("petra@origin.demo")
	novak := w.login("novak@target.demo")
	carol := w.login("carol@final.demo")

	data := bytesOf(5<<20, 'C') // defers: novak's custody is a real accept, not an auto-grant
	urn := upload(t, petra, data, false)
	out := w.send(petra, draftSpec{Subject: "Handover", Body: "<p>keep this safe</p>",
		Recipients: []string{"novak@target.demo"},
		Manifest:   []map[string]any{entrySpec(urn, len(data), "c.bin")}}, true)

	// Novak takes custody of the bytes first (direct §7.6 upgrade).
	if res := w.accept(novak, urn); res.Mode != "upgraded" {
		t.Fatalf("novak accept: %+v", res)
	}
	w.pushAll()
	if !objectLive(w.node("target.demo"), urn) {
		t.Fatal("novak must hold the bytes before custody means anything")
	}

	// Custody forward, window past the author's 2026-12-01.
	origin, envID := receivedEnvelopeID(t, w.node("target.demo"), out.MedialetCA)
	resp := forwardOverSocket(t, w, w.node("target.demo"), origin, envID,
		[]string{"carol@final.demo"}, "novak@target.demo", sn.Custody, false,
		"2027-03-01T00:00:00Z")
	if resp.StatusCode != 200 {
		t.Fatalf("custody dispatch: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// MEP-001: carol's effective deadline is the custodian's window.
	var until string
	w.node("final.demo").DB.QueryRow(`SELECT available_until FROM refs WHERE urn=?`, urn).Scan(&until)
	if until != "2027-03-01T00:00:00Z" {
		t.Fatalf("the effective deadline must be the covering custody window (MEP-001): %q", until)
	}

	// Petra's domain dies. Entirely.
	w.node("origin.demo").ts.Close()

	if res := w.accept(carol, urn); res.Mode != "delegated" {
		t.Fatalf("carol accept with a dead origin: %+v", res)
	}
	w.node("target.demo").pushOnce(context.Background()) // the custodian pushes
	if !objectLive(w.node("final.demo"), urn) {
		t.Fatal("custody must survive the origin's death (§9.5/MEP-001)")
	}
	got, err := readObject(w.node("final.demo"), urn)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("byte integrity through custody")
	}
}

// TestScenarioForwardLoopPrevention: D-51's loop signature is an
// envelope arriving back at a domain ALREADY IN ITS CHAIN and an
// automatic rule trying to dispatch it onward again. The walk:
// petra → novak (deliberate forward) → archive@origin.demo — home
// again. An automatic onward forward AT THE ORIGIN is refused; a
// deliberate one passes, because the human is allowed to mean it.
func TestScenarioForwardLoopPrevention(t *testing.T) {
	w := newWorld(t, map[string]string{
		"origin.demo": "petra", "target.demo": "novak"})
	petra := w.login("petra@origin.demo")
	n := w.node("origin.demo")
	n.DB.Exec(`INSERT INTO mailboxes (local_part, created) VALUES ('archive', '2026-01-01T00:00:00Z')`)

	out := w.send(petra, draftSpec{Subject: "Loopable", Body: "<p>around we go</p>",
		Recipients: []string{"novak@target.demo"}}, true)
	origin, envID := receivedEnvelopeID(t, w.node("target.demo"), out.MedialetCA)

	// Leg 2: novak deliberately forwards it home to the archive.
	resp := forwardOverSocket(t, w, w.node("target.demo"), origin, envID,
		[]string{"archive@origin.demo"}, "novak@target.demo", sn.Delegated, false, "")
	if resp.StatusCode != 200 {
		t.Fatalf("the deliberate forward home must pass: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// The envelope is home; origin.demo is the chain's root. An
	// AUTOMATIC onward dispatch here is the mail loop — refused.
	fwdOrigin, fwdEnvID := receivedEnvelopeID(t, n, out.MedialetCA)
	_, err := n.SN.Forward(context.Background(), fwdOrigin, fwdEnvID,
		[]string{"novak@target.demo"}, "", "", sn.Delegated, true /* automatic */, "")
	if err == nil {
		t.Fatal("an automatic re-dispatch at a chain member must be refused (D-51)")
	}
	// The same forward, deliberately: allowed.
	resp2 := forwardOverSocket(t, w, n, fwdOrigin, fwdEnvID,
		[]string{"novak@target.demo"}, "archive@origin.demo", sn.Delegated, false, "")
	if resp2.StatusCode != 200 {
		t.Fatalf("the deliberate re-dispatch must pass: %d", resp2.StatusCode)
	}
	resp2.Body.Close()
}

// TestScenarioResendAfterDeletion: novak holds, deletes (honest
// tombstone), petra resends. Possession is disclosed honestly: the
// verdict is grant (not have — he no longer holds it), the bytes
// travel again, and the reference lives a second life.
func TestScenarioResendAfterDeletion(t *testing.T) {
	w := newWorld(t, map[string]string{
		"origin.demo": "petra", "target.demo": "novak"})
	petra := w.login("petra@origin.demo")
	novak := w.login("novak@target.demo")

	data := bytesOf(8<<10, 'X')
	urn := upload(t, petra, data, false)
	w.send(petra, draftSpec{Subject: "First life", Body: "<p>here</p>",
		Recipients: []string{"novak@target.demo"},
		Manifest:   []map[string]any{entrySpec(urn, len(data), "x.bin")}}, true)
	// The reply makes petra a correspondent, so possession WOULD be
	// disclosed as `have` — which is exactly what deletion must undo.
	w.send(novak, draftSpec{Subject: "Re: First life", Body: "<p>got it</p>",
		Recipients: []string{"petra@origin.demo"}}, true)
	w.pushAll() // the small object auto-granted (D-139): bytes flow unprompted
	if !objectLive(w.node("target.demo"), urn) {
		t.Fatal("setup: bytes must land")
	}
	if state, _ := refState(t, w.node("target.demo"), urn); state != "available" {
		t.Fatalf("the auto-granted object is available with no ceremony (D-139): %q", state)
	}

	if code := novak.json(http.MethodDelete, "/api/v1/o/"+urn, "", nil); code/100 != 2 {
		t.Fatalf("delete: %d", code)
	}
	if objectLive(w.node("target.demo"), urn) {
		t.Fatal("deleted means gone")
	}
	if state, cause := refState(t, w.node("target.demo"), urn); state != "unavailable" || cause != "deleted" {
		t.Fatalf("the honest tombstone (§10.4): %s/%s", state, cause)
	}

	w.send(petra, draftSpec{Subject: "Second life", Body: "<p>again</p>",
		Recipients: []string{"novak@target.demo"},
		Manifest:   []map[string]any{entrySpec(urn, len(data), "x.bin")}}, true)
	if v := lastVerdictFor(t, w.node("target.demo"), urn); v != "grant" {
		t.Fatalf("a deleted object is not `have` — possession claims stay honest, even for a correspondent: %q", v)
	}
	w.pushAll() // the fresh grant's bytes travel again
	if !objectLive(w.node("target.demo"), urn) {
		t.Fatal("the second life's bytes")
	}
	if state, _ := refState(t, w.node("target.demo"), urn); state != "available" {
		t.Fatalf("the second life: %q", state)
	}
}

// TestScenarioGuestLockAndExpiry: five wrong PINs lock the link
// (423, checked BEFORE PIN evaluation — D-155/D-238); a fresh link
// expires 30 days out under the moving clock.
func TestScenarioGuestLockAndExpiry(t *testing.T) {
	w := newWorld(t, map[string]string{"origin.demo": "petra"})
	petra := w.login("petra@origin.demo")

	data := bytesOf(1024, 'G')
	urn := upload(t, petra, data, false)
	out := w.send(petra, draftSpec{Subject: "For a friend", Body: "<p>enjoy</p>",
		Guests: []string{"friend@example.net"}, GuestPIN: true, Recipients: []string{},
		Manifest: []map[string]any{entrySpec(urn, len(data), "g.bin")}}, true)
	if len(out.Guests) != 1 {
		t.Fatalf("guest link: %+v", out.Guests)
	}
	token, pin := strings.TrimPrefix(out.Guests[0].Path, "/g/"), out.Guests[0].PIN

	guest := &client{t: t, base: w.node("origin.demo").base}
	get := func(hdrPIN string) int {
		resp := guest.do(http.MethodGet, "/api/v1/guest/"+token, "",
			map[string]string{"X-MLP-Guest-PIN": hdrPIN})
		resp.Body.Close()
		return resp.StatusCode
	}
	for i := 0; i < 5; i++ {
		if code := get("000000"); code != 401 {
			t.Fatalf("wrong PIN %d: %d", i+1, code)
		}
	}
	if code := get(pin); code != 423 {
		t.Fatalf("the lock outranks even the CORRECT pin (D-155): %d", code)
	}

	// A fresh link on a new delivery: fine today, dead in 31 days.
	out2 := w.send(petra, draftSpec{Subject: "Another", Body: "<p>again</p>",
		Guests: []string{"other@example.net"}, GuestPIN: true, Recipients: []string{},
		Manifest: []map[string]any{entrySpec(urn, len(data), "g.bin")}}, true)
	token2, pin2 := strings.TrimPrefix(out2.Guests[0].Path, "/g/"), out2.Guests[0].PIN
	get2 := func() int {
		resp := guest.do(http.MethodGet, "/api/v1/guest/"+token2, "",
			map[string]string{"X-MLP-Guest-PIN": pin2})
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := get2(); code != 200 {
		t.Fatalf("fresh link: %d", code)
	}
	w.advance(31 * 24 * time.Hour)
	if code := get2(); code != 410 {
		t.Fatalf("a 30-day link is gone on day 31 (D-152): %d", code)
	}
}

// TestScenarioEphemeralGC: an auto-granted preview (ephemeral class)
// is collected once nothing needs it; the accepted, PINNED master is
// untouchable (§10.5 invariant 1 over sockets).
func TestScenarioEphemeralGC(t *testing.T) {
	w := newWorld(t, map[string]string{
		"origin.demo": "petra", "target.demo": "novak"})
	petra := w.login("petra@origin.demo")
	novak := w.login("novak@target.demo")

	master := bytesOf(5<<20, 'M') // above the auto-grant line: defers
	preview := bytesOf(2048, 'p') // auto-grants: ephemeral class
	masterURN := upload(t, petra, master, false)
	previewURN := upload(t, petra, preview, false)
	w.send(petra, draftSpec{Subject: "Shoot", Body: "<p>preview inside</p>",
		Recipients: []string{"novak@target.demo"},
		Manifest: []map[string]any{
			entrySpec(masterURN, len(master), "master.raw"),
			func() map[string]any {
				e := entrySpec(previewURN, len(preview), "preview.jpg")
				e["preview_of"] = masterURN
				return e
			}(),
		}}, true)
	w.pushAll() // the auto-granted preview arrives
	if !objectLive(w.node("target.demo"), previewURN) {
		t.Fatal("the preview must arrive on its own")
	}
	w.accept(novak, masterURN)
	w.pushAll()
	if code := novak.json(http.MethodPost, "/api/v1/o/"+masterURN+"/pin", "{}", nil); code/100 != 2 {
		t.Fatalf("pin: %d", code)
	}

	tn := w.node("target.demo")
	collected, prob := tn.SN.CollectGarbage(context.Background(), tn.BS, w.clock)
	if prob != nil {
		t.Fatal(prob)
	}
	got := map[string]bool{}
	for _, c := range collected {
		got[c.URN] = true
	}
	if !got[previewURN] || objectLive(tn, previewURN) {
		t.Fatal("the unpinned ephemeral preview is GC-first (D-139/D-251)")
	}
	if got[masterURN] || !objectLive(tn, masterURN) {
		t.Fatal("the pinned master retains absolutely (§10.5 invariant 1)")
	}
	if state, cause := refState(t, tn, previewURN); state != "unavailable" || cause != "expired-local" {
		t.Fatalf("the preview's tombstone: %s/%s", state, cause)
	}
}
