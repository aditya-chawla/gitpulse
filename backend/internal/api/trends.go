package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/harshit3011/gitpulse/backend/internal/auth"
)

func (h *handlers) trendsOverview(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	repos, err := h.stats.TrendsOverview(r.Context(), u.ID)
	if err != nil {
		h.respond(w, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": repos})
}

func (h *handlers) repoTrend(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "repoId"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid repo id"})
		return
	}
	v, err := h.stats.TrendForRepo(r.Context(), auth.UserFrom(r.Context()).ID, id)
	h.respond(w, v, err)
}

func (h *handlers) listActions(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	source := r.URL.Query().Get("source")
	if source == "" {
		source = "owned"
	}
	items, err := h.stats.ActionItemsForSource(r.Context(), u.ID, limit, source)
	if err != nil {
		h.respond(w, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (h *handlers) dismissAction(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid action id"})
		return
	}
	if err := h.stats.DismissActionItem(r.Context(), auth.UserFrom(r.Context()).ID, id); err != nil {
		h.respond(w, nil, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
