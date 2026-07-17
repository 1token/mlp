// Command mlpd runs one MLP domain: the federation server (dispatch,
// fulfillment, verdicts), the Body Store ingest door, the Client API,
// and the static client — the composition every test wires by hand,
// as one process.
//
// Production posture is the default. The -peer flag switches the
// named domains onto explicit HTTP base URLs through
// discovery.NewDemoFetcher — local demonstrations only, and the log
// says so at startup.
//
//	mlpd -domain origin.localhost -listen 127.0.0.1:8441 \
//	     -data ./data/origin -client ../client \
//	     -peer target.localhost=http://127.0.0.1:8442 \
//	     -init petra -password "correct horse"
package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"medialet.org/mlp/bs"
	"medialet.org/mlp/clientapi"
	"medialet.org/mlp/core"
	"medialet.org/mlp/discovery"
	"medialet.org/mlp/search"
	"medialet.org/mlp/sn"
	"medialet.org/mlp/store"
)

type peerFlags map[string]string

func (p peerFlags) String() string { return fmt.Sprint(map[string]string(p)) }
func (p peerFlags) Set(v string) error {
	domain, base, ok := strings.Cut(v, "=")
	if !ok {
		return fmt.Errorf("-peer wants domain=baseURL, got %q", v)
	}
	p[domain] = strings.TrimRight(base, "/")
	return nil
}

func main() {
	var (
		domain   = flag.String("domain", "", "this node's domain (required)")
		listen   = flag.String("listen", "127.0.0.1:8441", "listen address")
		dataDir  = flag.String("data", "./data", "data directory (db + objects)")
		client   = flag.String("client", "", "client directory to serve statically (optional)")
		initUser = flag.String("init", "", "provision a mailbox with this local part on first run")
		password = flag.String("password", "", "password for -init (fallback auth)")
		origin   = flag.String("origin", "", "browser origin for WebAuthn (default http://<listen>)")
		peers    = peerFlags{}
	)
	flag.Var(peers, "peer", "domain=baseURL demo override (repeatable; DEMO ONLY)")
	flag.Parse()
	if *domain == "" {
		log.Fatal("mlpd: -domain is required")
	}
	if err := run(*domain, *listen, *dataDir, *client, *initUser, *password, *origin, peers); err != nil {
		log.Fatal(err)
	}
}

func run(domain, listen, dataDir, clientDir, initUser, password, origin string, peers map[string]string) error {
	n, err := buildNode(config{
		Domain: domain, SelfBase: "http://" + listen, DataDir: dataDir,
		ClientDir: clientDir, InitUser: initUser, Password: password,
		Origin: origin, Peers: peers,
	})
	if err != nil {
		return err
	}
	defer n.Close()
	go func() {
		for {
			time.Sleep(300 * time.Millisecond)
			n.pushOnce(context.Background())
		}
	}()
	go func() {
		// The D-139 sweep: ephemeral-class references release their
		// bytes when nothing pins them (§10.5 invariants); hourly is
		// plenty at prototype scale.
		for {
			time.Sleep(time.Hour)
			if collected, prob := n.SN.CollectGarbage(context.Background(), n.BS, time.Now().UTC()); prob != nil {
				log.Printf("mlpd: gc: %v", prob)
			} else if len(collected) > 0 {
				log.Printf("mlpd: gc collected %d ephemeral objects", len(collected))
			}
		}
	}()
	log.Printf("mlpd: %s listening on %s (data %s)", domain, listen, dataDir)
	return http.ListenAndServe(listen, n.mux)
}

// config is one node's composition input; node is the composed
// process minus the listener — the two-domain demo test builds two
// of these on real sockets.
type config struct {
	Domain, SelfBase, DataDir, ClientDir string
	InitUser, Password, Origin           string
	Peers                                map[string]string
	// Clock, when set, drives every component's notion of now —
	// the scenario harness manipulates it for expiry and window
	// walks. nil = real time (production).
	Clock *time.Time
}

type node struct {
	DB     *sql.DB
	SN     *sn.SN
	BS     *bs.BS
	API    *clientapi.Server
	mux    *http.ServeMux
	pusher *bs.Pusher
}

func (n *node) Close() error { return n.DB.Close() }

func buildNode(cfg config) (*node, error) {
	domain, dataDir, clientDir := cfg.Domain, cfg.DataDir, cfg.ClientDir
	initUser, password, origin := cfg.InitUser, cfg.Password, cfg.Origin
	peers, selfBase := cfg.Peers, cfg.SelfBase
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	db, err := store.Open("sqlite3", "file:"+filepath.Join(dataDir, "mlp.db")+"?_fk=1")
	if err != nil {
		return nil, err
	}

	if err := bootstrap(db, initUser, password); err != nil {
		return nil, err
	}
	doc, err := domainDocument(db, domain)
	if err != nil {
		return nil, err
	}

	if origin == "" {
		origin = selfBase
	}

	resolver := &discovery.Resolver{DB: db, Fetcher: discovery.NewFetcher(), Supported: []string{"0.1"}}
	endpoint := func(path string) func(ctx context.Context, d string) (string, error) {
		return func(_ context.Context, d string) (string, error) {
			if base, ok := peers[d]; ok {
				return base + path, nil
			}
			return "", fmt.Errorf("mlpd: no route to %s (demo peers only)", d)
		}
	}
	pusherClient := (*http.Client)(nil)
	if len(peers) > 0 {
		log.Printf("mlpd: DEMO MODE — peer overrides %v; §5.4 hardening relaxed for these domains", peers)
		resolver.Fetcher = discovery.NewDemoFetcher(peers)
		pusherClient = http.DefaultClient // plain-HTTP pushes, demo only
		sn.AllowInsecureTransport = true  // §7.5 relaxed, demo only
	}

	var clock func() time.Time
	if cfg.Clock != nil {
		clock = func() time.Time { return *cfg.Clock }
	}
	node0 := &sn.SN{
		DB:               db,
		Resolver:         resolver,
		Domain:           domain,
		IngestBase:       selfBase + "/ingest/",
		DispatchEndpoint: endpoint("/dispatch"),
		FulfillEndpoint:  endpoint("/fulfill"),
		AutoGrant:        sn.D139AutoGrant, // the product ships D-139 on
		Now:              clock,
	}
	blob := &bs.BS{DB: db, Root: filepath.Join(dataDir, "objects"),
		PublicBase: selfBase, Resolver: resolver, Now: clock}
	indexer := &search.Indexer{DB: db, Now: clock,
		Open: func(urn string) (io.ReadCloser, error) { return os.Open(blob.ObjectPath(urn)) }}
	blob.OnVerified = func(urn string) {
		db.Exec(`UPDATE refs SET state='available', updated_at=? WHERE urn=? AND state='expected'`,
			time.Now().UTC().Format(time.RFC3339), urn)
		// S4.19 (D-261): extract text the moment custody verifies —
		// synchronous in the prototype (extraction is capped and the
		// index is self-healing; a production node would background it).
		if err := indexer.IndexObject(context.Background(), urn); err != nil {
			log.Printf("mlpd: search index %s: %v", urn, err)
		}
	}
	hub := clientapi.NewHub(db)
	api := &clientapi.Server{
		DB: db, SN: node0, BS: blob, Hub: hub, WebAuthnOrigin: origin,
		Search: indexer,
		Now:    clock,
		PostVerdict: func(ctx context.Context, target string, doc []byte) error {
			url, err := endpoint("/verdict")(ctx, target)
			if err != nil {
				return err
			}
			resp, err := http.Post(url, "application/mlp-verdict+json", strings.NewReader(string(doc)))
			if err != nil {
				return err
			}
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			if resp.StatusCode/100 != 2 {
				return fmt.Errorf("verdict refused by %s: %d %s", target, resp.StatusCode, body)
			}
			return nil
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/medialet.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "max-age=3600")
		w.Write(doc)
	})
	mux.Handle("/dispatch", sn.Handler(node0))
	mux.Handle("/fulfill", sn.Handler(node0))
	mux.Handle("/verdict", sn.Handler(node0))
	// No StripPrefix: the BS takes its token from MLP-Reservation,
	// and the RFC 9421 base must see the URI the pusher signed —
	// StripPrefix mutates r.URL.Path, which r.URL.RequestURI()
	// reconstructs from, silently breaking @target-uri.
	mux.Handle("/ingest/", bs.Handler(blob))
	mux.Handle("/api/v1/", api.Handler())
	if clientDir != "" {
		guest, err := os.ReadFile(filepath.Join(clientDir, "guest.html"))
		if err != nil {
			return nil, fmt.Errorf("mlpd: -client dir lacks guest.html: %w", err)
		}
		mux.HandleFunc("GET /g/{token}", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(guest)
		})
		mux.Handle("/", http.FileServer(http.Dir(clientDir)))
	}

	return &node{DB: db, SN: node0, BS: blob, API: api, mux: mux,
		pusher: &bs.Pusher{DB: db, Client: pusherClient, Now: clock,
			// §8.9 (M054): bao only toward domains advertising it.
			Caps: func(ctx context.Context, domain string) []string {
				doc, err := resolver.Resolve(ctx, domain)
				if err != nil || doc == nil {
					return nil
				}
				return doc.Capabilities
			}}}, nil
}

// pushOnce drives every pending or interrupted outbound reservation
// one pass through the resumable Pusher: kill mid-flight and the
// next pass resumes from the receiver's confirmed offset — zero
// redundant bytes (§8; the definition-of-done bullet).
func (n *node) pushOnce(ctx context.Context) {
	rows, err := n.DB.Query(`SELECT id, urn FROM reservations_out WHERE state IN ('pending','pushing')`)
	if err != nil {
		return
	}
	type job struct {
		id  int64
		urn string
	}
	var jobs []job
	for rows.Next() {
		var j job
		if rows.Scan(&j.id, &j.urn) == nil {
			jobs = append(jobs, j)
		}
	}
	rows.Close()
	for _, j := range jobs {
		f, err := os.Open(n.BS.ObjectPath(j.urn))
		if err != nil {
			continue
		}
		if err := n.pusher.Push(ctx, j.id, f); err != nil {
			log.Printf("mlpd: push %d (%s): %v", j.id, j.urn, err)
		}
		f.Close()
	}
}

// bootstrap provisions the store row, the domain's own key, and
// (optionally) a first mailbox — idempotently.
func bootstrap(db *sql.DB, initUser, password string) error {
	if _, err := db.Exec(`INSERT OR IGNORE INTO stores (id, name) VALUES (1, 'default')`); err != nil {
		return err
	}
	var keys int
	if err := db.QueryRow(`SELECT COUNT(*) FROM own_keys`).Scan(&keys); err != nil {
		return err
	}
	if keys == 0 {
		seed := make([]byte, ed25519.SeedSize)
		if _, err := rand.Read(seed); err != nil {
			return err
		}
		pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
		if _, err := db.Exec(`INSERT INTO own_keys (kid, seed, roles) VALUES (?,?,?)`,
			core.KID(pub), seed, `["sn","bs","author"]`); err != nil {
			return err
		}
		log.Printf("mlpd: generated domain key %s (roles sn,bs,author)", core.KID(pub))
	}
	if initUser != "" {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM mailboxes WHERE local_part=?`, initUser).Scan(&n)
		if n == 0 {
			res, err := db.Exec(`INSERT INTO mailboxes (local_part, created) VALUES (?,?)`,
				initUser, time.Now().UTC().Format(time.RFC3339))
			if err != nil {
				return err
			}
			id, _ := res.LastInsertId()
			if password != "" {
				hash, err := clientapi.HashPassword(password, 0)
				if err != nil {
					return err
				}
				if _, err := db.Exec(`INSERT INTO password_fallback (mailbox_id, hash) VALUES (?,?)`, id, hash); err != nil {
					return err
				}
			}
			log.Printf("mlpd: provisioned mailbox %s (id %d)", initUser, id)
		}
	}
	return nil
}

// domainDocument builds the §6 document from own_keys: unsigned by
// design — key entries self-verify through kid.
func domainDocument(db *sql.DB, domain string) ([]byte, error) {
	rows, err := db.Query(`SELECT seed, roles FROM own_keys`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []map[string]any
	for rows.Next() {
		var seed []byte
		var roles string
		if err := rows.Scan(&seed, &roles); err != nil {
			return nil, err
		}
		var rr []string
		json.Unmarshal([]byte(roles), &rr)
		pub := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
		keys = append(keys, map[string]any{
			"kid": core.KID(pub), "alg": "ed25519",
			"key": core.KeyField(pub), "roles": rr,
		})
	}
	return json.Marshal(map[string]any{
		"capabilities": []string{"bao-stream/1"}, // §5.2, MEP-003: the reference receives mlp-bao
		"domain":       domain, "mlp": []string{"0.1"},
		"sn": "https://" + domain + "/sn", "keys": keys,
	})
}
