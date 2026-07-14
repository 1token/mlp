package main

// Simple scenarios — each one story, one property, over real TCP
// sockets. Catalog with anchors: demo/SCENARIOS.md.

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"medialet.org/mlp/clientapi"
)

// TestScenarioSameDomainDelivery: two mailboxes on ONE domain. The
// dispatch loops back through the node's own SN (real ingest, no
// shortcut), and because the object is already in the domain's
// store, the recipient's accept completes instantly — offered →
// available with zero bytes moved (D-241).
func TestScenarioSameDomainDelivery(t *testing.T) {
	w := newWorld(t, map[string]string{"solo.demo": "petra"})
	n := w.node("solo.demo")
	// Second mailbox on the same domain.
	if _, err := n.DB.Exec(`INSERT INTO mailboxes (local_part, created) VALUES ('novak', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	hash, _ := clientapi.HashPassword("correct horse", 1000)
	n.DB.Exec(`INSERT INTO password_fallback (mailbox_id, hash) VALUES (2, ?)`, hash)

	petra := w.login("petra@solo.demo")
	novak := w.login("novak@solo.demo")

	data := bytesOf(4096, 'S')
	urn := upload(t, petra, data, false)
	out := w.send(petra, draftSpec{
		Subject: "Same roof", Body: "<p>next door</p>",
		Recipients: []string{"novak@solo.demo"},
		Manifest:   []map[string]any{entrySpec(urn, len(data), "s.bin")},
	}, true)
	if out.MedialetCA == "" {
		t.Fatal("no medialet CA")
	}
	if len(threads(t, novak, "inbox")) != 1 {
		t.Fatal("the delivery must arrive in novak's inbox")
	}
	res := w.accept(novak, urn)
	if res.State != "available" || !res.Instant {
		t.Fatalf("same-domain accept must be instant (D-241): %s instant=%v", res.State, res.Instant)
	}
}

// TestScenarioConversationLifecycle: a three-message conversation
// across two domains threads into ONE topic per side (D-110), and
// the thread lifecycle verbs (read, done, flag) hold.
func TestScenarioConversationLifecycle(t *testing.T) {
	w := newWorld(t, map[string]string{"origin.demo": "petra", "target.demo": "novak"})
	petra := w.login("petra@origin.demo")
	novak := w.login("novak@target.demo")

	first := w.send(petra, draftSpec{Subject: "Plans?", Body: "<p>Friday?</p>",
		Recipients: []string{"novak@target.demo"}}, true)
	reply := w.send(novak, draftSpec{Subject: "Re: Plans?", Body: "<p>Friday works.</p>",
		Recipients: []string{"petra@origin.demo"}, InReplyTo: first.MedialetCA}, true)
	w.send(petra, draftSpec{Subject: "Re: Re: Plans?", Body: "<p>Booked.</p>",
		Recipients: []string{"novak@target.demo"}, InReplyTo: reply.MedialetCA}, true)

	for who, c := range map[string]*client{"petra": petra, "novak": novak} {
		th := threads(t, c, "inbox")
		if len(th) != 1 {
			t.Fatalf("%s: the conversation must be ONE thread (D-110), got %d", who, len(th))
		}
	}
	// Novak's copy holds all three: petra's two + his own reply.
	novakThreadID := int64(threads(t, novak, "inbox")[0]["id"].(float64))
	var msgs int
	w.node("target.demo").DB.QueryRow(`SELECT COUNT(*) FROM messages WHERE thread_id=?`, novakThreadID).Scan(&msgs)
	if msgs != 3 {
		t.Fatalf("novak's topic thread must hold 3 messages, got %d", msgs)
	}
	for _, verb := range []string{"read", "flag", "done"} {
		if code := novak.json(http.MethodPost,
			"/api/v1/threads/"+itoa(novakThreadID)+"/"+verb, "{}", nil); code/100 != 2 {
			t.Fatalf("%s: %d", verb, code)
		}
	}
}

// TestScenarioAttachByReference: content the store already holds is
// attached by address alone — the have-check answers true and no
// second upload happens (D-135's reference half).
func TestScenarioAttachByReference(t *testing.T) {
	w := newWorld(t, map[string]string{"origin.demo": "petra", "target.demo": "novak"})
	petra := w.login("petra@origin.demo")

	data := bytesOf(64<<10, 'R')
	urn := upload(t, petra, data, false)

	// The declare on already-held content answers have:true — the
	// upload helper short-circuits without PATCHing a byte.
	again := upload(t, petra, data, false)
	if again != urn {
		t.Fatal("address mismatch")
	}
	var reservations int
	w.node("origin.demo").DB.QueryRow(
		`SELECT COUNT(*) FROM reservations_in WHERE urn=?`, urn).Scan(&reservations)
	if reservations != 1 {
		t.Fatalf("the second declare must not open a second upload: %d reservations", reservations)
	}
	w.send(petra, draftSpec{Subject: "By reference", Body: "<p>held</p>",
		Recipients: []string{"novak@target.demo"},
		Manifest:   []map[string]any{entrySpec(urn, len(data), "r.bin")}}, true)
}

// TestScenarioTierLifecycle: block silences a sender (their next
// delivery lands quarantined, media denied); release grants the
// allow override, which outranks the default stranger posture
// (D-162/D-163/D-165).
func TestScenarioTierLifecycle(t *testing.T) {
	w := newWorld(t, map[string]string{"origin.demo": "petra", "target.demo": "novak"})
	petra := w.login("petra@origin.demo")
	novak := w.login("novak@target.demo")

	w.send(petra, draftSpec{Subject: "One", Body: "<p>hello</p>",
		Recipients: []string{"novak@target.demo"}}, true)
	inbox := threads(t, novak, "inbox")
	if len(inbox) != 1 {
		t.Fatal("the stranger's first delivery lands in the inbox (Tier 2 accepts)")
	}
	threadID := itoa(int64(inbox[0]["id"].(float64)))
	if code := novak.json(http.MethodPost, "/api/v1/threads/"+threadID+"/block", "{}", nil); code/100 != 2 {
		t.Fatalf("block: %d", code)
	}

	data := bytesOf(2048, 'B')
	urn := upload(t, petra, data, false)
	out := w.send(petra, draftSpec{Subject: "Two", Body: "<p>again</p>",
		Recipients: []string{"novak@target.demo"},
		Manifest:   []map[string]any{entrySpec(urn, len(data), "b.bin")}}, false)
	if out.Targets[0].Message != "quarantined" {
		t.Fatalf("the blocked sender's verdict says quarantined — visible to the sender as policy, not a bounce (D-163): %s", out.Targets[0].Message)
	}
	if got := len(threads(t, novak, "inbox")); got != 0 {
		t.Fatalf("block sweeps the sender out of the inbox entirely: %d threads remain", got)
	}
	if got := len(threads(t, novak, "junk")); got == 0 {
		t.Fatal("the blocked sender's deliveries live in junk, for the record (D-165)")
	}
	if v := lastVerdictFor(t, w.node("target.demo"), urn); v != "defer" && v != "deny" {
		t.Fatalf("a blocked sender's media is never granted: %q", v)
	}

	// Release the junked thread: petra gains `allow`.
	junk := threads(t, novak, "junk")
	junkID := itoa(int64(junk[0]["id"].(float64)))
	if code := novak.json(http.MethodPost, "/api/v1/threads/"+junkID+"/release", "{}", nil); code/100 != 2 {
		t.Fatalf("release: %d", code)
	}
	w.send(petra, draftSpec{Subject: "Three", Body: "<p>welcome back</p>",
		Recipients: []string{"novak@target.demo"}}, true)
	if got := len(threads(t, novak, "inbox")); got < 2 {
		t.Fatalf("the released sender's next delivery reaches the inbox: %d", got)
	}
}

// TestScenarioMultiRecipientFanout: one draft, recipients on two
// domains — one envelope PER DOMAIN, and neither stored envelope
// names the other domain's recipients (Bcc-by-construction, D-04).
func TestScenarioMultiRecipientFanout(t *testing.T) {
	w := newWorld(t, map[string]string{
		"origin.demo": "petra", "target.demo": "novak", "final.demo": "carol"})
	petra := w.login("petra@origin.demo")

	out := w.send(petra, draftSpec{Subject: "Fan-out", Body: "<p>both of you</p>",
		Recipients: []string{"novak@target.demo", "carol@final.demo"}}, true)
	if len(out.Targets) != 2 {
		t.Fatalf("two domains, two dispatches: %d", len(out.Targets))
	}
	// D-04 proven at the source of truth: the origin's per-domain
	// dispatch records hold the canonical envelopes that went out.
	for domain, other := range map[string]string{
		"target.demo": "carol@final.demo", "final.demo": "novak@target.demo"} {
		var envelope string
		w.node("origin.demo").DB.QueryRow(
			`SELECT envelope_canonical FROM dispatches WHERE target_domain=?`, domain).Scan(&envelope)
		if envelope == "" {
			t.Fatalf("%s: no dispatch record", domain)
		}
		if strings.Contains(envelope, other) {
			t.Fatalf("the %s envelope must not name %s (D-04 envelope privacy)", domain, other)
		}
	}
	for _, addr := range []string{"novak@target.demo", "carol@final.demo"} {
		c := w.login(addr)
		if len(threads(t, c, "inbox")) != 1 {
			t.Fatalf("%s: delivery missing", addr)
		}
	}
}

// TestScenarioIdempotentSend: replaying a send with the same
// Idempotency-Key must not dispatch twice (D-169 — the offline
// queue's safety net).
func TestScenarioIdempotentSend(t *testing.T) {
	w := newWorld(t, map[string]string{"origin.demo": "petra", "target.demo": "novak"})
	petra := w.login("petra@origin.demo")
	novak := w.login("novak@target.demo")

	body := `{"subject":"Once","body_content":"<p>only once</p>","recipients":["novak@target.demo"]}`
	var created struct {
		ID string `json:"id"`
	}
	petra.json(http.MethodPost, "/api/v1/drafts", body, &created)

	key := "scenario-idempotent-send-0001"
	do := func() int {
		resp := petra.do(http.MethodPost, "/api/v1/drafts/"+created.ID+"/send", "{}",
			map[string]string{"Idempotency-Key": key})
		defer resp.Body.Close()
		return resp.StatusCode
	}
	first, second := do(), do()
	if first != 200 || second != 200 {
		t.Fatalf("send replays: %d then %d", first, second)
	}
	var dispatches int
	w.node("target.demo").DB.QueryRow(`SELECT COUNT(*) FROM envelopes_in`).Scan(&dispatches)
	if dispatches != 1 {
		t.Fatalf("one key, one dispatch (D-169): got %d envelopes", dispatches)
	}
	if got := len(threads(t, novak, "inbox")); got != 1 {
		t.Fatalf("novak sees the delivery once: %d", got)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
