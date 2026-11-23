package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/harshit3011/gitpulse/backend/internal/auth"
	"github.com/harshit3011/gitpulse/backend/internal/github"
	"github.com/harshit3011/gitpulse/backend/internal/store"
)

// appStatus tells the UI whether the GitHub App is installed and where to
// install or configure it.
func (h *handlers) appStatus(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	out := map[string]any{"installed": false}
	if info, err := h.install.App().Info(r.Context()); err == nil {
		out["appName"] = info.Name
		out["installUrl"] = info.InstallURL()
	} else {
		h.log.Warn("app info", "err", err)
	}
	in, err := h.install.Lookup(r.Context(), u.ID)
	switch {
	case errors.Is(err, github.ErrNotInstalled):
	case err != nil:
		h.internal(w, "installation lookup", err)
		return
	default:
		out["installed"] = true
		out["installationId"] = in.ID
		out["repositorySelection"] = in.RepositorySelection
		out["configureUrl"] = in.HTMLURL
		out["suspended"] = in.SuspendedAt != nil
		// Read permissions are required for scans; writes are optional features.
		missing := in.MissingFor(github.ReadOnly)
		if missing == nil {
			missing = []string{}
		}
		out["missingPermissions"] = missing
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *handlers) listRepoSettings(w http.ResponseWriter, r *http.Request) {
	list, err := h.store.ListRepoSettings(r.Context(), auth.UserFrom(r.Context()).ID)
	if err != nil {
		h.internal(w, "list repo settings", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"repos": list})
}

func (h *handlers) putRepoSetting(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad repo id"})
		return
	}
	var in struct {
		PRMode          string `json:"prMode"`
		ReleasesEnabled bool   `json:"releasesEnabled"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	u := auth.UserFrom(r.Context())
	out, err := h.store.SaveRepoSetting(r.Context(), u.ID, store.RepoSetting{
		RepoID: id, PRMode: in.PRMode, ReleasesEnabled: in.ReleasesEnabled,
	})
	switch {
	case errors.Is(err, store.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	case err != nil:
		h.internal(w, "save repo setting", err)
	default:
		h.log.Info("repo setting changed", "user", u.Login, "repo", out.FullName,
			"pr_mode", out.PRMode, "releases_enabled", out.ReleasesEnabled)
		writeJSON(w, http.StatusOK, out)
	}
}

// llmUsage reports AI status and this month's spend for the Settings page.
func (h *handlers) llmUsage(w http.ResponseWriter, r *http.Request) {
	u, err := h.llm.Usage(r.Context(), auth.UserFrom(r.Context()).ID)
	if err != nil {
		h.internal(w, "llm usage", err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}
