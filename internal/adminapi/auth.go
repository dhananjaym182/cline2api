// Package adminapi contains reusable administration primitives shared by the
// root HTTP wiring. It deliberately avoids dependencies on pools or transport
// state; callers inject those through small callbacks.
package adminapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

const (
	// SessionCookieName is the cookie carrying an administrator session token.
	SessionCookieName = "cline_admin_session"
	// SessionTTL is the lifetime of an administrator session.
	SessionTTL = 24 * time.Hour
)

// SessionStore is an in-memory administrator session table. Sessions do not
// survive process restarts.
type SessionStore struct {
	mu       sync.Mutex
	sessions map[string]time.Time
}

// NewSessionStore returns an empty session store.
func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[string]time.Time)}
}

// Add records a session token that expires at the given time.
func (s *SessionStore) Add(token string, expiresAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions == nil {
		s.sessions = make(map[string]time.Time)
	}
	s.sessions[token] = expiresAt
}

// Valid reports whether the token is known and unexpired. Expired entries are
// removed as they are encountered.
func (s *SessionStore) Valid(token string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	expiry, ok := s.sessions[token]
	if !ok {
		return false
	}
	if now.Before(expiry) {
		return true
	}
	delete(s.sessions, token)
	return false
}

// Remove deletes a single session token.
func (s *SessionStore) Remove(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

// Clear drops every session, forcing all administrators to log in again.
func (s *SessionStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = make(map[string]time.Time)
}

// Auth implements administrator authentication. PasswordSet and Unauthorized
// are injected by the composition layer so this package stays free of pool and
// i18n dependencies.
type Auth struct {
	Sessions     *SessionStore
	PasswordSet  func() bool
	Unauthorized func(w http.ResponseWriter, r *http.Request, reason string)
}

// Middleware passes requests through when no password is configured; otherwise
// it requires a valid session cookie. The reason passed to Unauthorized is
// either "login_required" (no cookie) or "session_expired".
func (a *Auth) Middleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if a.PasswordSet == nil || !a.PasswordSet() {
			next(w, r)
			return
		}
		c, err := r.Cookie(SessionCookieName)
		if err != nil {
			a.unauthorized(w, r, "login_required")
			return
		}
		if a.Sessions != nil && a.Sessions.Valid(c.Value, time.Now()) {
			next(w, r)
			return
		}
		a.unauthorized(w, r, "session_expired")
	}
}

func (a *Auth) unauthorized(w http.ResponseWriter, r *http.Request, reason string) {
	if a.Unauthorized == nil {
		http.Error(w, reason, http.StatusUnauthorized)
		return
	}
	a.Unauthorized(w, r, reason)
}

// SetSessionCookie writes the administrator session cookie for a token.
func SetSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/admin",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(SessionTTL.Seconds()),
	})
}

// ClearSessionCookie expires the administrator session cookie.
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: SessionCookieName, Value: "", Path: "/admin", MaxAge: -1})
}

// HashPassword derives the stored password hash: hex(sha256(saltHex+password)).
func HashPassword(saltHex, password string) string {
	sum := sha256.Sum256([]byte(saltHex + password))
	return hex.EncodeToString(sum[:])
}

// VerifyPassword reports whether password matches the stored salt and hash.
// An empty stored hash (no password configured) never matches.
func VerifyPassword(saltHex, storedHash, password string) bool {
	if storedHash == "" {
		return false
	}
	return HashPassword(saltHex, password) == storedHash
}

// NewSalt returns a hex-encoded 16-byte random salt.
func NewSalt() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// RandomHex returns n random bytes hex-encoded, or "" if the system source
// fails.
func RandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}
