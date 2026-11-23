package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/harshit3011/gitpulse/backend/internal/auth"
	"github.com/harshit3011/gitpulse/backend/internal/github"
	"github.com/harshit3011/gitpulse/backend/internal/prreview"
	"github.com/harshit3011/gitpulse/backend/internal/store"
)

// webhookPayload covers the fields gitpulse reads from pull_request and
// installation events.
type webhookPayload struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	PullRequest *struct {
		Title   string `json:"title"`
		HTMLURL string `json:"html_url"`
		User    struct {
			Login string `json:"login"`
		} `json:"user"`
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
	} `json:"pull_request"`
	Repository *struct {
		ID       int64  `json:"id"`
		FullName string `json:"full_name"`
		Owner    struct {
			ID int64 `json:"id"`
		} `json:"owner"`
	} `json:"repository"`
	Installation *struct {
		ID      int64 `json:"id"`
		Account *struct {
			ID int64 `json:"id"`
		} `json:"account"`
	} `json:"installation"`
}

var reviewActions = map[string]bool{"opened": true, "synchronize": true, "reopened": true, "ready_for_review": true}

// POST /api/webhooks/github. The signature is checked before anything is
// parsed. Work is queued in the database and the reply is immediate (202).
func (h *handlers) githubWebhook(w http.ResponseWriter, r *http.Request) {
	if len(h.webhookKey) == 0 {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "webhooks are not configured (GITHUB_WEBHOOK_SECRET)"})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 10<<20))
	if err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "payload too large"})
		return
	}
	if !prreview.VerifySignature(h.webhookKey, body, r.Header.Get("X-Hub-Signature-256")) {
		h.log.Warn("webhook signature mismatch", "delivery", r.Header.Get("X-GitHub-Delivery"))
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "bad signature"})
		return
	}
	event, delivery := r.Header.Get("X-GitHub-Event"), r.Header.Get("X-GitHub-Delivery")
	var p webhookPayload
	if err := json.Unmarshal(body, &p); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	log := h.log.With("event", event, "action", p.Action, "delivery", delivery)
	ctx := r.Context()

	switch event {
	case "ping":
		writeJSON(w, http.StatusOK, map[string]string{"status": "pong"})
	case "installation", "installation_repositories":
		if p.Installation == nil || p.Installation.Account == nil {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
			return
		}
		if p.Action == "deleted" {
			err = h.install.Delete(ctx, p.Installation.ID)
		} else if _, lerr := h.install.Lookup(ctx, p.Installation.Account.ID); lerr != nil && !errors.Is(lerr, github.ErrNotInstalled) {
			err = lerr
		}
		if err != nil {
			h.internal(w, "installation webhook", err)
			return
		}
		log.Info("installation updated")
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case "pull_request":
		if !reviewActions[p.Action] || p.PullRequest == nil || p.Repository == nil {
			writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
			return
		}
		u, err := h.store.GetUser(ctx, p.Repository.Owner.ID)
		if errors.Is(err, store.ErrNotFound) || (err == nil && !h.auth.Allowed(u.Login)) {
			log.Info("webhook for unknown or non-allowlisted owner ignored", "repo", p.Repository.FullName)
			writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
			return
		}
		if err != nil {
			h.internal(w, "webhook user", err)
			return
		}
		created, err := h.prs.Enqueue(ctx, prreview.Job{
			UserID: u.ID, RepoID: p.Repository.ID, RepoName: p.Repository.FullName, Number: p.Number,
			HeadSHA: p.PullRequest.Head.SHA, Title: p.PullRequest.Title, Author: p.PullRequest.User.Login,
			URL: p.PullRequest.HTMLURL, Source: "webhook",
		})
		if err != nil {
			h.internal(w, "enqueue review", err)
			return
		}
		log.Info("pr webhook", "repo", p.Repository.FullName, "pr", p.Number, "queued", created)
		writeJSON(w, http.StatusAccepted, map[string]bool{"queued": created})
	default:
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
	}
}

// POST /api/prs/sync: SCAN_SECRET (Actions cron, all users) or a session.
func (h *handlers) syncPRs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	present, ok := h.checkSecret(r)
	if present && !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid scan secret"})
		return
	}
	var users []store.User
	if present {
		all, err := h.store.ListUsers(ctx)
		if err != nil {
			h.internal(w, "list users", err)
			return
		}
		for _, u := range all {
			if h.auth.Allowed(u.Login) {
				users = append(users, u)
			}
		}
	} else {
		u := h.auth.CurrentUser(r)
		if u == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not signed in"})
			return
		}
		users = []store.User{*u}
	}
	results := []prreview.SyncResult{}
	for i := range users {
		res, err := h.prs.Sync(ctx, &users[i])
		if err != nil {
			if errors.Is(err, github.ErrNotInstalled) {
				h.log.Warn("pr sync: app not installed", "user", users[i].Login)
				continue
			}
			h.internal(w, "pr sync", err)
			return
		}
		h.log.Info("pr sync", "user", users[i].Login, "repos", res.ReposChecked, "open", res.OpenPRs, "queued", res.Queued)
		results = append(results, res)
	}
	writeJSON(w, http.StatusOK, map[string]any{"users": results})
}

func (h *handlers) listPRs(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 && v <= 500 {
		limit = v
	}
	rows, err := h.prs.Recent(r.Context(), auth.UserFrom(r.Context()).ID, limit)
	if err != nil {
		h.internal(w, "list reviews", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reviews": rows, "aiEnabled": h.llm.Enabled()})
}

func (h *handlers) getPR(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	row, err := h.prs.Get(r.Context(), auth.UserFrom(r.Context()).ID, id)
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
	case err != nil:
		h.internal(w, "get review", err)
	default:
		writeJSON(w, http.StatusOK, row)
	}
}
