package clientapi

// S4.12 acceptance. The guest journey end to end (D-151–D-155):
// compose with named guests → capability links with second-channel
// PINs and the D-153 notifier → the PIN gate with the D-155 lock →
// the payload serving the render form with NO view recorded (D-147)
// → downloads recorded → the claim minting a mailbox, re-dispatching
// the original Signed Medialet through the real ingest, issuing a
// session → instant-have on accept (the bytes never move) → the
// link surviving its claim → expiry. Then the passkey ceremonies
// (D-161): register under the session, assert to log in, refusals.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"medialet.org/mlp/webauthn"
)

func guestDraft(withPIN bool, guests ...string) string {
	d := map[string]any{
		"subject":      "Photos from the shoot",
		"body_content": `<p>Here they are! File: <a href="` + mediaURN + `">sample.txt</a></p>`,
		"recipients":   []string{},
		"guests":       guests,
		"guest_pin":    withPIN,
		"manifest": []map[string]any{{
			"urn": mediaURN, "size": 36, "type": "text/plain",
			"name": "sample.txt", "available_until": "2026-08-04T10:00:00Z",
		}},
	}
	b, _ := json.Marshal(d)
	return string(b)
}

func sendGuestDraft(t *testing.T, do func(method, path, body string, hdr map[string]string) *http.Response, draft string) (guests []map[string]any) {
	t.Helper()
	resp := do(http.MethodPost, "/api/v1/drafts", draft, nil)
	var created struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&created)
	resp.Body.Close()
	resp = do(http.MethodPost, "/api/v1/drafts/"+created.ID+"/send", "{}",
		map[string]string{"Idempotency-Key": "guest-send-" + created.ID})
	if resp.StatusCode != 200 {
		t.Fatalf("send: %d", resp.StatusCode)
	}
	var result struct {
		Guests []map[string]any `json:"guests"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()
	return result.Guests
}

func TestGuestJourneyEndToEnd(t *testing.T) {
	clock := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	s := originAPI(t, &clock)
	// The fixture holds the objects ROW; the download serves BYTES.
	objPath := s.BS.ObjectPath(mediaURN)
	if err := os.MkdirAll(filepath.Dir(objPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(objPath, []byte("MLP test vector 001: media object A\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	notified := map[string]string{}
	s.SN.GuestNotifier = func(_ context.Context, recipient, path string) error {
		notified[recipient] = path
		return nil
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	do := login(t, ts, "petra@origin.example")

	guests := sendGuestDraft(t, do, guestDraft(true, "friend@example.net", "locky@example.net"))
	if len(guests) != 2 {
		t.Fatalf("guest outcomes: %+v", guests)
	}
	if len(notified) != 2 || !strings.HasPrefix(notified["friend@example.net"], "/g/") {
		t.Fatalf("the D-153 notifier must fire per guest: %+v", notified)
	}
	byRecipient := map[string]map[string]any{}
	for _, g := range guests {
		byRecipient[g["recipient"].(string)] = g
	}
	friend := byRecipient["friend@example.net"]
	locky := byRecipient["locky@example.net"]
	pin, _ := friend["pin"].(string)
	if len(pin) != 6 {
		t.Fatalf("PIN must be 6 digits: %q", pin)
	}
	token := strings.TrimPrefix(friend["path"].(string), "/g/")
	lockToken := strings.TrimPrefix(locky["path"].(string), "/g/")

	guestGet := func(tok, pin, path string) *http.Response {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/guest/"+tok+path, nil)
		if pin != "" {
			req.Header.Set("X-MLP-Guest-PIN", pin)
		}
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	// No PIN → pin-required; the D-155 lock after 5 wrong attempts.
	resp := guestGet(token, "", "")
	if resp.StatusCode != 401 {
		t.Fatalf("missing PIN: %d", resp.StatusCode)
	}
	resp.Body.Close()
	for i := 0; i < 5; i++ {
		resp = guestGet(lockToken, "000000", "")
		if resp.StatusCode != 401 {
			t.Fatalf("wrong PIN attempt %d: %d", i, resp.StatusCode)
		}
		resp.Body.Close()
	}
	resp = guestGet(lockToken, "000000", "")
	if resp.StatusCode != 423 {
		t.Fatalf("6th attempt must be locked (D-155): %d", resp.StatusCode)
	}
	resp.Body.Close()

	// The payload: render form served; the view NEVER recorded.
	resp = guestGet(token, pin, "")
	if resp.StatusCode != 200 {
		t.Fatalf("payload: %d", resp.StatusCode)
	}
	var payload struct {
		Subject string         `json:"subject"`
		Author  string         `json:"author"`
		Body    map[string]any `json:"body"`
		Files   []map[string]any
	}
	json.NewDecoder(resp.Body).Decode(&payload)
	resp.Body.Close()
	if payload.Author != "petra@origin.example" || len(payload.Files) != 1 {
		t.Fatalf("payload: %+v", payload)
	}
	if c, _ := payload.Body["content"].(string); !strings.Contains(c, "Here they are!") {
		t.Fatalf("render form missing: %q", c)
	}
	var viewEvents int
	s.DB.QueryRow(`SELECT COUNT(*) FROM timeline_events WHERE kind LIKE 'guest.view%'`).Scan(&viewEvents)
	if viewEvents != 0 {
		t.Fatal("opens are never recorded (D-147)")
	}

	// The download: bytes + the recorded fact.
	resp = guestGet(token, pin, "/o/"+mediaURN)
	body := readAll(t, resp)
	if resp.StatusCode != 200 || body != "MLP test vector 001: media object A\n" {
		t.Fatalf("download: %d %q", resp.StatusCode, body)
	}
	var downloads int
	s.DB.QueryRow(`SELECT COUNT(*) FROM guest_downloads`).Scan(&downloads)
	var dlEvents int
	s.DB.QueryRow(`SELECT COUNT(*) FROM timeline_events WHERE kind='guest.download'`).Scan(&dlEvents)
	if downloads != 1 || dlEvents != 1 {
		t.Fatalf("downloads shown (D-147): rows=%d events=%d", downloads, dlEvents)
	}

	// The claim (D-154): mailbox minted, the delivery re-dispatched
	// through the real ingest, a session issued.
	claimReq, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/guest/"+token+"/claim",
		strings.NewReader(`{"local_part":"friend"}`))
	claimReq.Header.Set("X-MLP-Guest-PIN", pin)
	claimResp, err := ts.Client().Do(claimReq)
	if err != nil || claimResp.StatusCode != 200 {
		t.Fatalf("claim: %v %d %s", err, claimResp.StatusCode, readAll(t, claimResp))
	}
	var claim struct {
		Address   string `json:"address"`
		MailboxID int64  `json:"mailbox_id"`
	}
	json.NewDecoder(claimResp.Body).Decode(&claim)
	var claimCookie *http.Cookie
	for _, c := range claimResp.Cookies() {
		if c.Name == sessionCookie {
			claimCookie = c
		}
	}
	claimResp.Body.Close()
	if claim.Address != "friend@origin.example" || claimCookie == nil {
		t.Fatalf("claim result: %+v cookie=%v", claim, claimCookie)
	}
	doClaimed := func(method, path, body string) *http.Response {
		req, _ := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
		req.AddCookie(claimCookie)
		req.Header.Set("X-MLP-Client", "test")
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	resp = doClaimed(http.MethodGet, "/api/v1/threads?view=inbox", "")
	var threads struct {
		Threads []map[string]any `json:"threads"`
	}
	json.NewDecoder(resp.Body).Decode(&threads)
	resp.Body.Close()
	if len(threads.Threads) != 1 {
		t.Fatalf("the re-dispatched delivery must be in the new inbox: %+v", threads.Threads)
	}

	// Instant-have (D-154): the bytes are already here.
	resp = doClaimed(http.MethodPost, "/api/v1/o/"+mediaURN+"/accept", "{}")
	var accept struct {
		State   string `json:"state"`
		Instant bool   `json:"instant"`
	}
	json.NewDecoder(resp.Body).Decode(&accept)
	resp.Body.Close()
	if resp.StatusCode != 200 || accept.State != "available" || !accept.Instant {
		t.Fatalf("instant-have: %d %+v", resp.StatusCode, accept)
	}
	var refState string
	s.DB.QueryRow(`SELECT state FROM refs WHERE mailbox_id=? AND urn=?`, claim.MailboxID, mediaURN).Scan(&refState)
	if refState != "available" {
		t.Fatalf("ref after instant accept: %q", refState)
	}

	// The link survives its claim (D-154); a second claim refuses.
	resp = guestGet(token, pin, "")
	var after struct {
		ClaimedAs string `json:"claimed_as"`
	}
	json.NewDecoder(resp.Body).Decode(&after)
	resp.Body.Close()
	if resp.StatusCode != 200 || after.ClaimedAs != "friend@origin.example" {
		t.Fatalf("the link must survive the claim: %d %+v", resp.StatusCode, after)
	}
	claimReq2, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/guest/"+token+"/claim",
		strings.NewReader(`{"local_part":"friend2"}`))
	claimReq2.Header.Set("X-MLP-Guest-PIN", pin)
	claimResp2, _ := ts.Client().Do(claimReq2)
	if claimResp2.StatusCode != 409 {
		t.Fatalf("one claim per link (D-155): %d", claimResp2.StatusCode)
	}
	claimResp2.Body.Close()

	// Expiry (D-152).
	clock = clock.Add(31 * 24 * time.Hour)
	resp = guestGet(token, pin, "")
	if resp.StatusCode != 410 {
		t.Fatalf("expired link must answer 410: %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestWebAuthnCeremonies(t *testing.T) {
	clock := time.Date(2026, 7, 4, 12, 0, 0, 0, time.UTC)
	tv1 := loadJSON(t, tv001Path)
	s := newAPI(t, "target.example", "novak", seedT3, &clock,
		map[string]json.RawMessage{"origin.example": tv1["domain_document"]})
	s.WebAuthnOrigin = "https://target.example"
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	do := login(t, ts, "novak@target.example")

	// Registration under the session.
	resp := do(http.MethodPost, "/api/v1/webauthn/register/begin", "{}", nil)
	var begin struct {
		Challenge string `json:"challenge"`
	}
	json.NewDecoder(resp.Body).Decode(&begin)
	resp.Body.Close()

	priv, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	cose := webauthn.EncodeCBOR(map[any]any{
		int64(1): int64(2), int64(3): int64(webauthn.AlgES256), int64(-1): int64(1),
		int64(-2): priv.X.FillBytes(make([]byte, 32)),
		int64(-3): priv.Y.FillBytes(make([]byte, 32)),
	})
	credID := []byte("api-test-cred-01")
	rpHash := sha256.Sum256([]byte("target.example"))
	authData := append([]byte{}, rpHash[:]...)
	authData = append(authData, 0x45) // UP | UV | AT
	authData = binary.BigEndian.AppendUint32(authData, 1)
	authData = append(authData, make([]byte, 16)...)
	authData = binary.BigEndian.AppendUint16(authData, uint16(len(credID)))
	authData = append(authData, credID...)
	authData = append(authData, cose...)
	att := webauthn.EncodeCBOR(map[any]any{
		"fmt": "none", "attStmt": map[any]any{}, "authData": authData,
	})
	cd, _ := json.Marshal(map[string]string{
		"type": "webauthn.create", "challenge": begin.Challenge,
		"origin": "https://target.example",
	})
	finishBody, _ := json.Marshal(map[string]string{
		"challenge":          begin.Challenge,
		"client_data_json":   base64.RawURLEncoding.EncodeToString(cd),
		"attestation_object": base64.RawURLEncoding.EncodeToString(att),
	})
	resp = do(http.MethodPost, "/api/v1/webauthn/register/finish", string(finishBody), nil)
	if resp.StatusCode != 200 {
		t.Fatalf("register finish: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Login: begin (public), assert, finish → a fresh session.
	loginBegin := func() (string, []string) {
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/webauthn/login/begin",
			strings.NewReader(`{"address":"novak@target.example"}`))
		req.Header.Set("X-MLP-Client", "test")
		resp, err := ts.Client().Do(req)
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("login begin: %v %d", err, resp.StatusCode)
		}
		var lb struct {
			Challenge        string `json:"challenge"`
			AllowCredentials []struct {
				ID string `json:"id"`
			} `json:"allowCredentials"`
		}
		json.NewDecoder(resp.Body).Decode(&lb)
		resp.Body.Close()
		ids := make([]string, len(lb.AllowCredentials))
		for i, a := range lb.AllowCredentials {
			ids[i] = a.ID
		}
		return lb.Challenge, ids
	}
	challenge, allow := loginBegin()
	wantCred := base64.RawURLEncoding.EncodeToString(credID)
	if len(allow) != 1 || allow[0] != wantCred {
		t.Fatalf("allow list: %v", allow)
	}
	assertAuthData := append([]byte{}, rpHash[:]...)
	assertAuthData = append(assertAuthData, 0x01) // UP
	assertAuthData = binary.BigEndian.AppendUint32(assertAuthData, 2)
	loginCD, _ := json.Marshal(map[string]string{
		"type": "webauthn.get", "challenge": challenge,
		"origin": "https://target.example",
	})
	cdHash := sha256.Sum256(loginCD)
	digest := sha256.Sum256(append(append([]byte{}, assertAuthData...), cdHash[:]...))
	sig, _ := ecdsa.SignASN1(rand.Reader, priv, digest[:])
	loginFinish := func(challenge string, sig []byte) *http.Response {
		body, _ := json.Marshal(map[string]string{
			"challenge":          challenge,
			"credential_id":      wantCred,
			"client_data_json":   base64.RawURLEncoding.EncodeToString(loginCD),
			"authenticator_data": base64.RawURLEncoding.EncodeToString(assertAuthData),
			"signature":          base64.RawURLEncoding.EncodeToString(sig),
		})
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/webauthn/login/finish",
			strings.NewReader(string(body)))
		req.Header.Set("X-MLP-Client", "test")
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}
	resp = loginFinish(challenge, sig)
	if resp.StatusCode != 200 {
		t.Fatalf("login finish: %d", resp.StatusCode)
	}
	var passkeyCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie {
			passkeyCookie = c
		}
	}
	resp.Body.Close()
	if passkeyCookie == nil {
		t.Fatal("passkey login must issue a session")
	}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/threads?view=inbox", nil)
	req.AddCookie(passkeyCookie)
	resp, _ = ts.Client().Do(req)
	if resp.StatusCode != 200 {
		t.Fatalf("passkey session must work: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Challenge reuse refuses (single-use by construction).
	resp = loginFinish(challenge, sig)
	if resp.StatusCode != 401 {
		t.Fatalf("challenge reuse must refuse: %d", resp.StatusCode)
	}
	resp.Body.Close()

	// A tampered signature refuses.
	challenge2, _ := loginBegin()
	loginCD, _ = json.Marshal(map[string]string{
		"type": "webauthn.get", "challenge": challenge2,
		"origin": "https://target.example",
	})
	cdHash = sha256.Sum256(loginCD)
	digest = sha256.Sum256(append(append([]byte{}, assertAuthData...), cdHash[:]...))
	sig, _ = ecdsa.SignASN1(rand.Reader, priv, digest[:])
	sig[10] ^= 0x20
	resp = loginFinish(challenge2, sig)
	if resp.StatusCode != 401 {
		t.Fatalf("tampered assertion must refuse: %d", resp.StatusCode)
	}
	resp.Body.Close()
}
