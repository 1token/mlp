package sn

import (
	"encoding/json"
	"io"
	"net/http"
)

// Media types of §7.2.
const (
	ctDelegation = "application/mlp-delegation+json"
	ctEnvelope   = "application/mlp-envelope+json"
	ctVerdict    = "application/mlp-verdict+json"
	ctProblem    = "application/problem+json"
)

// Handler exposes the SN server-to-server API (§7.2) rooted at the
// Domain Document's sn URL: POST /dispatch and POST /verdict.
// (/resolve is optional per §5.6 and intentionally not served: the
// endpoint is a courtesy an operator enables deliberately.)
func Handler(s *SN) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /dispatch", func(w http.ResponseWriter, r *http.Request) {
		body, prob := readBody(w, r, ctEnvelope)
		if prob != nil {
			writeProblem(w, prob)
			return
		}
		verdict, prob := s.ProcessDispatch(r.Context(), body)
		if prob != nil {
			writeProblem(w, prob)
			return
		}
		w.Header().Set("Content-Type", ctVerdict)
		w.WriteHeader(http.StatusOK)
		w.Write(verdict)
	})
	mux.HandleFunc("POST /fulfill", func(w http.ResponseWriter, r *http.Request) {
		body, prob := readBody(w, r, ctDelegation)
		if prob != nil {
			writeProblem(w, prob)
			return
		}
		resp, prob := s.ProcessFulfill(r.Context(), body)
		if prob != nil {
			writeProblem(w, prob)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(resp)
	})
	mux.HandleFunc("POST /verdict", func(w http.ResponseWriter, r *http.Request) {
		body, prob := readBody(w, r, ctVerdict)
		if prob != nil {
			writeProblem(w, prob)
			return
		}
		if prob := s.ProcessVerdictUpdate(r.Context(), body); prob != nil {
			writeProblem(w, prob)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

func readBody(w http.ResponseWriter, r *http.Request, wantCT string) ([]byte, *Problem) {
	if ct := r.Header.Get("Content-Type"); ct != wantCT {
		return nil, problemf(http.StatusUnsupportedMediaType, "malformed",
			"content type %q where %s is required (§7.2)", ct, wantCT)
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxEnvelopeBytes+1))
	if err != nil {
		return nil, problemf(http.StatusRequestEntityTooLarge, "envelope-too-large", "%v", err)
	}
	return body, nil
}

// writeProblem emits the unsigned RFC 9457 problem response (§7.2):
// type is the reason-code URI urn:mlp:err:<code> (§7.8).
func writeProblem(w http.ResponseWriter, p *Problem) {
	w.Header().Set("Content-Type", ctProblem)
	w.WriteHeader(p.Status)
	json.NewEncoder(w).Encode(map[string]any{
		"type":   "urn:mlp:err:" + p.Code,
		"title":  p.Code,
		"status": p.Status,
		"detail": p.Detail,
	})
}
