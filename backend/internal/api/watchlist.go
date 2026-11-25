package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/harshit3011/gitpulse/backend/internal/auth"
	"github.com/harshit3011/gitpulse/backend/internal/watchlist"
)

func (h *handlers) listWatchlist(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	items, err := h.watchlist.List(r.Context(), u.ID)
	if err != nil {
		h.log.Error("list watchlist", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load watchlist"})
		return
	}
	if items == nil {
		items = []watchlist.WatchedRepo{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": items})
}

func (h *handlers) addWatchlist(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	var in struct {
		Repo string `json:"repo"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	item, err := h.watchlist.Add(r.Context(), u.ID, in.Repo)
	if err != nil {
		switch {
		case errors.Is(err, watchlist.ErrInvalid), errors.Is(err, watchlist.ErrLimitReached):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		case errors.Is(err, watchlist.ErrPrivateRepo):
			writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		case errors.Is(err, watchlist.ErrNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		case errors.Is(err, watchlist.ErrAlreadyWatched):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		default:
			h.log.Error("add watchlist", "err", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to add repository"})
		}
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *handlers) removeWatchlist(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	if err := h.watchlist.Remove(r.Context(), u.ID, id); err != nil {
		if errors.Is(err, watchlist.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		h.log.Error("remove watchlist", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to remove repository"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
