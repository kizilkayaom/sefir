package webapp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kizilkayaom/sefir/mcp/internal/workspace"
)

func setupConfig(t *testing.T) Config {
	t.Helper()
	root := t.TempDir()
	return Config{Origin: "http://localhost:8080", Dist: root, SignupEnabled: true,
		CredentialsPath: filepath.Join(root, ".sefir", "owner.json"), Accounts: newMemoryAccounts()}
}

func setupBody(username, password, confirmation string) string {
	body, _ := json.Marshal(map[string]string{"username": username, "password": password, "confirm_password": confirmation})
	return string(body)
}

func TestOwnerValidation(t *testing.T) {
	for _, name := range []string{"omer", "o.mer_1-2", strings.Repeat("a", 32)} {
		if err := validateUsername(name); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"", "ab", "two names", "name@email.com", strings.Repeat("a", 33)} {
		if validateUsername(name) == nil {
			t.Fatal("invalid username accepted")
		}
	}
	for _, password := range []string{"letter1!", "Şifre12!", strings.Repeat("a", 126) + "1!", "  word1!"} {
		if err := validatePassword(password); err != nil {
			t.Fatal(err)
		}
	}
	for _, password := range []string{"short1!", "letters!", "letters1", "1234567!", "word12  ", strings.Repeat("a", 127) + "1!", "letter1!\xff"} {
		if validatePassword(password) == nil {
			t.Fatal("invalid password accepted")
		}
	}
}

func TestPasswordSignupCreatesSeparateAccounts(t *testing.T) {
	config := setupConfig(t)
	server, err := New(config, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	first := request(server, "POST", "/api/signup", setupBody(" Owner ", testPassword, testPassword), nil, "")
	if first.Code != 200 {
		t.Fatal(first.Code, first.Body.String())
	}
	cookie := namedCookie(t, first, "sefir_session")
	owner, err := config.Accounts.ByUsername(context.Background(), "owner")
	if err != nil || len(owner.Hash) != 32 || !owner.matches("owner", testPassword) {
		t.Fatal(owner.ID, err)
	}
	second := request(server, "POST", "/api/signup", setupBody("Other", testPassword, testPassword), nil, "")
	if second.Code != 200 {
		t.Fatal(second.Code, second.Body.String())
	}
	other, err := config.Accounts.ByUsername(context.Background(), "Other")
	if err != nil || owner.ID == other.ID {
		t.Fatal("signup reused an existing account")
	}
	if got := request(server, "GET", "/api/session", "", cookie, "").Body.String(); !strings.Contains(got, `"owner":"Owner"`) {
		t.Fatal(got)
	}
	restarted, err := New(config, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := request(restarted, "GET", "/api/session", "", cookie, "").Body.String(); !strings.Contains(got, `"authenticated":false`) {
		t.Fatal("restart preserved a session")
	}
	login(t, restarted)
	if got := request(restarted, "POST", "/api/signup", setupBody("owner", testPassword, testPassword), nil, "").Code; got != 409 {
		t.Fatal("duplicate username accepted", got)
	}
}

func TestSignupRejectsInvalidRequests(t *testing.T) {
	for _, input := range []string{
		setupBody("a b", testPassword, testPassword), setupBody("Owner", "short1!", "short1!"),
		setupBody("Owner", "letters!", "letters!"), setupBody("Owner", "letters1", "letters1"),
		setupBody("Owner", "1234567!", "1234567!"), setupBody("Owner", testPassword, "different1!"),
	} {
		config := setupConfig(t)
		server, err := New(config, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		response := request(server, "POST", "/api/signup", input, nil, "")
		if response.Code != 422 || len(response.Result().Cookies()) != 0 {
			t.Fatal(response.Code)
		}
		if len(config.Accounts.(*memoryAccounts).items) != 0 {
			t.Fatal("invalid signup created an account")
		}
	}
}

func TestSignupRequiresSameOriginAndAllowsHTTPSVisitors(t *testing.T) {
	config := setupConfig(t)
	config.Origin = "https://sefir.example"
	server, err := New(config, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, origin := range []string{"https://other.example", config.Origin} {
		r := httptest.NewRequest("POST", config.Origin+"/api/signup", strings.NewReader(setupBody("Owner", testPassword, testPassword)))
		r.RemoteAddr = "192.0.2.1:1234"
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.Handler().ServeHTTP(w, r)
		if origin != config.Origin && w.Code != 403 {
			t.Fatal(w.Code)
		}
		if origin == config.Origin && (w.Code != 200 || !namedCookie(t, w, "sefir_session").Secure) {
			t.Fatal(w.Code)
		}
	}
}

func TestConcurrentSignupRejectsDuplicateUsername(t *testing.T) {
	config := setupConfig(t)
	first, err := New(config, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(config, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan int, 2)
	for _, server := range []*Server{first, second} {
		go func(s *Server) {
			results <- request(s, "POST", "/api/signup", setupBody("Owner", testPassword, testPassword), nil, "").Code
		}(server)
	}
	counts := map[int]int{}
	counts[<-results]++
	counts[<-results]++
	if counts[200] != 1 || counts[409] != 1 {
		t.Fatal(counts)
	}
}

func TestSignupStorageFailureDoesNotCreateSession(t *testing.T) {
	config := setupConfig(t)
	server, err := New(config, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	config.Accounts.(*memoryAccounts).err = errors.New("database unavailable")
	response := request(server, "POST", "/api/signup", setupBody("Owner", testPassword, testPassword), nil, "")
	if response.Code != 503 || len(response.Result().Cookies()) != 0 {
		t.Fatal(response.Code)
	}
}

func TestLegacyAccountImportPreservesIdentity(t *testing.T) {
	config := setupConfig(t)
	owner, err := newOwner("Owner", testPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := saveOwner(config.CredentialsPath, owner); err != nil {
		t.Fatal(err)
	}
	server, err := New(config, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	login(t, server)
	account, err := config.Accounts.ByID(context.Background(), workspace.LegacyAccountID)
	if err != nil || account.Username != "Owner" {
		t.Fatal(account.ID, err)
	}
	config.Owner, config.Password = "Different", "another1!"
	if _, err := New(config, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	account, err = config.Accounts.ByID(context.Background(), workspace.LegacyAccountID)
	if err != nil || account.Username != "Owner" {
		t.Fatal("restart overwrote the imported account")
	}
}

func TestOwnerConfiguration(t *testing.T) {
	config := setupConfig(t)
	config.Owner, config.Password = " Owner ", testPassword
	server, err := New(config, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	login(t, server)
	if server.config.Password != "" {
		t.Fatal("retained plaintext password")
	}
	config.Password = ""
	if _, err := New(config, nil, nil, nil); err == nil {
		t.Fatal("partial environment credentials accepted")
	}
	config.Owner = ""
	if err := os.MkdirAll(filepath.Dir(config.CredentialsPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.CredentialsPath, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(config, nil, nil, nil); err == nil {
		t.Fatal("invalid legacy credentials ignored")
	}
}

func TestInvalidateAccountSessions(t *testing.T) {
	server := &Server{
		sessions: map[string]session{
			"session-a-1": {AccountID: "account-a"},
			"session-a-2": {AccountID: "account-a"},
			"session-b-1": {AccountID: "account-b"},
		},
		googleAttempts: map[string]googleAttempt{
			"attempt-a-1": {SessionID: "session-a-1"},
			"attempt-a-2": {SessionID: "session-a-2"},
			"attempt-b-1": {SessionID: "session-b-1"},
		},
	}
	for call := 1; call <= 2; call++ {
		server.invalidateAccountSessions("account-a")
		for _, id := range []string{"session-a-1", "session-a-2"} {
			if _, exists := server.sessions[id]; exists {
				t.Fatalf("session %s was not removed after call %d", id, call)
			}
		}
		for _, id := range []string{"attempt-a-1", "attempt-a-2"} {
			if _, exists := server.googleAttempts[id]; exists {
				t.Fatalf("Google attempt %s was not removed after call %d", id, call)
			}
		}
		if value, exists := server.sessions["session-b-1"]; !exists || value.AccountID != "account-b" {
			t.Fatalf("account-b session was removed or changed after call %d", call)
		}
		if value, exists := server.googleAttempts["attempt-b-1"]; !exists || value.SessionID != "session-b-1" {
			t.Fatalf("account-b Google attempt was removed or changed after call %d", call)
		}
	}
}
