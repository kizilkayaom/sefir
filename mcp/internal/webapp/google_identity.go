package webapp

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/kizilkayaom/sefir/mcp/internal/draftcontent"
	"github.com/kizilkayaom/sefir/mcp/internal/email"
	"golang.org/x/oauth2"
)

type GoogleIdentity struct {
	Subject string
	Email   string
}

type GoogleIdentityProvider interface {
	Start(state, nonce, verifier string) string
	Complete(context.Context, string, string, string) (GoogleIdentity, error)
}

type GoogleSignIn struct {
	config   *oauth2.Config
	verifier *oidc.IDTokenVerifier
}

func NewGoogleSignIn(credentialsPath, redirectURL string) (*GoogleSignIn, error) {
	config, err := email.LoadOAuthConfig(credentialsPath, redirectURL)
	if err != nil {
		return nil, err
	}
	config.Scopes = []string{oidc.ScopeOpenID, "email"}
	ctx := oidc.ClientContext(context.Background(), &http.Client{Timeout: 15 * time.Second})
	keys := oidc.NewRemoteKeySet(ctx, "https://www.googleapis.com/oauth2/v3/certs")
	return &GoogleSignIn{
		config:   config,
		verifier: oidc.NewVerifier("https://accounts.google.com", keys, &oidc.Config{ClientID: config.ClientID}),
	}, nil
}

func (g *GoogleSignIn) Start(state, nonce, verifier string) string {
	return g.config.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("prompt", "select_account"))
}

func (g *GoogleSignIn) Complete(ctx context.Context, code, nonce, verifier string) (GoogleIdentity, error) {
	token, err := g.config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return GoogleIdentity{}, err
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		return GoogleIdentity{}, errors.New("Google did not return an identity token")
	}
	identity, err := g.verifier.Verify(ctx, raw)
	if err != nil {
		return GoogleIdentity{}, err
	}
	if nonce == "" || subtle.ConstantTimeCompare([]byte(identity.Nonce), []byte(nonce)) != 1 ||
		identity.Subject == "" || len(identity.Subject) > 255 {
		return GoogleIdentity{}, errors.New("invalid Google identity")
	}
	var claims struct {
		Email    string `json:"email"`
		Verified bool   `json:"email_verified"`
	}
	if err := identity.Claims(&claims); err != nil || !claims.Verified {
		return GoogleIdentity{}, errors.New("Google email is not verified")
	}
	address, err := draftcontent.NormalizeRecipient(claims.Email)
	if err != nil {
		return GoogleIdentity{}, err
	}
	return GoogleIdentity{Subject: identity.Subject, Email: address}, nil
}
