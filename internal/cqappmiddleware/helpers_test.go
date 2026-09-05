package cqappmiddleware_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/aurc/commission-quote-app/internal/cqappmiddleware"
	"github.com/aurc/commission-quote-app/internal/platform/logging"
	"github.com/aurc/commission-quote-app/internal/platform/staffdir"
)

const validRequest = `{"loanAmount":250000.00,"loanTermInMonths":240,"riskBand":"B"}`

// fixturePath is the committed staff file. Tests authorise against the real
// implementation over the real fixture, so no staff identifier is written into
// Go code and a fixture edit cannot leave these tests passing against something
// the service no longer does.
func fixturePath() string { return filepath.Join("..", "..", "config", "staff.csv") }

func staffFixture(t *testing.T) *staffdir.Directory {
	t.Helper()
	d, err := staffdir.Load(fixturePath())
	if err != nil {
		t.Fatalf("config/staff.csv must load: %v", err)
	}
	return d
}

// entitledSubject returns a staff member the fixture grants the scope to.
func entitledSubject(t *testing.T) string {
	t.Helper()
	for _, s := range staffFixture(t).All() {
		if slices.Contains(s.Scopes, cqappmiddleware.ScopeQuoteGenerate) {
			return s.ID
		}
	}
	t.Fatalf("the fixture needs a staff member holding %s", cqappmiddleware.ScopeQuoteGenerate)
	return ""
}

// unentitledSubject returns an authenticated staff member holding no grant.
// Without one, the 403 path cannot be exercised.
func unentitledSubject(t *testing.T) string {
	t.Helper()
	for _, s := range staffFixture(t).All() {
		if !slices.Contains(s.Scopes, cqappmiddleware.ScopeQuoteGenerate) {
			return s.ID
		}
	}
	t.Fatalf("the fixture needs a staff member without %s, or 403 is untested", cqappmiddleware.ScopeQuoteGenerate)
	return ""
}

// vendorResponse is what a well behaved CQAPI returns for validRequest.
const vendorResponse = `{"quoteId":"7c4677e6-b95b-4ee8-bcf5-c17bbda9d63a","commissionRate":0.0180,"totalCommission":4500.00}`

// testBuffer is a concurrency safe log sink.
type testBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *testBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *testBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *testBuffer) Contains(s string) bool { return strings.Contains(b.String(), s) }

// fakeVendor is a stand in for CQAPI. Tests drive it rather than the real mock,
// so a Middleware test never depends on the vendor's dice or its network.
type fakeVendor struct {
	*httptest.Server
	mu       sync.Mutex
	requests []string
	keys     []string
}

func newFakeVendor(t *testing.T, handler http.HandlerFunc) *fakeVendor {
	t.Helper()
	f := &fakeVendor{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(r.Body)

		f.mu.Lock()
		f.requests = append(f.requests, body.String())
		f.keys = append(f.keys, r.Header.Get("api-key"))
		f.mu.Unlock()

		handler(w, r)
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeVendor) lastRequest() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		return ""
	}
	return f.requests[len(f.requests)-1]
}

func (f *fakeVendor) lastKey() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.keys) == 0 {
		return ""
	}
	return f.keys[len(f.keys)-1]
}

// okVendor returns a vendor that always succeeds.
func okVendor(t *testing.T) *fakeVendor {
	return newFakeVendor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(vendorResponse))
	})
}

// newMiddleware wires a router pointed at a fake vendor, discarding logs.
func newMiddleware(t *testing.T, vendor *fakeVendor) http.Handler {
	t.Helper()
	return newMiddlewareWithLogs(t, vendor, &testBuffer{})
}

func newMiddlewareWithLogs(t *testing.T, vendor *fakeVendor, logs *testBuffer) http.Handler {
	t.Helper()
	cfg := testConfig()
	cfg.VendorBaseURL = vendor.URL

	log := logging.New(logging.Options{Component: "middleware", Output: logs})
	client := cqappmiddleware.NewVendorClient(cfg.VendorBaseURL, cfg.VendorAPIKey, cfg.VendorTimeout, log)
	return cqappmiddleware.NewRouterWith(cfg, staffFixture(t), client, log)
}

// quote posts a body as an entitled caller.
func quote(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/quotes", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+mint(t, tokenOpts{}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// newMiddlewareWithConfig wires a router with an explicit config.
func newMiddlewareWithConfig(t *testing.T, vendor *fakeVendor, cfg cqappmiddleware.Config, logs *testBuffer) http.Handler {
	t.Helper()
	if cfg.VendorBaseURL == "" {
		cfg.VendorBaseURL = vendor.URL
	}
	log := logging.New(logging.Options{Component: "middleware", Output: logs})
	client := cqappmiddleware.NewVendorClient(cfg.VendorBaseURL, cfg.VendorAPIKey, cfg.VendorTimeout, log)
	return cqappmiddleware.NewRouterWith(cfg, staffFixture(t), client, log)
}
