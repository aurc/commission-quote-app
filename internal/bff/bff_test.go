package bff_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aurc/commission-quote-app/internal/bff"
	"github.com/aurc/commission-quote-app/internal/platform/logging"
	"github.com/aurc/commission-quote-app/internal/platform/staffdir"
)

// The committed fixture's development password, stated in config/credentials.csv.
const devPassword = "demo-password"

const signingKey = "test-signing-key-0123456789abcdef"

type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *logBuffer) Contains(s string) bool { return strings.Contains(b.String(), s) }

func fixtures(t *testing.T) (*staffdir.Directory, bff.AuthProvider) {
	t.Helper()
	dir, err := staffdir.Load(filepath.Join("..", "..", "config", "staff.csv"))
	if err != nil {
		t.Fatalf("staff fixture must load: %v", err)
	}
	auth, err := bff.NewFixtureAuth(dir, filepath.Join("..", "..", "config", "credentials.csv"))
	if err != nil {
		t.Fatalf("credentials fixture must load: %v", err)
	}
	return dir, auth
}

// entitledStaff returns an id the fixture grants the quote scope to.
func entitledStaff(t *testing.T) string {
	t.Helper()
	dir, _ := fixtures(t)
	for _, s := range dir.All() {
		if len(s.Scopes) > 0 {
			return s.ID
		}
	}
	t.Fatal("the fixture needs an entitled staff member")
	return ""
}

type stack struct {
	http.Handler
	middleware *httptest.Server
	logs       *logBuffer
	seen       []*http.Request
	mu         sync.Mutex
}

// newStack wires the BFF against a fake Middleware.
func newStack(t *testing.T, middleware http.HandlerFunc) *stack {
	t.Helper()
	s := &stack{logs: &logBuffer{}}

	s.middleware = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.seen = append(s.seen, r.Clone(r.Context()))
		s.mu.Unlock()
		middleware(w, r)
	}))
	t.Cleanup(s.middleware.Close)

	dir, auth := fixtures(t)
	_ = dir

	cfg := bff.Config{
		MiddlewareBaseURL: s.middleware.URL,
		SigningKey:        signingKey,
		SessionTTL:        30 * time.Minute,
		TokenTTL:          time.Minute,
		RequestTimeout:    5 * time.Second,
		CookieSecure:      false,
	}
	log := logging.New(logging.Options{Component: "bff", Output: s.logs})
	client := bff.NewMiddlewareClient(cfg.MiddlewareBaseURL, cfg.SigningKey, cfg.TokenTTL, cfg.RequestTimeout, log)

	s.Handler = bff.NewRouter(cfg, auth, bff.NewSessionStore(cfg.SessionTTL), client, log)
	return s
}

func (s *stack) middlewareCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.seen)
}

func (s *stack) lastAuthHeader() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.seen) == 0 {
		return ""
	}
	return s.seen[len(s.seen)-1].Header.Get("Authorization")
}

func quoteOK(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"quoteId":"q-1","commissionRate":0.0180,"totalCommission":4500.00}`))
}

func post(t *testing.T, h http.Handler, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// signIn returns the session cookie.
func signIn(t *testing.T, h http.Handler, staffID, password string) *http.Cookie {
	t.Helper()
	rec := post(t, h, "/api/session", `{"staffId":"`+staffID+`","password":"`+password+`"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("sign in failed: %d %s", rec.Code, rec.Body)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == bff.SessionCookie {
			return c
		}
	}
	t.Fatal("sign in did not set a session cookie")
	return nil
}

func TestSignInWithTheCorrectPassword(t *testing.T) {
	s := newStack(t, quoteOK)
	id := entitledStaff(t)

	rec := post(t, s, "/api/session", `{"staffId":"`+id+`","password":"`+devPassword+`"}`, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}

	var body struct {
		StaffID string `json:"staffId"`
		Name    string `json:"name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.StaffID != id || body.Name == "" {
		t.Errorf("body = %+v", body)
	}

	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == bff.SessionCookie {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no session cookie was set")
	}
	if !cookie.HttpOnly {
		t.Error("the session cookie must be HttpOnly, or script can read it")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Error("the session cookie must be SameSite=Lax, which is the CSRF control")
	}
	if len(cookie.Value) < 40 {
		t.Errorf("the session value is the credential and must be long and random, got %d characters", len(cookie.Value))
	}
}

// Telling a wrong password apart from an unknown staff member tells an attacker
// who exists.
func TestSignInFailuresAreIndistinguishable(t *testing.T) {
	s := newStack(t, quoteOK)
	id := entitledStaff(t)

	attempts := map[string]string{
		"wrong password":   `{"staffId":"` + id + `","password":"not-the-password"}`,
		"unknown staff":    `{"staffId":"nobody-at-all","password":"` + devPassword + `"}`,
		"empty password":   `{"staffId":"` + id + `","password":""}`,
		"empty staff id":   `{"staffId":"","password":"` + devPassword + `"}`,
		"missing fields":   `{}`,
		"malformed body":   `{`,
		"password as null": `{"staffId":"` + id + `","password":null}`,
	}

	// correlationId is unique per request by design, so the comparison is over
	// everything a caller could use to tell the failures apart.
	type shape struct {
		status  int
		code    string
		message string
		details int
	}
	var first *shape

	for name, body := range attempts {
		t.Run(name, func(t *testing.T) {
			rec := post(t, s, "/api/session", body, nil)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401: %s", rec.Code, rec.Body)
			}
			for _, c := range rec.Result().Cookies() {
				if c.Name == bff.SessionCookie && c.Value != "" {
					t.Error("a failed sign in must not set a session")
				}
			}

			var env struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
					Details []any  `json:"details"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("not the error envelope: %v\n%s", err, rec.Body)
			}
			got := shape{rec.Code, env.Error.Code, env.Error.Message, len(env.Error.Details)}

			if first == nil {
				first = &got
				return
			}
			if got != *first {
				t.Errorf("failure kinds are distinguishable:\n got  %+v\n want %+v", got, *first)
			}
		})
	}
}

func TestPasswordIsNeverLogged(t *testing.T) {
	s := newStack(t, quoteOK)

	post(t, s, "/api/session", `{"staffId":"`+entitledStaff(t)+`","password":"`+devPassword+`"}`, nil)
	post(t, s, "/api/session", `{"staffId":"someone","password":"a-secret-password"}`, nil)

	if s.logs.Contains(devPassword) || s.logs.Contains("a-secret-password") {
		t.Error("a password reached the logs")
	}
}

func TestSessionLifecycle(t *testing.T) {
	s := newStack(t, quoteOK)
	id := entitledStaff(t)
	cookie := signIn(t, s, id, devPassword)

	// Signed in.
	req := httptest.NewRequest(http.MethodGet, "/api/session", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/session = %d, want 200", rec.Code)
	}

	// Sign out.
	req = httptest.NewRequest(http.MethodDelete, "/api/session", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /api/session = %d, want 204", rec.Code)
	}

	// The same cookie must no longer work. Clearing the browser's copy is not
	// enough: a copied cookie would still be a valid credential.
	req = httptest.NewRequest(http.MethodGet, "/api/session", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("a signed out cookie still worked: %d", rec.Code)
	}
}

func TestQuoteRequiresASession(t *testing.T) {
	s := newStack(t, quoteOK)

	rec := post(t, s, "/api/v1/quotes", `{"loanAmount":250000.00,"loanTermInMonths":240,"riskBand":"B"}`, nil)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if s.middlewareCalls() != 0 {
		t.Error("an unauthenticated request reached the Middleware")
	}
}

func TestQuoteExchangesTheSessionForABearer(t *testing.T) {
	s := newStack(t, quoteOK)
	cookie := signIn(t, s, entitledStaff(t), devPassword)

	rec := post(t, s, "/api/v1/quotes", `{"loanAmount":250000.00,"loanTermInMonths":240,"riskBand":"B"}`, cookie)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if auth := s.lastAuthHeader(); !strings.HasPrefix(auth, "Bearer ") {
		t.Errorf("the Middleware was called with Authorization %q", auth)
	}
	// The session cookie is a browser concern and must not travel inward.
	if s.lastAuthHeader() == cookie.Value {
		t.Error("the session value was sent as the bearer")
	}
}

func newGet(path string, cookie *http.Cookie) *http.Request {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	return req
}

func recorder() *httptest.ResponseRecorder { return httptest.NewRecorder() }
