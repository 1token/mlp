package main

// TestScenarioWorkingGroupExploder — an IETF-style working-group
// mailing list built on MLP primitives, over real TCP sockets.
//
//	go test ./cmd/mlpd/ -run TestScenarioWorkingGroupExploder -v
//
// The cast: medialet-wg@lists.demo is the list (its mailbox owner is
// the moderator). petra@ietfa.demo, novak@ietfb.demo and
// carol@ietfc.demo are working-group members; mirror@archive.demo is
// a second, cross-subscribed exploder — the classic mail-loop
// configuration that has melted real mailing lists.
//
// The exploder itself is an APPLICATION, exactly as mailman is an
// application on SMTP: MLP does not define list membership. The
// roster lives with the exploder; everything the exploder DOES is
// pure protocol — §3.4.2 re-dispatch with automatic=true, because an
// exploder is automation and must say so. That honesty is what lets
// D-51 protect the federation in phase 7.
//
// What the phases prove:
//  1. a member's post reaches the list like any delivery
//  2. the exploder fans out — authorship and content address
//     survive re-dispatch byte-identically at every subscriber
//  3. HEAVY MEDIA NEVER TOUCHES THE LIST: subscribers accept
//     through §9.3 delegation and the draft flows point-to-point
//     from the author's domain; the list domain never holds the
//     object (the structural difference from SMTP exploders and
//     their attachment-size limits)
//  4. the discussion THREADS across the exploder: replies
//     re-distributed by the list join the same topic at every
//     subscriber, because the content address is the message
//     identity (D-110 over re-dispatch)
//  5. moderation is the tier system: a blocked troll's next post
//     arrives `quarantined`, never enters the inbox, and is
//     therefore never exploded; release is moderator approval
//  6. the cross-subscribed loop dies at one revolution: the
//     boomeranged envelope ARRIVES back at the list (provably),
//     but the automatic re-explosion is refused — lists.demo is
//     already in the chain (D-51)

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"medialet.org/mlp/clientapi"
	"medialet.org/mlp/sn"
)

// exploder is the list application: a mailbox, a roster, and one
// rule — automatically re-dispatch inbox arrivals to every
// subscriber except the author.
type exploder struct {
	w       *world
	domain  string   // the list's home domain
	address string   // the list address (forwarded_by disclosure)
	roster  []string // subscriber addresses
}

// explode re-dispatches (origin, envID) to the roster minus the
// author, one Forward per subscriber domain (D-04: each domain's
// envelope names only its own recipients). It returns D-51's
// refusal, if any, for the caller to assert on.
func (e *exploder) explode(t *testing.T, origin, envID, author string) error {
	t.Helper()
	listNode := e.w.node(e.domain)
	byDomain := map[string][]string{}
	for _, member := range e.roster {
		if member == author {
			continue
		}
		d := member[strings.LastIndex(member, "@")+1:]
		byDomain[d] = append(byDomain[d], member)
	}
	for d, members := range byDomain {
		e.w.advance(time.Second)
		canon, err := listNode.SN.Forward(context.Background(), origin, envID,
			members, e.address, sn.Delegated, true /* an exploder IS automation */, "")
		if err != nil {
			return err
		}
		resp, err := http.Post(e.w.node(d).base+"/dispatch",
			"application/mlp-envelope+json", bytes.NewReader(canon))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("explode to %s: %d", d, resp.StatusCode)
		}
		resp.Body.Close()
	}
	return nil
}

// messagesFor counts a mailbox's copies of one Medialet.
func messagesFor(t *testing.T, n *demoNode, mailbox int64, ca string) int {
	t.Helper()
	var c int
	n.DB.QueryRow(`SELECT COUNT(*) FROM messages WHERE mailbox_id=? AND medialet_ca=?`,
		mailbox, ca).Scan(&c)
	return c
}

func TestScenarioWorkingGroupExploder(t *testing.T) {
	w := newWorld(t, map[string]string{
		"lists.demo":   "medialet-wg",
		"archive.demo": "mirror",
		"ietfa.demo":   "petra",
		"ietfb.demo":   "novak",
		"ietfc.demo":   "carol",
	})
	moderator := w.login("medialet-wg@lists.demo")
	petra := w.login("petra@ietfa.demo")
	novak := w.login("novak@ietfb.demo")
	carol := w.login("carol@ietfc.demo")

	wg := &exploder{w: w, domain: "lists.demo", address: "medialet-wg@lists.demo",
		roster: []string{"petra@ietfa.demo", "novak@ietfb.demo", "carol@ietfc.demo"}}

	// ---- Phase 1+2: a member posts an Internet-Draft to the list.
	draft := bytesOf(5<<20, 'I') // above the D-139 line: subscribers accept deliberately
	draftURN := upload(t, petra, draft, false)
	post := w.send(petra, draftSpec{
		Subject:    "[medialet-wg] draft-medialet-core-00",
		Body:       "<p>Working group, the -00 draft is attached. Review by Friday.</p>",
		Recipients: []string{"medialet-wg@lists.demo"},
		Manifest:   []map[string]any{entrySpec(draftURN, len(draft), "draft-medialet-core-00.txt")},
	}, true)
	if len(threads(t, moderator, "inbox")) != 1 {
		t.Fatal("the post reaches the list like any delivery")
	}

	// ---- Phase 3: the exploder fans out.
	origin, envID := receivedEnvelopeID(t, w.node("lists.demo"), post.MedialetCA)
	if err := wg.explode(t, origin, envID, "petra@ietfa.demo"); err != nil {
		t.Fatalf("the list's first explosion must pass D-51 (lists.demo is not in the chain): %v", err)
	}
	for who, c := range map[string]*client{"novak": novak, "carol": carol} {
		if len(threads(t, c, "inbox")) != 1 {
			t.Fatalf("%s: the exploded post must arrive", who)
		}
	}
	// Authorship and identity survive the exploder byte-identically.
	for _, d := range []string{"ietfb.demo", "ietfc.demo"} {
		var author, ca string
		w.node(d).DB.QueryRow(`SELECT author, content_address FROM medialets LIMIT 1`).Scan(&author, &ca)
		if author != "petra@ietfa.demo" || ca != post.MedialetCA {
			t.Fatalf("%s: authorship/identity through the exploder: %s / match=%v", d, author, ca == post.MedialetCA)
		}
	}

	// ---- Phase 4: the draft flows point-to-point; the list holds nothing.
	for _, c := range []*client{novak, carol} {
		if res := w.accept(c, draftURN); res.Mode != "delegated" {
			t.Fatalf("subscribers accept through §9.3 delegation: %+v", res)
		}
	}
	w.pushAll()
	for _, d := range []string{"ietfb.demo", "ietfc.demo"} {
		if !objectLive(w.node(d), draftURN) {
			t.Fatalf("%s: the draft must arrive from the author's domain", d)
		}
		got, err := readObject(w.node(d), draftURN)
		if err != nil || !bytes.Equal(got, draft) {
			t.Fatalf("%s: draft byte integrity", d)
		}
	}
	if objectLive(w.node("lists.demo"), draftURN) {
		t.Fatal("THE POINT: the exploder re-dispatches envelopes, never payloads — the list domain must never hold the draft")
	}

	// ---- Phase 5: the discussion threads across the exploder.
	reply := w.send(novak, draftSpec{
		Subject:    "Re: [medialet-wg] draft-medialet-core-00",
		Body:       "<p>Section 3 needs a normative reference.</p>",
		Recipients: []string{"medialet-wg@lists.demo"},
		InReplyTo:  post.MedialetCA,
	}, true)
	rOrigin, rEnvID := receivedEnvelopeID(t, w.node("lists.demo"), reply.MedialetCA)
	if err := wg.explode(t, rOrigin, rEnvID, "novak@ietfb.demo"); err != nil {
		t.Fatalf("exploding the reply: %v", err)
	}
	// One topic everywhere: carol received both via the list; petra
	// holds her own sent post + the exploded reply.
	for who, c := range map[string]*client{"carol": carol, "petra": petra} {
		th := threads(t, c, "inbox")
		if len(th) != 1 {
			t.Fatalf("%s: the WG discussion is ONE thread (D-110 across re-dispatch), got %d", who, len(th))
		}
	}
	var carolThreadMsgs int
	w.node("ietfc.demo").DB.QueryRow(`SELECT COUNT(*) FROM messages WHERE mailbox_id=1`).Scan(&carolThreadMsgs)
	if carolThreadMsgs != 2 {
		t.Fatalf("carol's topic holds the post and the reply: %d", carolThreadMsgs)
	}

	// ---- Phase 6: moderation is the tier system.
	// A troll appears on carol's domain.
	nc := w.node("ietfc.demo")
	if _, err := nc.DB.Exec(`INSERT INTO mailboxes (local_part, created) VALUES ('troll', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	hash, _ := clientapi.HashPassword("correct horse", 1000)
	nc.DB.Exec(`INSERT INTO password_fallback (mailbox_id, hash) VALUES (2, ?)`, hash)
	troll := w.login("troll@ietfc.demo")

	w.send(troll, draftSpec{Subject: "BUY NOW", Body: "<p>spam</p>",
		Recipients: []string{"medialet-wg@lists.demo"}}, true)
	// The moderator blocks the sender: the record sweeps to junk.
	inbox := threads(t, moderator, "inbox")
	var trollThread string
	for _, th := range inbox {
		if rollup, ok := th["rollup"].(map[string]any); ok && rollup["subject"] == "BUY NOW" {
			trollThread = itoa(int64(th["id"].(float64)))
		}
	}
	if trollThread == "" {
		t.Fatal("the troll's first post lands like any stranger's")
	}
	if code := moderator.json(http.MethodPost, "/api/v1/threads/"+trollThread+"/block", "{}", nil); code/100 != 2 {
		t.Fatalf("block: %d", code)
	}
	// The blocked troll posts again: quarantined at the door, never
	// in the inbox, therefore never exploded.
	before := len(threads(t, moderator, "inbox"))
	out := w.send(troll, draftSpec{Subject: "BUY NOW 2", Body: "<p>spam again</p>",
		Recipients: []string{"medialet-wg@lists.demo"}}, false)
	if out.Targets[0].Message != "quarantined" {
		t.Fatalf("the blocked sender sees policy, not a bounce (D-163): %s", out.Targets[0].Message)
	}
	if got := len(threads(t, moderator, "inbox")); got != before {
		t.Fatalf("the quarantined post must not enter the moderation-passed inbox: %d -> %d", before, got)
	}
	if got := len(threads(t, novak, "inbox")); got != 1 {
		t.Fatalf("nothing to explode, nothing exploded: novak has %d threads", got)
	}
	// Moderator approval: release the sender, the next post flows.
	junk := threads(t, moderator, "junk")
	if len(junk) == 0 {
		t.Fatal("the troll's record waits in junk")
	}
	junkID := itoa(int64(junk[0]["id"].(float64)))
	if code := moderator.json(http.MethodPost, "/api/v1/threads/"+junkID+"/release", "{}", nil); code/100 != 2 {
		t.Fatalf("release: %d", code)
	}
	approved := w.send(troll, draftSpec{Subject: "An actual review", Body: "<p>Section 5 nit.</p>",
		Recipients: []string{"medialet-wg@lists.demo"}}, true)
	aOrigin, aEnvID := receivedEnvelopeID(t, w.node("lists.demo"), approved.MedialetCA)
	if err := wg.explode(t, aOrigin, aEnvID, "troll@ietfc.demo"); err != nil {
		t.Fatalf("exploding the approved post: %v", err)
	}
	if got := len(threads(t, novak, "inbox")); got != 2 {
		t.Fatalf("the approved post reaches the working group: novak has %d threads", got)
	}

	// ---- Phase 7: the cross-subscribed loop dies at one revolution.
	// The archive mirror subscribes to the WG list, and — the classic
	// misconfiguration — the WG list subscribes to the mirror.
	wg.roster = append(wg.roster, "mirror@archive.demo")
	mirror := &exploder{w: w, domain: "archive.demo", address: "mirror@archive.demo",
		roster: []string{"medialet-wg@lists.demo"}}

	announce := w.send(petra, draftSpec{
		Subject:    "[medialet-wg] WGLC announced",
		Body:       "<p>Working group last call starts today.</p>",
		Recipients: []string{"medialet-wg@lists.demo"},
	}, true)
	nOrigin, nEnvID := receivedEnvelopeID(t, w.node("lists.demo"), announce.MedialetCA)
	if err := wg.explode(t, nOrigin, nEnvID, "petra@ietfa.demo"); err != nil {
		t.Fatalf("explosion one: %v", err)
	}
	// The mirror explodes back at the list — legal: archive.demo is
	// not yet in the chain. The envelope boomerangs home.
	mOrigin, mEnvID := receivedEnvelopeID(t, w.node("archive.demo"), announce.MedialetCA)
	if err := mirror.explode(t, mOrigin, mEnvID, "petra@ietfa.demo"); err != nil {
		t.Fatalf("the mirror's explosion is the loop's first revolution and must pass: %v", err)
	}
	// The boomerang PROVABLY arrived: the list holds two Delivery
	// Records for one announcement.
	var arrivals int
	w.node("lists.demo").DB.QueryRow(`SELECT COUNT(*) FROM envelopes_in WHERE medialet_ca=?`,
		announce.MedialetCA).Scan(&arrivals)
	if arrivals != 2 {
		t.Fatalf("the boomerang must arrive before D-51 can matter: %d records", arrivals)
	}
	// The list's exploder picks it up — and D-51 refuses: lists.demo
	// already dispatched this envelope; automation stops here.
	bOrigin, bEnvID := receivedEnvelopeID(t, w.node("lists.demo"), announce.MedialetCA)
	err := wg.explode(t, bOrigin, bEnvID, "petra@ietfa.demo")
	if !errors.Is(err, sn.ErrForwardLoop) {
		t.Fatalf("the cross-subscription loop must die at one revolution (D-51): %v", err)
	}
	// Nobody drowned: every member holds exactly ONE copy.
	for _, m := range []struct {
		domain string
		box    int64
	}{{"ietfb.demo", 1}, {"ietfc.demo", 1}, {"ietfa.demo", 1}} {
		if c := messagesFor(t, w.node(m.domain), m.box, announce.MedialetCA); c != 1 {
			t.Fatalf("%s: exactly one copy despite the loop, got %d", m.domain, c)
		}
	}
}
