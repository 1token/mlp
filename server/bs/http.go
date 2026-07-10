package bs

import (
	"encoding/json"
	"net/http"
)

const ctOffset = "application/offset+octet-stream"

// Handler serves the upload resources under PublicBase. Every
// response carries Tus-Resumable (§8.2 rule 1); successful ones carry
// Upload-Expires (rule 4). Responses are not signed (D-79).
func Handler(b *BS) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Tus-Resumable", "1.0.0")
		if r.Header.Get("Tus-Resumable") != "1.0.0" {
			writeProblem(w, problemf(http.StatusPreconditionFailed, "reservation-invalid",
				"Tus-Resumable: 1.0.0 is required (§8.2 rule 1)"))
			return
		}
		token := r.Header.Get("MLP-Reservation")
		targetURI := b.PublicBase + r.URL.RequestURI()
		header := func(name string) string { return r.Header.Get(name) }

		switch r.Method {
		case http.MethodHead:
			offset, length, expires, prob := b.Head(r.Context(), token, targetURI, header)
			if prob != nil {
				writeProblem(w, prob)
				return
			}
			w.Header().Set("Upload-Offset", itoa(offset))
			w.Header().Set("Upload-Length", itoa(length))
			w.Header().Set("Upload-Expires", expires.UTC().Format(http.TimeFormat))
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusOK)

		case http.MethodPatch:
			if r.Header.Get("Content-Type") != ctOffset {
				w.WriteHeader(http.StatusUnsupportedMediaType) // tus semantics (§8.5)
				return
			}
			offset, verified, prob := b.Patch(r.Context(), token, targetURI, header, r.Body)
			if prob != nil {
				writeProblem(w, prob)
				return
			}
			w.Header().Set("Upload-Offset", itoa(offset))
			if verified {
				w.Header().Set("MLP-Object-State", "verified")
			}
			w.WriteHeader(http.StatusNoContent)

		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func writeProblem(w http.ResponseWriter, p *Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	json.NewEncoder(w).Encode(map[string]any{
		"type":   "urn:mlp:err:" + p.Code,
		"title":  p.Code,
		"status": p.Status,
		"detail": p.Detail,
	})
}
