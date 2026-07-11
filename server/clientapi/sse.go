package clientapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Hub is the live feed (D-132/D-170). The events table is the source
// of truth — every emission is journaled first, which is what makes
// Last-Event-ID resume exact — and connected clients get an
// in-process fanout of the same rows.
type Hub struct {
	DB  *sql.DB
	Now func() time.Time
	// Heartbeat is the keep-alive comment interval; tests shorten it.
	Heartbeat time.Duration

	mu   sync.Mutex
	subs map[int64]map[chan sseEvent]struct{}
}

type sseEvent struct {
	ID   int64
	Type string
	Data string
}

func NewHub(db *sql.DB) *Hub {
	return &Hub{DB: db, Heartbeat: 15 * time.Second, subs: map[int64]map[chan sseEvent]struct{}{}}
}

func (h *Hub) now() time.Time {
	if h.Now != nil {
		return h.Now().UTC()
	}
	return time.Now().UTC()
}

// Emit journals one typed event for a mailbox and fans it out to
// connected subscribers.
func (h *Hub) Emit(ctx context.Context, mailboxID int64, typ string, data map[string]any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	res, err := h.DB.ExecContext(ctx,
		`INSERT INTO events (mailbox_id, type, data_json, at) VALUES (?,?,?,?)`,
		mailboxID, typ, string(b), h.now().Format(time.RFC3339))
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	ev := sseEvent{ID: id, Type: typ, Data: string(b)}
	h.mu.Lock()
	for ch := range h.subs[mailboxID] {
		select {
		case ch <- ev:
		default: // a slow consumer resumes from the journal
		}
	}
	h.mu.Unlock()
	return nil
}

func (h *Hub) subscribe(mailboxID int64) (chan sseEvent, func()) {
	ch := make(chan sseEvent, 32)
	h.mu.Lock()
	if h.subs[mailboxID] == nil {
		h.subs[mailboxID] = map[chan sseEvent]struct{}{}
	}
	h.subs[mailboxID][ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs[mailboxID], ch)
		h.mu.Unlock()
	}
}

// handleEvents is GET /events: replay past Last-Event-ID from the
// journal, then live-tail.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	mailbox, _, prob := s.session(r)
	if prob != nil {
		writeProblem(w, prob)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeProblem(w, problemf(http.StatusInternalServerError, "malformed", "streaming unsupported"))
		return
	}
	var last int64
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		last, _ = strconv.ParseInt(v, 10, 64)
	} else if v := r.URL.Query().Get("last_event_id"); v != "" {
		last, _ = strconv.ParseInt(v, 10, 64)
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ch, cancel := s.Hub.subscribe(mailbox)
	defer cancel()

	// Journal replay: everything this mailbox missed.
	rows, err := s.DB.QueryContext(r.Context(),
		`SELECT id, type, data_json FROM events WHERE mailbox_id=? AND id>? ORDER BY id`, mailbox, last)
	if err != nil {
		return
	}
	for rows.Next() {
		var ev sseEvent
		if rows.Scan(&ev.ID, &ev.Type, &ev.Data) != nil {
			break
		}
		writeSSE(w, ev)
		last = ev.ID
	}
	rows.Close()
	flusher.Flush()

	heartbeat := s.Hub.Heartbeat
	if heartbeat <= 0 {
		heartbeat = 15 * time.Second
	}
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev := <-ch:
			if ev.ID <= last {
				continue // already replayed from the journal
			}
			writeSSE(w, ev)
			last = ev.ID
			flusher.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		}
	}
}

func writeSSE(w http.ResponseWriter, ev sseEvent) {
	fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.ID, ev.Type, ev.Data)
}
