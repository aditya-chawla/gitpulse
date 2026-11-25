package api

import (
	"encoding/json"
	"net/http"

	"github.com/harshit3011/gitpulse/backend/internal/advisor"
	"github.com/harshit3011/gitpulse/backend/internal/auth"
)

func (h *handlers) askAgent(w http.ResponseWriter, r *http.Request) {
	if h.advisor == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "advisor unavailable"})
		return
	}
	var req advisor.AskRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	res, err := h.advisor.Ask(r.Context(), auth.UserFrom(r.Context()).ID, req)
	h.respond(w, res, err)
}
