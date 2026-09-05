package bff

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/aurc/commission-quote-app/internal/platform/staffdir"
)

// SessionCookie is the browser's only credential after signing in.
const SessionCookie = "cq_session"

// Session is a signed in staff member.
type Session struct {
	ID        string
	Staff     staffdir.Staff
	ExpiresAt time.Time
}

// SessionStore holds sessions in memory.
//
// A restart signs everyone out and a second replica would not recognise the
// first's sessions, so the MVP runs one. That is a property of the deployment
// rather than an oversight, and assumptions.md 1.5 records it next to the
// statelessness claim it qualifies. Production is a distributed store.
type SessionStore struct {
	mu   sync.Mutex
	byID map[string]Session
	ttl  time.Duration
	now  func() time.Time
}

// NewSessionStore returns an empty store.
func NewSessionStore(ttl time.Duration) *SessionStore {
	return &SessionStore{byID: make(map[string]Session), ttl: ttl, now: time.Now}
}

// Create issues a session for a staff member.
func (s *SessionStore) Create(staff staffdir.Staff) (Session, error) {
	id, err := newSessionID()
	if err != nil {
		return Session{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	sess := Session{ID: id, Staff: staff, ExpiresAt: s.now().Add(s.ttl)}
	s.byID[id] = sess
	return sess, nil
}

// Get returns a live session. An expired one is removed rather than returned,
// so a stale cookie behaves exactly like an unknown one.
func (s *SessionStore) Get(id string) (Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.byID[id]
	if !ok {
		return Session{}, false
	}
	if !s.now().Before(sess.ExpiresAt) {
		delete(s.byID, id)
		return Session{}, false
	}
	return sess, true
}

// Delete signs a session out. Sign out must invalidate the session server side,
// not merely clear the cookie: a copy of the cookie would otherwise still work.
func (s *SessionStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byID, id)
}

// Count reports live sessions, for tests and diagnostics.
func (s *SessionStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.byID)
}

// newSessionID returns 256 bits of randomness. The value is the credential, so
// it is generated the way a credential should be and never derived from
// anything about the user.
func newSessionID() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// CookieOptions configures the session cookie.
type CookieOptions struct {
	// Secure is configurable because a Secure cookie over http://localhost is
	// honoured by some browsers and not others, and a reviewer who cannot sign
	// in because of their browser is worse than a documented flag.
	Secure bool
	TTL    time.Duration
}

// setSessionCookie writes the session cookie.
//
// HttpOnly so script cannot read it, SameSite=Lax so it is withheld from cross
// site POSTs, which is the CSRF control for the only state changing endpoint.
func setSessionCookie(w http.ResponseWriter, id string, opts CookieOptions) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		Secure:   opts.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(opts.TTL.Seconds()),
	})
}

// clearSessionCookie expires the cookie in the browser.
func clearSessionCookie(w http.ResponseWriter, opts CookieOptions) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   opts.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}
