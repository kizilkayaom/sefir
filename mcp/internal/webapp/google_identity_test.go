package webapp

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

func TestGoogleIdentityVerification(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"valid", "nonce", "audience", "issuer", "expired", "unverified", "missing subject", "signature", "missing token"} {
		t.Run(scenario, func(t *testing.T) {
			claims := map[string]any{
				"iss": "https://accounts.google.com", "aud": "client-id", "sub": "google-subject",
				"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": "nonce",
				"email": "owner@example.com", "email_verified": true,
			}
			signingKey := key
			switch scenario {
			case "nonce":
				claims["nonce"] = "different"
			case "audience":
				claims["aud"] = "another-client"
			case "issuer":
				claims["iss"] = "https://other.example"
			case "expired":
				claims["exp"] = time.Now().Add(-time.Hour).Unix()
			case "unverified":
				claims["email_verified"] = false
			case "missing subject":
				delete(claims, "sub")
			case "signature":
				signingKey = otherKey
			}
			body, err := json.Marshal(claims)
			if err != nil {
				t.Fatal(err)
			}
			unsigned := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." + base64.RawURLEncoding.EncodeToString(body)
			hash := sha256.Sum256([]byte(unsigned))
			signature, err := rsa.SignPKCS1v15(rand.Reader, signingKey, crypto.SHA256, hash[:])
			if err != nil {
				t.Fatal(err)
			}
			token := unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
			endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				if r.Form.Get("code") != "code" || r.Form.Get("code_verifier") != "verifier" || r.Form.Get("redirect_uri") != "http://localhost:8080/oauth/google/sign-in/callback" {
					t.Error("invalid exchange")
				}
				w.Header().Set("Content-Type", "application/json")
				result := map[string]any{"access_token": "temporary", "token_type": "Bearer", "id_token": token}
				if scenario == "missing token" {
					delete(result, "id_token")
				}
				json.NewEncoder(w).Encode(result)
			}))
			defer endpoint.Close()
			g := &GoogleSignIn{
				config:   &oauth2.Config{ClientID: "client-id", ClientSecret: "test", RedirectURL: "http://localhost:8080/oauth/google/sign-in/callback", Endpoint: oauth2.Endpoint{AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: endpoint.URL}, Scopes: []string{"openid", "email"}},
				verifier: oidc.NewVerifier("https://accounts.google.com", &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}}, &oidc.Config{ClientID: "client-id"}),
			}
			authURL, err := url.Parse(g.Start("state", "nonce", "verifier"))
			if err != nil {
				t.Fatal(err)
			}
			query := authURL.Query()
			if query.Get("nonce") != "nonce" || query.Get("state") != "state" || query.Get("code_challenge_method") != "S256" || query.Get("scope") != "openid email" {
				t.Fatal(query)
			}
			identity, err := g.Complete(context.Background(), "code", "nonce", "verifier")
			if scenario == "valid" {
				if err != nil || identity.Subject != "google-subject" || identity.Email != "owner@example.com" {
					t.Fatal(identity, err)
				}
			} else if err == nil {
				t.Fatal("invalid identity accepted")
			}
		})
	}
}
