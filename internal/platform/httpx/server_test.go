package httpx_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aurc/commission-quote-app/internal/platform/httpx"
	"github.com/aurc/commission-quote-app/internal/platform/logging"
)

// A service that logs "listening" and then fails to bind reads as running, and
// sends an operator looking in the wrong place.
func TestServeDoesNotAnnounceListeningWhenThePortIsTaken(t *testing.T) {
	// Bind the same wildcard address Serve uses. Holding 127.0.0.1 only would
	// not necessarily conflict with a wildcard bind, and the test would hang
	// waiting on a server that started successfully.
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	var logs strings.Builder
	log := logging.New(logging.Options{Component: "test", Output: &logs})

	// Guard against the failure mode this test exists to catch: if Serve binds
	// anyway, it blocks forever rather than failing.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err = httpx.Serve(ctx, log, http.NotFoundHandler(), httpx.ServeOptions{Port: port})

	if err == nil {
		t.Fatal("Serve must fail when the port is already bound")
	}
	if strings.Contains(logs.String(), "listening") {
		t.Errorf("Serve announced listening despite failing to bind: %s", logs.String())
	}
}

func TestServeShutsDownGracefully(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	log := logging.New(logging.Options{Component: "test", Output: io.Discard})

	done := make(chan error, 1)
	go func() {
		done <- httpx.Serve(ctx, log, http.NotFoundHandler(), httpx.ServeOptions{
			Port:          0,
			ShutdownGrace: time.Second,
		})
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("clean shutdown returned %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return after the context was cancelled")
	}
}
