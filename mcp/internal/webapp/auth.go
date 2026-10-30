package webapp

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type session struct {
	AccountID    string
	CSRF         string
	Expires      time.Time
	OAuthState   string
	Verifier     string
	OAuthExpires time.Time
}

const signupClosedMessage = "Registration is closed on this instance. Existing accounts can still sign in."

func (s *Server) currentSession(r *http.Request) (string, session, bool) {
	cookie, err := r.Cookie("sefir_session")
	if err != nil {
		return "", session{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.sessions[cookie.Value]
	if !ok || !value.Expires.After(time.Now()) {
		delete(s.sessions, cookie.Value)
		return "", session{}, false
	}
	return cookie.Value, value, true
}

func (s *Server) setCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name: "sefir_session", Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: s.origin.Scheme == "https", SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) sessionResponse(w http.ResponseWriter, account Account, value session) {
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true, "owner": account.Username, "account_id": account.ID,
		"csrf_token": value.CSRF, "google_available": s.config.GoogleAuth != nil,
		"password_available": len(account.Hash) == 32,
		"signup_available":   s.config.SignupEnabled,
	})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	_, value, ok := s.currentSession(r)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated": false, "signup_available": s.config.SignupEnabled,
			"google_available": s.config.GoogleAuth != nil, "password_available": true,
		})
		return
	}
	account, err := s.accounts.ByID(r.Context(), value.AccountID)
	if err != nil {
		storeError(w, err)
		return
	}
	s.sessionResponse(w, account, value)
}

func (s *Server) allowAuthRequest(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Origin") != s.config.Origin {
		fail(w, http.StatusForbidden, "Unexpected request origin")
		return false
	}
	if !s.loginRate.Allow() {
		w.Header().Set("Retry-After", "10")
		fail(w, http.StatusTooManyRequests, "Too many attempts. Wait before trying again.")
		return false
	}
	return true
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if !s.allowAuthRequest(w, r) {
		return
	}
	if !s.config.SignupEnabled {
		fail(w, http.StatusForbidden, signupClosedMessage)
		return
	}
	var input struct {
		Username        string `json:"username"`
		Password        string `json:"password"`
		ConfirmPassword string `json:"confirm_password"`
	}
	if !decode(w, r, &input) {
		return
	}
	input.Username = strings.TrimSpace(input.Username)
	if err := validateUsername(input.Username); err != nil {
		fail(w, 422, err.Error())
		return
	}
	if err := validatePassword(input.Password); err != nil {
		fail(w, 422, err.Error())
		return
	}
	if input.Password != input.ConfirmPassword {
		fail(w, 422, "Passwords do not match")
		return
	}
	credentials, err := newOwner(input.Username, input.Password)
	if err != nil {
		fail(w, 500, "Could not create your account. Please try again.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	account, err := s.accounts.Create(ctx, *credentials)
	if errors.Is(err, ErrAccountConflict) {
		fail(w, http.StatusConflict, "That username is already taken. Choose another or sign in.")
		return
	}
	if err != nil {
		fail(w, 503, "Could not save your account. Please try again.")
		return
	}
	s.startSession(w, r, account)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.allowAuthRequest(w, r) {
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &input) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	account, err := s.accounts.ByUsername(ctx, input.Username)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		storeError(w, err)
		return
	}
	if err != nil || !account.matches(input.Username, input.Password) {
		fail(w, http.StatusUnauthorized, "Incorrect username or password")
		return
	}
	s.startSession(w, r, account)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, account Account) {
	value, ok := s.createSession(w, r, account)
	if ok {
		s.sessionResponse(w, account, value)
	}
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request, account Account) (session, bool) {
	id := rand.Text()
	value := session{AccountID: account.ID, CSRF: rand.Text(), Expires: time.Now().Add(8 * time.Hour)}
	s.mu.Lock()
	for key, existing := range s.sessions {
		if !existing.Expires.After(time.Now()) {
			delete(s.sessions, key)
		}
	}
	if cookie, err := r.Cookie("sefir_session"); err == nil {
		delete(s.sessions, cookie.Value)
	}
	if len(s.sessions) >= 1000 {
		s.mu.Unlock()
		fail(w, http.StatusTooManyRequests, "Too many active sessions")
		return session{}, false
	}
	s.sessions[id] = value
	s.mu.Unlock()
	s.setCookie(w, id, 8*60*60)
	return value, true
}

func (s *Server) protect(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, value, ok := s.currentSession(r)
		if !ok {
			fail(w, http.StatusUnauthorized, "Sign in to continue")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if r.Header.Get("Origin") != s.config.Origin || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(value.CSRF)) != 1 {
				fail(w, http.StatusForbidden, "Session validation failed. Refresh and try again.")
				return
			}
		}
		account, err := s.accounts.ByID(r.Context(), value.AccountID)
		if err != nil {
			storeError(w, err)
			return
		}
		next(w, r.WithContext(withAccount(r.Context(), account)))
	}
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	id, _, _ := s.currentSession(r)
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
	s.setCookie(w, "", -1)
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": false})
}

func (s *Server) invalidateAccountSessions(accountID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, attempt := range s.googleAttempts {
		value, exists := s.sessions[attempt.SessionID]
		if exists && value.AccountID == accountID {
			delete(s.googleAttempts, key)
		}
	}

	for key, value := range s.sessions {
		if value.AccountID == accountID {
			delete(s.sessions, key)
		}
	}
}
