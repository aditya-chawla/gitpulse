package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/harshit3011/gitpulse/backend/internal/auth"
	"github.com/harshit3011/gitpulse/backend/internal/stats"
)

// respond maps stats errors to HTTP. ErrNoData becomes 200 {"noData": true}
// so the UI can show an onboarding state instead of an error.
func (h *handlers) respond(w http.ResponseWriter, v any, err error) {
	var ve *stats.ValidationError
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, v)
	case errors.Is(err, stats.ErrNoData):
		writeJSON(w, http.StatusOK, map[string]bool{"noData": true})
	case errors.Is(err, stats.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	case errors.As(err, &ve):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": ve.Msg})
	default:
		h.internal(w, "stats", err)
	}
}

func (h *handlers) overview(w http.ResponseWriter, r *http.Request) {
	v, err := h.stats.Overview(r.Context(), auth.UserFrom(r.Context()).ID)
	h.respond(w, v, err)
}

func (h *handlers) analytics(w http.ResponseWriter, r *http.Request) {
	v, err := h.stats.Analytics(r.Context(), auth.UserFrom(r.Context()).ID)
	h.respond(w, v, err)
}

func (h *handlers) repos(w http.ResponseWriter, r *http.Request) {
	v, err := h.stats.Repos(r.Context(), auth.UserFrom(r.Context()).ID)
	h.respond(w, v, err)
}

func (h *handlers) repo(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad repo id"})
		return
	}
	v, err := h.stats.Repo(r.Context(), auth.UserFrom(r.Context()).ID, id)
	h.respond(w, v, err)
}

func (h *handlers) digest(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	v, err := h.stats.Digest(r.Context(), u.ID, u.Login)
	h.respond(w, v, err)
}

func (h *handlers) getSettings(w http.ResponseWriter, r *http.Request) {
	v, err := h.stats.Settings(r.Context(), auth.UserFrom(r.Context()).ID)
	h.respond(w, v, err)
}

func (h *handlers) putSettings(w http.ResponseWriter, r *http.Request) {
	var in stats.Settings
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	v, err := h.stats.SaveSettings(r.Context(), auth.UserFrom(r.Context()).ID, in)
	h.respond(w, v, err)
}
