package webapp

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/kizilkayaom/sefir/mcp/internal/email"
	"golang.org/x/oauth2"
)

type GmailStatus struct {
	Connected bool   `json:"connected"`
	Email     string `json:"email,omitempty"`
	Message   string `json:"message,omitempty"`
}

type Gmail interface {
	Status(context.Context) GmailStatus
	Start(string, string) (string, error)
	Complete(context.Context, string, string) error
}

type GmailConnection struct {
	CredentialsPath string
	TokenPath       string
	RedirectURL     string
	mu              sync.Mutex
	disabled        bool
}

var errGmailConnectionDisabled = errors.New("gmail connection is disabled")

func (g *GmailConnection) Status(ctx context.Context) GmailStatus {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.disabled {
		return GmailStatus{Message: "Gmail connection is disabled."}
	}
	config, err := email.LoadOAuthConfig(g.CredentialsPath, g.RedirectURL)
	if err != nil {
		return GmailStatus{Message: "Configure the Gmail OAuth credentials file on the server."}
	}
	client, err := email.NewGmailClient(ctx, config, g.TokenPath)
	if err != nil {
		return GmailStatus{Message: "Connect Gmail to authorize this account."}
	}
	profile, err := email.GetGmailProfile(ctx, client)
	if err != nil {
		return GmailStatus{Message: "Gmail could not be reached or authorization expired. Try again or reconnect."}
	}

	return GmailStatus{Connected: true, Email: profile.EmailAddress}
}

func (g *GmailConnection) Start(state, verifier string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.disabled {
		return "", errGmailConnectionDisabled
	}
	config, err := email.LoadOAuthConfig(g.CredentialsPath, g.RedirectURL)
	if err != nil {
		return "", err
	}
	return config.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce, oauth2.S256ChallengeOption(verifier)), nil
}

func (g *GmailConnection) Complete(ctx context.Context, code, verifier string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.disabled {
		return errGmailConnectionDisabled
	}
	config, err := email.LoadOAuthConfig(g.CredentialsPath, g.RedirectURL)
	if err != nil {
		return err
	}
	token, err := config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(g.TokenPath), 0700); err != nil {
		return err
	}
	return email.SaveOAuthToken(g.TokenPath, token)
}

func (s *Server) accountGmail(ctx context.Context) (Gmail, error) {
	id, err := contextAccountID(ctx)
	if err != nil {
		return nil, err
	}
	if s.config.GmailForAccount != nil {
		return s.config.GmailForAccount(id)
	}
	return s.gmail, nil
}

func (s *Server) handleGmailStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	gmail, err := s.accountGmail(ctx)
	if err != nil || gmail == nil {
		fail(w, 503, "Gmail connection is unavailable")
		return
	}
	writeJSON(w, http.StatusOK, gmail.Status(ctx))
}

func (s *Server) handleGmailConnect(w http.ResponseWriter, r *http.Request) {
	id, value, ok := s.currentSession(r)
	if !ok {
		fail(w, http.StatusUnauthorized, "Sign in to continue")
		return
	}
	value.OAuthState = rand.Text()
	value.Verifier = oauth2.GenerateVerifier()
	value.OAuthExpires = time.Now().Add(10 * time.Minute)
	gmail, err := s.accountGmail(r.Context())
	if err != nil || gmail == nil {
		fail(w, 503, "Gmail connection is unavailable")
		return
	}
	authURL, err := gmail.Start(value.OAuthState, value.Verifier)
	if err != nil {
		fail(w, http.StatusServiceUnavailable, "Configure the Gmail OAuth credentials file on the server.")
		return
	}
	s.mu.Lock()
	if _, exists := s.sessions[id]; exists {
		s.sessions[id] = value
	}
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"url": authURL})
}

func (s *Server) handleGmailCallback(w http.ResponseWriter, r *http.Request) {
	id, _, _ := s.currentSession(r)
	s.mu.Lock()
	value, exists := s.sessions[id]
	state := r.URL.Query().Get("state")
	valid := exists && state != "" && value.OAuthExpires.After(time.Now()) &&
		subtle.ConstantTimeCompare([]byte(state), []byte(value.OAuthState)) == 1
	if valid {
		updated := value
		updated.OAuthState, updated.Verifier = "", ""
		s.sessions[id] = updated
	}
	s.mu.Unlock()
	if !valid {
		fail(w, http.StatusBadRequest, "Invalid or expired Gmail connection attempt")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" || r.URL.Query().Get("error") != "" {
		http.Redirect(w, r, "/?gmail=cancelled#settings", http.StatusSeeOther)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	gmail, err := s.accountGmail(ctx)
	if err != nil || gmail == nil {
		http.Redirect(w, r, "/?gmail=failed#settings", http.StatusSeeOther)
		return
	}
	if err := gmail.Complete(ctx, code, value.Verifier); err != nil {
		http.Redirect(w, r, "/?gmail=failed#settings", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/?gmail=connected#settings", http.StatusSeeOther)
}

func (g *GmailConnection) Disable() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.disabled = true
}

func (g *GmailConnection) RemoveToken() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.disabled = true

	if g.TokenPath == "" {
		return errors.New("gmail token path is required")
	}

	err := os.Remove(g.TokenPath)

	if errors.Is(err, os.ErrNotExist) {
		return nil
	} else {
		return err
	}
}
