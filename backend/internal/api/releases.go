package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/harshit3011/gitpulse/backend/internal/auth"
	"github.com/harshit3011/gitpulse/backend/internal/github"
	"github.com/harshit3011/gitpulse/backend/internal/install"
	"github.com/harshit3011/gitpulse/backend/internal/releases"
	"github.com/harshit3011/gitpulse/backend/internal/store"
)

// writeFeatureError maps errors shared by the write features to HTTP.
func (h *handlers) writeFeatureError(w http.ResponseWriter, what string, err error) {
	var mp *install.MissingPermissionsError
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	case errors.Is(err, releases.ErrNotAllowed):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
	case errors.Is(err, releases.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, github.ErrNotInstalled), errors.Is(err, install.ErrSuspended), errors.As(err, &mp):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
	default:
		h.internal(w, what, err)
	}
}

func repoIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad repo id"})
		return 0, false
	}
	return id, true
}

// POST /api/repos/{id}/release-notes/preview (POST: it may spend AI budget).
func (h *handlers) releasePreview(w http.ResponseWriter, r *http.Request) {
	id, ok := repoIDParam(w, r)
	if !ok {
		return
	}
	p, err := h.releases.Preview(r.Context(), auth.UserFrom(r.Context()), id)
	if err != nil {
		h.writeFeatureError(w, "release preview", err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// POST /api/repos/{id}/releases creates a DRAFT release.
func (h *handlers) createRelease(w http.ResponseWriter, r *http.Request) {
	id, ok := repoIDParam(w, r)
	if !ok {
		return
	}
	var in releases.CreateInput
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	rel, err := h.releases.Create(r.Context(), auth.UserFrom(r.Context()), id, in, "ui")
	if err != nil {
		h.writeFeatureError(w, "create release", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": rel.ID, "url": rel.HTMLURL, "draft": rel.Draft, "tag": rel.TagName})
}

func (h *handlers) releaseDrafts(w http.ResponseWriter, r *http.Request) {
	d, err := h.releases.Drafts(r.Context(), auth.UserFrom(r.Context()).ID)
	if err != nil {
		h.internal(w, "list drafts", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"drafts": d})
}
