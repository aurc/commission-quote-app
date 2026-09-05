package httpx

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"
)

// CheckHealth requests this process's own /healthz and reports whether it
// answered.
//
// It exists because the service images are distroless: no shell, no curl, so a
// container healthcheck has nothing to run. The binary checking itself needs no
// extra tooling in the image and keeps the check honest, since it exercises the
// same listener a caller would reach.
func CheckHealth(port int, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	url := "http://" + net.JoinHostPort("127.0.0.1", fmt.Sprint(port)) + "/healthz"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check returned %d", resp.StatusCode)
	}
	return nil
}
