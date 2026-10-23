package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"github.com/kizilkayaom/sefir/mcp/internal/email"
	"golang.org/x/oauth2"
	"net/http"
	"sync"
	"time"
)

type oauthAttempt struct {
	State     string
	Verifier  string
	ExpiresAt time.Time
}

type oauthServer struct {
	config   *oauth2.Config
	attempts map[string]oauthAttempt
	mu       sync.Mutex
}

func (s *oauthServer) handleStart(w http.ResponseWriter, r *http.Request) {
	sessionID := rand.Text()
	state := rand.Text()
	verifier := oauth2.GenerateVerifier()

	var attempt oauthAttempt
	attempt.State = state
	attempt.ExpiresAt = time.Now().Add(10 * time.Minute)
	attempt.Verifier = verifier

	s.mu.Lock()
	s.attempts[sessionID] = attempt
	now := time.Now()
	for id, pending := range s.attempts {
		if !pending.ExpiresAt.After(now) {
			delete(s.attempts, id)
		}
	}
	s.mu.Unlock()

	cookie := &http.Cookie{
		Name:     "sefir_oauth",
		Value:    sessionID,
		Path:     "/oauth/google",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
		Secure:   false,
	}

	http.SetCookie(w, cookie)

	authURL := s.config.AuthCodeURL(
		state,
		oauth2.AccessTypeOffline,
		oauth2.ApprovalForce,
		oauth2.S256ChallengeOption(verifier),
	)

	http.Redirect(w, r, authURL, http.StatusFound)
}

func (s *oauthServer) consumeAttempt(sessionID, state string) (oauthAttempt, error) {
	if sessionID == "" || state == "" {
		return oauthAttempt{}, errors.New("session ID and state are required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	attempt, ok := s.attempts[sessionID]
	if !ok {
		return oauthAttempt{}, errors.New("sign-in attempt not found")
	}
	if !attempt.ExpiresAt.After(time.Now()) {
		delete(s.attempts, sessionID)
		return oauthAttempt{}, errors.New("sign-in attempt has expired")
	}
	if subtle.ConstantTimeCompare([]byte(state), []byte(attempt.State)) != 1 {
		return oauthAttempt{}, errors.New("OAuth state does not match")
	}

	delete(s.attempts, sessionID)
	return attempt, nil
}

func (s *oauthServer) handleCallback(w http.ResponseWriter, r *http.Request) {

	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")

	cookie, err := r.Cookie("sefir_oauth")
	if err != nil || cookie.Value == "" {
		http.Error(w, "Missing sign-in session", http.StatusBadRequest)
		return
	}

	query := r.URL.Query()
	attempt, err := s.consumeAttempt(cookie.Value, query.Get("state"))

	if err != nil {
		http.Error(w, "Invalid or expired sign-in attempt", http.StatusBadRequest)
		return
	}
	cookie = &http.Cookie{
		Name:     "sefir_oauth",
		Value:    "",
		Path:     "/oauth/google",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Secure:   false,
	}

	http.SetCookie(w, cookie)

	if query.Get("error") != "" {
		http.Error(w, "Google authorization was not completed", http.StatusBadRequest)
		return
	}

	if query.Get("code") == "" {
		http.Error(w, "Missing authorization code", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	token, err := s.config.Exchange(
		ctx,
		query.Get("code"),
		oauth2.VerifierOption(attempt.Verifier),
	)

	if err != nil {
		http.Error(w, "Google authorization exchange failed", http.StatusBadGateway)
		return
	}
	if !token.Valid() || token.RefreshToken == "" {
		http.Error(w, "Google did not return the required tokens", http.StatusBadGateway)
		return
	}

	err = email.SaveOAuthToken("../token-gmail.json", token)

	if err != nil {
		http.Error(w, "Could not save Gmail authorization", http.StatusInternalServerError)
		return
	}

	fmt.Fprint(w, "Gmail authorization saved. You can close this tab.")
}
