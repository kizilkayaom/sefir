package webapp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/time/rate"

	"github.com/kizilkayaom/sefir/mcp/internal/workspace"
)

type Config struct {
	Origin            string
	SignupEnabled     bool
	Owner             string
	Password          string
	Dist              string
	CredentialsPath   string
	GoogleAuth        GoogleIdentityProvider
	Accounts          Accounts
	GmailForAccount   func(string) (Gmail, error)
	OperatorAccountID string
	AI                *AIConfiguration
}

type Server struct {
	config         Config
	origin         *url.URL
	store          Store
	bridge         Bridge
	gmail          Gmail
	mu             sync.Mutex
	accounts       Accounts
	sessions       map[string]session
	loginRate      *rate.Limiter
	generating     chan struct{}
	googleAttempts map[string]googleAttempt
	modelCatalogs  map[string]*aiModelCache
}

func New(config Config, store Store, bridge Bridge, gmail Gmail) (*Server, error) {
	if config.OperatorAccountID == "" {
		config.OperatorAccountID = workspace.LegacyAccountID
	}
	if !uuidPattern.MatchString(config.OperatorAccountID) {
		return nil, errors.New("SEFIR_OPERATOR_ACCOUNT_ID must be an account UUID")
	}
	config.OperatorAccountID = strings.ToLower(config.OperatorAccountID)
	origin, err := url.Parse(config.Origin)
	if err != nil || origin.Host == "" || origin.User != nil || origin.Path != "" ||
		origin.RawQuery != "" || origin.Fragment != "" {
		return nil, errors.New("SEFIR_WEB_ORIGIN must be an origin without a path")
	}
	local := origin.Hostname() == "localhost" || origin.Hostname() == "127.0.0.1" || origin.Hostname() == "::1"
	if origin.Scheme != "https" && !(origin.Scheme == "http" && local) {
		return nil, errors.New("HTTPS is required outside localhost")
	}
	owner, err := loadOwner(config)
	if err != nil {
		return nil, err
	}
	if config.Accounts == nil {
		return nil, errors.New("account storage is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := config.Accounts.ImportLegacy(ctx, owner); err != nil {
		return nil, errors.New("could not import the existing account; apply database migrations and check account conflicts")
	}
	config.Password = ""
	return &Server{
		config: config, origin: origin, store: store, bridge: bridge, gmail: gmail, accounts: config.Accounts,
		sessions:       make(map[string]session),
		loginRate:      rate.NewLimiter(rate.Every(10*time.Second), 5),
		generating:     make(chan struct{}, 1),
		googleAttempts: make(map[string]googleAttempt),
		modelCatalogs:  map[string]*aiModelCache{"cohere": {}, "anthropic": {}, "openai": {}, "openrouter": {}},
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/session", s.handleSession)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/setup", s.handleSetup)
	mux.HandleFunc("POST /api/signup", s.handleSetup)
	mux.HandleFunc("POST /api/auth/google", s.handleGoogleStart)
	mux.HandleFunc("GET /oauth/google/sign-in/callback", s.handleGoogleSignInCallback)
	mux.HandleFunc("GET /api/settings/account", s.protect(s.handleAccount))
	mux.HandleFunc("GET /api/settings/ai", s.protect(s.handleAIProviders))
	mux.HandleFunc("GET /api/settings/ai/{provider}/models", s.protect(s.handleAIModels))
	mux.HandleFunc("POST /api/settings/ai/{provider}/models", s.protect(s.handleAIModels))
	mux.HandleFunc("PUT /api/settings/ai/default", s.protect(s.handleAIDefault))
	mux.HandleFunc("PUT /api/settings/ai/{provider}", s.protect(s.handleAISave))
	mux.HandleFunc("DELETE /api/settings/ai/{provider}", s.protect(s.handleAIRemove))
	mux.HandleFunc("POST /api/settings/account/google", s.protect(s.handleGoogleLink))
	mux.HandleFunc("POST /api/logout", s.protect(s.handleLogout))
	mux.HandleFunc("GET /api/drafts", s.protect(s.handleDrafts))
	mux.HandleFunc("POST /api/drafts", s.protect(s.handleGenerate))
	mux.HandleFunc("POST /api/drafts/manual", s.protect(s.handleCompose))
	mux.HandleFunc("POST /api/drafts/{id}/versions/{version}/edit", s.protect(s.handleEditDraft))
	mux.HandleFunc("GET /api/drafts/{id}/versions/{version}", s.protect(s.handleDraft))
	mux.HandleFunc("POST /api/drafts/{id}/versions/{version}/approve", s.protect(s.handleApprove))
	mux.HandleFunc("GET /api/drafts/{id}/versions/{version}/status", s.protect(s.handleStatus))
	mux.HandleFunc("GET /api/contacts", s.protect(s.handleContacts))
	mux.HandleFunc("POST /api/contacts", s.protect(s.handleSaveContact))
	mux.HandleFunc("PUT /api/contacts/{id}", s.protect(s.handleSaveContact))
	mux.HandleFunc("GET /api/activity", s.protect(s.handleActivity))
	mux.HandleFunc("GET /api/settings/gmail", s.protect(s.handleGmailStatus))
	mux.HandleFunc("POST /api/settings/gmail/connect", s.protect(s.handleGmailConnect))
	mux.HandleFunc("GET /oauth/google/callback", s.protect(s.handleGmailCallback))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		fail(w, http.StatusNotFound, "Endpoint not found")
	})
	files := http.FileServer(http.Dir(s.config.Dist))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			fail(w, http.StatusMethodNotAllowed, "Method not allowed")
			return
		}
		files.ServeHTTP(w, r)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; font-src 'self'; img-src 'self' data:; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/oauth/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		if !strings.EqualFold(r.Host, s.origin.Host) {
			fail(w, http.StatusForbidden, "Unexpected request host")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 150*time.Second)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(ctx))
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

func fail(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func decode(w http.ResponseWriter, r *http.Request, value any) bool {
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		fail(w, http.StatusUnsupportedMediaType, "Send application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		fail(w, http.StatusBadRequest, "Invalid request body")
		return false
	}
	if decoder.Decode(new(any)) != io.EOF {
		fail(w, http.StatusBadRequest, "Expected one JSON object")
		return false
	}
	return true
}

func storeError(w http.ResponseWriter, err error) {
	if errors.Is(err, pgx.ErrNoRows) {
		fail(w, http.StatusNotFound, "Record not found")
		return
	}
	fail(w, http.StatusServiceUnavailable, "Database operation failed. Please try again.")
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func draftKey(w http.ResponseWriter, r *http.Request) (string, int, bool) {
	id := strings.ToLower(r.PathValue("id"))
	version, err := strconv.Atoi(r.PathValue("version"))
	if !uuidPattern.MatchString(id) || err != nil || version < 1 || version > 2147483647 {
		fail(w, http.StatusBadRequest, "A draft UUID and positive version are required")
		return "", 0, false
	}
	return id, version, true
}

func pagination(w http.ResponseWriter, r *http.Request) (int, int, bool) {
	limit, offset := 25, 0
	var err error
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil {
			limit = 0
		}
	}
	if raw := r.URL.Query().Get("offset"); raw != "" {
		offset, err = strconv.Atoi(raw)
		if err != nil {
			offset = -1
		}
	}
	if limit < 1 || limit > 100 || offset < 0 || offset > 1000000 {
		fail(w, http.StatusBadRequest, "Invalid pagination")
		return 0, 0, false
	}
	return limit, offset, true
}

func page[T any](w http.ResponseWriter, items []T, limit, offset int) {
	more := len(items) > limit
	if more {
		items = items[:limit]
	}
	if items == nil {
		items = []T{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items": items, "has_more": more, "offset": offset, "next_offset": offset + len(items),
	})
}
