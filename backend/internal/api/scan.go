package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/harshit3011/gitpulse/backend/internal/auth"
	"github.com/harshit3011/gitpulse/backend/internal/scanner"
)

const scanSecretHeader = "X-Scan-Secret"

// secretState: absent (no header), valid, or invalid.
func (h *handlers) checkSecret(r *http.Request) (present, ok bool) {
	got := r.Header.Get(scanSecretHeader)
	if got == "" {
		return false, false
	}
	a, b := sha256.Sum256([]byte(got)), sha256.Sum256([]byte(h.scanSecret))
	return true, subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

type startedScan struct {
	UserID         int64 `json:"userId"`
	ScanID         int64 `json:"scanId"`
	AlreadyRunning bool  `json:"alreadyRunning"`
}

// POST /api/scan
//   - with X-Scan-Secret: scans every signed-up user (GitHub Actions cron)
//   - otherwise: requires a session and scans that user
//
// An already-running scan is returned rather than treated as an error, so a
// retried cron call is harmless.
func (h *handlers) startScan(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	present, ok := h.checkSecret(r)
	if present && !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid scan secret"})
		return
	}
	var userIDs []int64
	trigger := "ui"
	if present {
		trigger = "cron"
		users, err := h.store.ListUsers(ctx)
		if err != nil {
			h.internal(w, "list users", err)
			return
		}
		for _, u := range users {
			if h.auth.Allowed(u.Login) {
				userIDs = append(userIDs, u.ID)
			}
		}
		if len(userIDs) == 0 {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "no users yet: sign in once through the UI first"})
			return
		}
	} else {
		u := h.auth.CurrentUser(r)
		if u == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not signed in"})
			return
		}
		userIDs = []int64{u.ID}
	}

	out := make([]startedScan, 0, len(userIDs))
	for _, id := range userIDs {
		scanID, existing, err := h.scanner.Start(ctx, id, trigger)
		if err != nil {
			h.internal(w, "start scan", err)
			return
		}
		out = append(out, startedScan{UserID: id, ScanID: scanID, AlreadyRunning: existing})
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"scans": out})
}

// GET /api/scans/{id}: by secret (cron polling) or by the owning session.
func (h *handlers) getScan(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad scan id"})
		return
	}
	present, ok := h.checkSecret(r)
	if present && !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid scan secret"})
		return
	}
	var userID int64
	if !present {
		u := h.auth.CurrentUser(r)
		if u == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not signed in"})
			return
		}
		userID = u.ID
	}
	s, err := scanner.GetScan(r.Context(), h.db, id)
	if errors.Is(err, scanner.ErrNotFound) || (err == nil && !present && s.UserID != userID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "scan not found"})
		return
	}
	if err != nil {
		h.internal(w, "get scan", err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// GET /api/scans/latest: the signed-in user's most recent scan (or null).
func (h *handlers) latestScan(w http.ResponseWriter, r *http.Request) {
	u := auth.UserFrom(r.Context())
	s, err := scanner.LatestScan(r.Context(), h.db, u.ID)
	if errors.Is(err, scanner.ErrNotFound) {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	if err != nil {
		h.internal(w, "latest scan", err)
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (h *handlers) internal(w http.ResponseWriter, what string, err error) {
	h.log.Error(what, "err", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
}
