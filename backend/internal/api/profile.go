package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/harshit3011/gitpulse/backend/internal/auth"
	"github.com/harshit3011/gitpulse/backend/internal/profile"
)

func (h *handlers) profileError(w http.ResponseWriter, what string, err error) {
	switch {
	case errors.Is(err, profile.ErrInvalidConfig):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	case errors.Is(err, profile.ErrRepoNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
	case errors.Is(err, profile.ErrNoScan):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "run a scan first"})
	default:
		h.writeFeatureError(w, what, err)
	}
}

func (h *handlers) getProfileSettings(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	st, err := h.profile.Settings(r.Context(), u.ID)
	if err != nil {
		h.internal(w, "profile settings", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": st, "repo": h.profile.RepoName(u)})
}

func (h *handlers) putProfileSettings(w http.ResponseWriter, r *http.Request) {
	var in profile.Settings
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return
	}
	u := auth.UserFrom(r.Context())
	st, err := h.profile.SaveSettings(r.Context(), u.ID, in)
	if err != nil {
		h.profileError(w, "save profile settings", err)
		return
	}
	h.log.Info("profile README settings changed", "user", u.Login, "enabled", st.Enabled, "feed", st.FeedURL != "")
	writeJSON(w, http.StatusOK, map[string]any{"settings": st, "repo": h.profile.RepoName(u)})
}

func (h *handlers) profilePreview(w http.ResponseWriter, r *http.Request) {
	p, err := h.profile.Preview(r.Context(), auth.UserFrom(r.Context()))
	if err != nil {
		h.profileError(w, "profile preview", err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// POST /api/profile-readme/apply: X-Scan-Secret (weekly cron, every
// allowlisted user who turned it on) or a session.
func (h *handlers) profileApply(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	present, ok := h.checkSecret(r)
	if present && !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid scan secret"})
		return
	}
	if !present {
		u := h.auth.CurrentUser(r)
		if u == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not signed in"})
			return
		}
		run, err := h.profile.Apply(ctx, u, "ui")
		if err != nil {
			h.profileError(w, "profile apply", err)
			return
		}
		writeJSON(w, http.StatusOK, run)
		return
	}
	users, err := h.store.ListUsers(ctx)
	if err != nil {
		h.internal(w, "list users", err)
		return
	}
	type result struct {
		UserID int64       `json:"userId"`
		Repo   string      `json:"repo"`
		Run    profile.Run `json:"run"`
	}
	out := []result{}
	for i := range users {
		u := &users[i]
		if !h.auth.Allowed(u.Login) {
			continue
		}
		run, err := h.profile.Apply(ctx, u, "cron")
		if err != nil {
			h.log.Warn("profile apply", "user", u.Login, "err", err)
		}
		out = append(out, result{UserID: u.ID, Repo: h.profile.RepoName(u), Run: run})
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": out})
}
