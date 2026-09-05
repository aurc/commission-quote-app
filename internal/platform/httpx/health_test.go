package httpx_test

import (
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/aurc/commission-quote-app/internal/platform/httpx"
)

func servePort(t *testing.T, handler http.Handler) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().(*net.TCPAddr).Port
}

func TestCheckHealthPassesWhenTheServiceAnswers(t *testing.T) {
	port := servePort(t, httpx.Health())

	if err := httpx.CheckHealth(port, time.Second); err != nil {
		t.Errorf("expected a healthy service to pass: %v", err)
	}
}

func TestCheckHealthFailsWhenNothingIsListening(t *testing.T) {
	// Bind and release, so the port is almost certainly free.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	if err := httpx.CheckHealth(port, 500*time.Millisecond); err == nil {
		t.Error("expected a failure when nothing is listening")
	}
}

// A process that is up but answering errors is not healthy, and a check that
// only tested reachability would report it as such.
func TestCheckHealthFailsOnANonOKStatus(t *testing.T) {
	port := servePort(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))

	if err := httpx.CheckHealth(port, time.Second); err == nil {
		t.Error("expected a failure when the service answers with 503")
	}
}
