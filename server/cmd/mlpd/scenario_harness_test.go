package main

// The scenario harness: fixtures shared by every TestScenario* —
// real mlpd nodes on real TCP sockets, exactly as TestTwoDomainDemo
// runs, factored so each scenario reads as its own story. Run any
// scenario alone:
//
//	go test ./cmd/mlpd/ -run TestScenarioCustodySurvivesOriginDeath -v
//
// The full catalog with what each scenario proves: demo/SCENARIOS.md.

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// world is N domains sharing one peer map and one controllable clock.
type world struct {
	t     *testing.T
	clock time.Time
	nodes map[string]*demoNode
}

// newWorld boots one node per (domain, firstUser) pair, all peered,
// all on the shared clock starting 2026-07-12T12:00:00Z.
func newWorld(t *testing.T, domains map[string]string) *world {
	t.Helper()
	w := &world{t: t, nodes: map[string]*demoNode{},
		clock: time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)}
	peers := map[string]string{}
	type pending struct {
		l    net.Listener
		base string
	}
	pend := map[string]pending{}
	for domain := range domains {
		l, base := reserveListener(t)
		peers[domain] = base
		pend[domain] = pending{l, base}
	}
	for domain, user := range domains {
		p := pend[domain]
		n, err := buildNode(config{
			Domain: domain, SelfBase: p.base, DataDir: t.TempDir(),
			InitUser: user, Password: "correct horse", Origin: p.base,
			Peers: peers, Clock: &w.clock,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { n.Close() })
		ts := &httptest.Server{Listener: p.l, Config: &http.Server{Handler: n.mux}}
		ts.Start()
		t.Cleanup(ts.Close)
		w.nodes[domain] = &demoNode{node: n, base: p.base, ts: ts}
	}
	return w
}

func (w *world) node(domain string) *demoNode { return w.nodes[domain] }

// login opens a client session for local@domain.
func (w *world) login(addr string) *client {
	w.t.Helper()
	var domain string
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == '@' {
			domain = addr[i+1:]
			break
		}
	}
	n, ok := w.nodes[domain]
	if !ok {
		w.t.Fatalf("no node for %s", domain)
	}
	return loginDemo(w.t, n.base, addr)
}

// advance moves the shared clock.
func (w *world) advance(d time.Duration) { w.clock = w.clock.Add(d) }

// pushAll drives every node's pending pushes once.
func (w *world) pushAll() {
	for _, n := range w.nodes {
		n.pushOnce(context.Background())
	}
}

// --- composition helpers ------------------------------------------------

type draftSpec struct {
	Subject    string           `json:"subject"`
	Body       string           `json:"body_content"`
	Recipients []string         `json:"recipients"`
	Guests     []string         `json:"guests,omitempty"`
	GuestPIN   bool             `json:"guest_pin,omitempty"`
	JobTag     string           `json:"job_tag,omitempty"`
	InReplyTo  string           `json:"in_reply_to,omitempty"`
	Manifest   []map[string]any `json:"manifest,omitempty"`
}

func entrySpec(urn string, size int, name string) map[string]any {
	return map[string]any{"urn": urn, "size": size, "type": "application/octet-stream",
		"name": name, "available_until": "2026-12-01T00:00:00Z"}
}

type sendOutcome struct {
	DeliveryID int64  `json:"delivery_id"`
	MedialetCA string `json:"medialet_ca"`
	Targets    []struct {
		Domain  string `json:"domain"`
		Message string `json:"message"`
	} `json:"targets"`
	Guests []struct {
		Recipient string `json:"recipient"`
		Path      string `json:"path"`
		PIN       string `json:"pin"`
	} `json:"guests"`
}

// send creates a draft and sends it, failing the test on anything
// but 200 send + accepted targets (pass wantAccepted=false to keep
// refusals for inspection). Every protocol-visible operation first
// advances the world clock one second: with time frozen, causally
// ordered verdicts would share one RFC 3339 `created` and §7.6
// supersession would fall to the verdict_id tiebreak — which real
// deployments resolve by UUIDv7 millisecond ordering, unavailable
// under a frozen clock.
func (w *world) send(c *client, d draftSpec, wantAccepted bool) sendOutcome {
	t := w.t
	t.Helper()
	w.advance(time.Second)
	body, _ := json.Marshal(d)
	var created struct {
		ID string `json:"id"`
	}
	if code := c.json(http.MethodPost, "/api/v1/drafts", string(body), &created); code != 201 {
		t.Fatalf("draft: %d", code)
	}
	var out sendOutcome
	if code := c.json(http.MethodPost, "/api/v1/drafts/"+created.ID+"/send", "{}", &out); code != 200 {
		t.Fatalf("send: %d", code)
	}
	if wantAccepted {
		for _, tg := range out.Targets {
			if tg.Message != "accepted" {
				t.Fatalf("target %s: %s", tg.Domain, tg.Message)
			}
		}
	}
	return out
}

// acceptResult mirrors the three shapes handleAccept answers with:
// the instant-have path ({state:"available", instant:true}), the
// direct deferred upgrade ({mode:"upgraded"}), and the forwarded
// delegation ({mode:"delegated", outcomes:[...]}).
type acceptResult struct {
	State   string `json:"state"`
	Instant bool   `json:"instant"`
	Mode    string `json:"mode"`
	Detail  string `json:"detail"`
}

// accept POSTs the accept and returns the parsed result (see send
// for the clock advancement rationale).
func (w *world) accept(c *client, urn string) acceptResult {
	t := w.t
	t.Helper()
	w.advance(time.Second)
	var res acceptResult
	code := c.json(http.MethodPost, "/api/v1/o/"+urn+"/accept", "{}", &res)
	if code != 200 {
		t.Fatalf("accept %s: %d %s", urn[:24], code, res.Detail)
	}
	return res
}

// refState reads one mailbox-scoped reference state at a node.
func refState(t *testing.T, n *demoNode, urn string) (state, cause string) {
	t.Helper()
	n.DB.QueryRow(`SELECT state, COALESCE(cause,'') FROM refs WHERE urn=? ORDER BY id DESC LIMIT 1`,
		urn).Scan(&state, &cause)
	return
}

func objectLive(n *demoNode, urn string) bool {
	var st string
	n.DB.QueryRow(`SELECT state FROM objects WHERE urn=?`, urn).Scan(&st)
	return st == "live"
}

// lastVerdictFor reads the most recent outbound media verdict a node
// issued for a urn (grant | defer | have | deny).
func lastVerdictFor(t *testing.T, n *demoNode, urn string) string {
	t.Helper()
	var v string
	n.DB.QueryRow(`SELECT vm.verdict FROM verdict_media vm
		JOIN verdicts vv ON vv.id = vm.verdict_row
		WHERE vv.direction='out' AND vm.urn=? ORDER BY vv.id DESC LIMIT 1`, urn).Scan(&v)
	return v
}

// threads lists a lens for a client.
func threads(t *testing.T, c *client, view string) []map[string]any {
	t.Helper()
	var out struct {
		Threads []map[string]any `json:"threads"`
	}
	if code := c.json(http.MethodGet, "/api/v1/threads?view="+view, "", &out); code != 200 {
		t.Fatalf("threads: %d", code)
	}
	return out.Threads
}

func bytesOf(n int, seed byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = seed + byte(i%17)
	}
	return b
}
