package httpx

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/aurc/commission-quote-app/internal/platform/logging"
)

// CorrelationHeader carries the correlation id across every hop, per
// contract.md section 8.
const CorrelationHeader = "X-Correlation-Id"

const maxCorrelationLen = 64

// Middleware decorates a handler.
type Middleware func(http.Handler) http.Handler

// Chain applies middleware so the first listed is the outermost, which reads in
// the order requests actually traverse them.
func Chain(h http.Handler, mw ...Middleware) http.Handler {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

// Correlation establishes the correlation id for the request and echoes it on the
// response so a user can quote it in a support call.
//
// Precedence: a valid inbound header, then the active trace id so logs and traces
// join on one value, then a fresh random id.
func Correlation() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			id := sanitiseCorrelationID(r.Header.Get(CorrelationHeader))
			if id == "" {
				if traceID, _ := logging.TraceIDs(ctx); traceID != "" {
					id = traceID
				}
			}
			if id == "" {
				id = newID()
			}

			ctx = logging.WithCorrelationID(ctx, id)
			w.Header().Set(CorrelationHeader, id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// sanitiseCorrelationID accepts only an id we are willing to write into logs and
// response headers. An inbound value is attacker controlled, so anything outside
// a conservative alphabet, or over the length limit, is discarded rather than
// trimmed: a truncated id would silently break the join across services.
func sanitiseCorrelationID(s string) string {
	if s == "" || len(s) > maxCorrelationLen {
		return ""
	}
	for _, c := range []byte(s) {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '-', c == '_', c == '.':
		default:
			return ""
		}
	}
	return s
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail in practice; degrade to a time based id
		// rather than dropping correlation entirely.
		return hex.EncodeToString([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
	}
	return hex.EncodeToString(b[:])
}

// RequestLogger records one line per completed request.
func RequestLogger(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &recorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rec, r)

			log.InfoContext(r.Context(), "request completed",
				slog.String("httpMethod", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Int64("durationMs", time.Since(start).Milliseconds()),
			)
		})
	}
}

// Recoverer turns a panic into a logged internal error rather than a dropped
// connection. The stack goes to the log; the caller sees only the safe message.
func Recoverer(log *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				if v == http.ErrAbortHandler {
					panic(v)
				}
				log.ErrorContext(r.Context(), "panic recovered",
					slog.Any("panic", v),
					slog.String("stack", string(debug.Stack())),
				)
				WriteError(r.Context(), w, nil, Internal(nil))
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// Timeout bounds how long a handler may run, per the budgets in contract.md
// section 6.
func Timeout(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// recorder captures the status code for the request log.
type recorder struct {
	http.ResponseWriter
	status  int
	written bool
}

func (r *recorder) WriteHeader(status int) {
	if r.written {
		return
	}
	r.written = true
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *recorder) Write(b []byte) (int, error) {
	r.written = true
	return r.ResponseWriter.Write(b)
}
