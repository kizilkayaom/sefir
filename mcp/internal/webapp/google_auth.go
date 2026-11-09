package webapp

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"
)

type googleAttempt struct {
	State, Nonce, Verifier, Mode, SessionID string
	Expires                                 time.Time
}

func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	account := contextAccount(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"google_available":  s.config.GoogleAuth != nil,
		"google_linked":     account.GoogleSubject != "",
		"google_email":      account.GoogleEmail,
		"default_preset_id": account.DefaultPresetID,
	})
}

func (s *Server) handleGoogleStart(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Mode string `json:"mode"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Mode != "signup" && input.Mode != "signin" {
		fail(w, 422, "Choose sign up or sign in")
		return
	}
	s.startGoogle(w, r, input.Mode)
}

func (s *Server) handleGoogleLink(w http.ResponseWriter, r *http.Request) {
	s.startGoogle(w, r, "link")
}

func (s *Server) startGoogle(w http.ResponseWriter, r *http.Request, mode string) {
	if !s.allowAuthRequest(w, r) {
		return
	}
	if mode == "signup" && !s.config.SignupEnabled {
		fail(w, http.StatusForbidden, signupClosedMessage)
		return
	}
	if s.config.GoogleAuth == nil {
		fail(w, 503, "Google sign-in is not configured on this server")
		return
	}
	id, _, authenticated := s.currentSession(r)
	if mode == "link" && !authenticated {
		fail(w, 401, "Sign in before linking Google")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, attempt := range s.googleAttempts {
		if !attempt.Expires.After(time.Now()) {
			delete(s.googleAttempts, key)
		}
	}
	if len(s.googleAttempts) >= 100 {
		fail(w, 429, "Too many sign-in attempts. Try again shortly.")
		return
	}
	if cookie, err := r.Cookie("sefir_google"); err == nil {
		delete(s.googleAttempts, cookie.Value)
	}
	key := rand.Text()
	attempt := googleAttempt{
		State: rand.Text(), Nonce: rand.Text(), Verifier: oauth2.GenerateVerifier(),
		Mode: mode, SessionID: id, Expires: time.Now().Add(10 * time.Minute),
	}
	s.googleAttempts[key] = attempt
	s.googleCookie(w, key, 600)
	writeJSON(w, http.StatusOK, map[string]string{"url": s.config.GoogleAuth.Start(attempt.State, attempt.Nonce, attempt.Verifier)})
}

func (s *Server) googleCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: "sefir_google", Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: s.origin.Scheme == "https", SameSite: http.SameSiteLaxMode})
}

func (s *Server) handleGoogleSignInCallback(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("sefir_google")
	if err != nil || s.config.GoogleAuth == nil {
		http.Redirect(w, r, "/?auth=expired#signin", 303)
		return
	}
	s.mu.Lock()
	attempt, exists := s.googleAttempts[cookie.Value]
	valid := exists && attempt.Expires.After(time.Now()) && r.URL.Query().Get("state") != "" &&
		subtle.ConstantTimeCompare([]byte(attempt.State), []byte(r.URL.Query().Get("state"))) == 1
	if valid {
		delete(s.googleAttempts, cookie.Value)
	}
	s.mu.Unlock()
	if !valid {
		http.Redirect(w, r, "/?auth=expired#signin", 303)
		return
	}
	s.googleCookie(w, "", -1)
	page := "signin"
	if attempt.Mode == "link" {
		page = "settings"
	} else if attempt.Mode == "signup" {
		page = "signup"
	}
	failure := func(reason string) { http.Redirect(w, r, "/?auth="+reason+"#"+page, 303) }
	if r.URL.Query().Get("error") != "" || r.URL.Query().Get("code") == "" {
		failure("cancelled")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	identity, err := s.config.GoogleAuth.Complete(ctx, r.URL.Query().Get("code"), attempt.Nonce, attempt.Verifier)
	if err != nil {
		failure("failed")
		return
	}
	var account Account
	if attempt.Mode == "link" {
		sessionID, value, authenticated := s.currentSession(r)
		if !authenticated || sessionID != attempt.SessionID {
			failure("expired")
			return
		}
		account, err = s.accounts.LinkGoogle(ctx, value.AccountID, identity)
	} else if s.config.SignupEnabled {
		account, err = s.accounts.Google(ctx, identity)
	} else {
		account, err = s.accounts.ByGoogleSubject(ctx, identity.Subject)
		if errors.Is(err, pgx.ErrNoRows) {
			failure("signup_closed")
			return
		}
	}
	if err != nil {
		if errors.Is(err, ErrAccountConflict) {
			failure("already_linked")
		} else {
			failure("failed")
		}
		return
	}
	if _, ok := s.createSession(w, r, account); !ok {
		return
	}
	destination := "/#drafts"
	if attempt.Mode == "link" {
		destination = "/?auth=linked#settings"
	}
	http.Redirect(w, r, destination, 303)
}
