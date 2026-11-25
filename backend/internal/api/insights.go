package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/harshit3011/gitpulse/backend/internal/auth"
)

func (h *handlers) listInsights(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	items, err := h.stats.Insights(r.Context(), u.ID, r.URL.Query().Get("source"))
	if err != nil {
		h.respond(w, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": items})
}

func (h *handlers) repoInsights(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "repoId"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid repo id"})
		return
	}
	v, err := h.stats.RepoInsights(r.Context(), auth.UserFrom(r.Context()).ID, id)
	h.respond(w, v, err)
}
