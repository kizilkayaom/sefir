package main

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"net/http"
	"sync"
	"time"

	"golang.org/x/oauth2"
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
