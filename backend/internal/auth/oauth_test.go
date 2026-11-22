package auth

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// fakeGitHub serves the token exchange and /user endpoints.
func fakeGitHub(t *testing.T, login string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("code") != "good-code" {
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad_verification_code"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "ghu_x"})
	})
	mux.HandleFunc("GET /user", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ghu_x" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 7, "node_id": "U_7", "login": login})
	})
	mux.HandleFunc("DELETE /applications/cid/token", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newTestService(gh *httptest.Server) *Service {
	return NewService(Config{
		ClientID: "cid", ClientSecret: "csecret", RedirectURL: "http://localhost/api/auth/callback",
		AllowedUsers: map[string]bool{"harshit3011": true}, FrontendURL: "http://localhost:5173",
		AuthorizeURL: gh.URL + "/login/oauth/authorize", TokenURL: gh.URL + "/login/oauth/access_token",
		APIBaseURL: gh.URL,
	}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestLoginSetsStateAndRedirects(t *testing.T) {
	s := newTestService(fakeGitHub(t, "harshit3011"))
	rec := httptest.NewRecorder()
	s.Login(rec, httptest.NewRequest("GET", "/api/auth/login", nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("status %d", rec.Code)
	}
	loc, _ := url.Parse(rec.Header().Get("Location"))
	var state string
	for _, c := range rec.Result().Cookies() {
		if c.Name == stateCookie {
			state = c.Value
			if !c.HttpOnly {
				t.Error("state cookie must be HttpOnly")
			}
		}
	}
	if state == "" || loc.Query().Get("state") != state {
		t.Fatalf("state not bound: cookie=%q url=%q", state, loc.Query().Get("state"))
	}
	// GitHub Apps ignore scopes; permissions come from the app settings.
	if loc.Query().Has("scope") {
		t.Errorf("unexpected scope param %q", loc.Query().Get("scope"))
	}
	if loc.Query().Get("client_id") != "cid" {
		t.Errorf("client_id = %q", loc.Query().Get("client_id"))
	}
}

func callback(s *Service, state, cookieState, code string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/api/auth/callback?state="+state+"&code="+code, nil)
	if cookieState != "" {
		req.AddCookie(&http.Cookie{Name: stateCookie, Value: cookieState})
	}
	rec := httptest.NewRecorder()
	s.Callback(rec, req)
	return rec
}

func TestCallbackRejectsStateMismatch(t *testing.T) {
	s := newTestService(fakeGitHub(t, "harshit3011"))
	rec := callback(s, "abc", "xyz", "good-code")
	if got := rec.Header().Get("Location"); got != "http://localhost:5173/login?error=invalid_state" {
		t.Fatalf("location = %q", got)
	}
}

func TestCallbackRejectsUserNotInAllowlist(t *testing.T) {
	// The store is nil: reaching it would panic, proving rejection happens first.
	s := newTestService(fakeGitHub(t, "intruder"))
	rec := callback(s, "abc", "abc", "good-code")
	if got := rec.Header().Get("Location"); got != "http://localhost:5173/login?error=not_allowed" {
		t.Fatalf("location = %q", got)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == SessionCookie {
			t.Fatal("session cookie must not be set for rejected user")
		}
	}
}

func TestCallbackBadCode(t *testing.T) {
	s := newTestService(fakeGitHub(t, "harshit3011"))
	rec := callback(s, "abc", "abc", "bad")
	if got := rec.Header().Get("Location"); got != "http://localhost:5173/login?error=exchange_failed" {
		t.Fatalf("location = %q", got)
	}
}
