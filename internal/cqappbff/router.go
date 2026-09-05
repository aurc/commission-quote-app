package cqappbff

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/aurc/commission-quote-app/internal/platform/httpx"
	"github.com/aurc/commission-quote-app/internal/platform/telemetry"
)

const component = "cqapp-bff"

// Handler serves the browser facing API.
type Handler struct {
	auth     AuthProvider
	sessions *SessionStore
	quotes   *MiddlewareClient
	cookie   CookieOptions
	log      *slog.Logger
}

// NewHandler builds a Handler.
func NewHandler(auth AuthProvider, sessions *SessionStore, quotes *MiddlewareClient, cookie CookieOptions, log *slog.Logger) *Handler {
	return &Handler{auth: auth, sessions: sessions, quotes: quotes, cookie: cookie, log: log}
}

type signInRequest struct {
	StaffID  string `json:"staffId"`
	Password string `json:"password"`
}

type staffResponse struct {
	StaffID string `json:"staffId"`
	Name    string `json:"name"`
}

// SignIn authenticates a staff member and starts a session.
func (h *Handler) SignIn(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req signInRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxBody)).Decode(&req); err != nil {
		// Same answer as bad credentials: a malformed body should not be a
		// separate signal either.
		h.refuse(ctx, w, "", err)
		return
	}

	staff, err := h.auth.Authenticate(ctx, req.StaffID, req.Password)
	if err != nil {
		h.refuse(ctx, w, req.StaffID, err)
		return
	}

	session, err := h.sessions.Create(staff)
	if err != nil {
		httpx.WriteError(ctx, w, h.log, httpx.Internal(err))
		return
	}

	h.log.InfoContext(ctx, "staff signed in", slog.String("staffId", staff.ID))
	setSessionCookie(w, session.ID, h.cookie)
	httpx.WriteJSON(ctx, w, h.log, http.StatusOK, staffResponse{StaffID: staff.ID, Name: staff.Name})
}

// refuse answers every sign in failure identically. The password is never
// logged; the attempted id is, because a failed sign in is worth investigating
// and the id is not a secret.
func (h *Handler) refuse(ctx context.Context, w http.ResponseWriter, attemptedID string, cause error) {
	h.log.WarnContext(ctx, "sign in refused",
		slog.String("attemptedStaffId", attemptedID),
		slog.String("cause", cause.Error()))

	e := httpx.Unauthenticated(errors.New("invalid credentials"))
	e.Message = "That staff ID or password is not correct."
	httpx.WriteError(ctx, w, nil, e)
}

// CurrentSession returns the signed in staff member.
func (h *Handler) CurrentSession(w http.ResponseWriter, r *http.Request) {
	session, ok := h.session(r)
	if !ok {
		h.unauthenticated(r.Context(), w)
		return
	}
	httpx.WriteJSON(r.Context(), w, h.log, http.StatusOK,
		staffResponse{StaffID: session.Staff.ID, Name: session.Staff.Name})
}

// SignOut invalidates the session server side as well as clearing the cookie.
// Clearing the cookie alone would leave a copied cookie working.
func (h *Handler) SignOut(w http.ResponseWriter, r *http.Request) {
	if session, ok := h.session(r); ok {
		h.sessions.Delete(session.ID)
		h.log.InfoContext(r.Context(), "staff signed out", slog.String("staffId", session.Staff.ID))
	}
	clearSessionCookie(w, h.cookie)
	w.WriteHeader(http.StatusNoContent)
}

// Quote forwards a quote request for the signed in staff member.
func (h *Handler) Quote(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	session, ok := h.session(r)
	if !ok {
		h.unauthenticated(ctx, w)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		httpx.WriteError(ctx, w, h.log, httpx.Malformed(err))
		return
	}

	// The body is forwarded unvalidated. Validation is authoritative in the
	// Middleware, and a second implementation here would be one more thing to
	// keep in step for no gain.
	status, out := h.quotes.Quote(ctx, session.Staff.ID, body)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(out); err != nil {
		h.log.ErrorContext(ctx, "failed to write response", slog.String("cause", err.Error()))
	}
}

func (h *Handler) session(r *http.Request) (Session, bool) {
	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return Session{}, false
	}
	return h.sessions.Get(c.Value)
}

func (h *Handler) unauthenticated(ctx context.Context, w http.ResponseWriter) {
	e := httpx.Unauthenticated(errors.New("no valid session"))
	e.Message = userMessage(httpx.CodeUnauthenticated)
	httpx.WriteError(ctx, w, nil, e)
}

// NewRouter wires the BFF.
func NewRouter(cfg Config, auth AuthProvider, sessions *SessionStore, quotes *MiddlewareClient, log *slog.Logger) http.Handler {
	h := NewHandler(auth, sessions, quotes, CookieOptions{Secure: cfg.CookieSecure, TTL: cfg.SessionTTL}, log)

	mux := http.NewServeMux()
	mux.Handle("GET /healthz", httpx.Health())
	mux.HandleFunc("POST /api/session", h.SignIn)
	mux.HandleFunc("GET /api/session", h.CurrentSession)
	mux.HandleFunc("DELETE /api/session", h.SignOut)
	mux.HandleFunc("POST /api/v1/quotes", h.Quote)

	return httpx.Chain(mux,
		telemetry.Middleware(component),
		httpx.Correlation(),
		httpx.RequestLogger(log),
		httpx.Recoverer(log),
		httpx.Timeout(cfg.RequestTimeout),
	)
}
