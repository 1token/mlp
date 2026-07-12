package sn

// Guest delivery (S3.6, D-151–D-155): a recipient without an MLP
// mailbox anywhere is named explicitly by the sender and receives a
// capability link to a delivery page hosted by the sender's own
// domain. Guests never appear in envelope_to — guesthood is an
// addressing fact, not a delivery failure. The capability is the
// token; the database holds only its hash (D-155: a leak must not
// mint working links). The PIN, when the sender asks for one, is
// returned ONCE to the sender for conveyance through a second
// channel (D-152) and never rides in the notification (D-153).

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"medialet.org/mlp/core"
)

// GuestOutcome is the sender-facing result of one guest link: the
// path (the client composes the absolute URL) and, when a PIN was
// requested, its single disclosure.
type GuestOutcome struct {
	Recipient string `json:"recipient"`
	Path      string `json:"path"` // /g/{token}
	PIN       string `json:"pin,omitempty"`
	ExpiresAt string `json:"expires_at"`
}

// GuestLinkTTL is the D-152 default link lifetime.
const GuestLinkTTL = 30 * 24 * time.Hour

// guestPINDigits is the PIN length: 6 digits, second-channel secret.
const guestPINDigits = 6

// HashToken is the stored form of capability tokens and PINs.
func HashToken(token string) string {
	return core.URNMlet([]byte("mlp-guest-token:" + token))
}

func newGuestToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func newGuestPIN() (string, error) {
	pin := ""
	for i := 0; i < guestPINDigits; i++ {
		n, err := rand.Int(rand.Reader, big.NewInt(10))
		if err != nil {
			return "", err
		}
		pin += n.String()
	}
	return pin, nil
}

// createGuestLinks mints one capability link per guest recipient for
// a just-materialized delivery. Called from Send after the fan-out;
// a guest failure does not unsend anything (mirrors the per-target
// posture).
func (s *SN) createGuestLinks(ctx context.Context, deliveryID int64, guests []string, withPIN bool, now time.Time) ([]GuestOutcome, *Problem) {
	out := make([]GuestOutcome, 0, len(guests))
	nowS := now.Format(time.RFC3339)
	expires := now.Add(GuestLinkTTL).Format(time.RFC3339)
	for _, recipient := range guests {
		token, err := newGuestToken()
		if err != nil {
			return nil, problemf(http.StatusInternalServerError, "malformed", "entropy: %v", err)
		}
		var pin, pinHash any
		var pinClear string
		if withPIN {
			pinClear, err = newGuestPIN()
			if err != nil {
				return nil, problemf(http.StatusInternalServerError, "malformed", "entropy: %v", err)
			}
			pinHash = HashToken(pinClear)
			pin = pinClear
			_ = pin
		}
		if _, err := s.DB.ExecContext(ctx,
			`INSERT INTO guest_links (delivery_id, recipient_hint, token_hash, pin_hash, expires)
			 VALUES (?,?,?,?,?)`,
			deliveryID, recipient, HashToken(token), pinHash, expires); err != nil {
			return nil, problemf(http.StatusInternalServerError, "malformed", "store: %v", err)
		}
		path := "/g/" + token
		s.DB.ExecContext(ctx,
			`INSERT INTO timeline_events (delivery_id, at, kind, data_json) VALUES (?,?,?,?)`,
			deliveryID, nowS, "guest.link-created",
			fmt.Sprintf(`{"recipient":%q}`, recipient))
		// The D-153 notification: link only — no PIN, no tracking.
		// The hook is the prototype's mail room; absent a hook, the
		// sender conveys the link (both channels theirs).
		if s.GuestNotifier != nil {
			if err := s.GuestNotifier(ctx, recipient, path); err == nil {
				s.DB.ExecContext(ctx,
					`INSERT INTO timeline_events (delivery_id, at, kind, data_json) VALUES (?,?,?,?)`,
					deliveryID, nowS, "guest.notified",
					fmt.Sprintf(`{"recipient":%q}`, recipient))
			}
		}
		out = append(out, GuestOutcome{Recipient: recipient, Path: path, PIN: pinClear, ExpiresAt: expires})
	}
	return out, nil
}
