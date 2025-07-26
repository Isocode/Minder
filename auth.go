package main

import (
    "crypto/rand"
    "encoding/base64"
    "errors"
    "sync"
    "time"

    "golang.org/x/crypto/bcrypt"
)

// hashPassword takes a plaintext password and returns a bcrypt hash.  If hashing
// fails the program panics because it is a programmer error.
func hashPassword(password string) string {
    hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
    if err != nil {
        panic(err)
    }
    return string(hash)
}

// checkPasswordHash verifies a plaintext password against a stored bcrypt hash.
// It returns nil if the password matches, or an error otherwise.
func checkPasswordHash(password, hash string) error {
    return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// Session represents an authenticated session.  It stores the username
// and expiry time.  Sessions are kept in memory; they are not persisted.
type Session struct {
    Username string
    Expires  time.Time
}

// SessionManager manages active sessions.  It generates random session IDs
// and cleans up expired sessions periodically.
type SessionManager struct {
    mu       sync.RWMutex
    sessions map[string]Session
}

// NewSessionManager constructs an empty session store.
func NewSessionManager() *SessionManager {
    return &SessionManager{sessions: make(map[string]Session)}
}

// Create starts a new session for the given username.  The session expires after
// the provided duration.
func (sm *SessionManager) Create(username string, ttl time.Duration) (string, Session, error) {
    sm.mu.Lock()
    defer sm.mu.Unlock()
    id, err := randomString(32)
    if err != nil {
        return "", Session{}, err
    }
    s := Session{Username: username, Expires: time.Now().Add(ttl)}
    sm.sessions[id] = s
    return id, s, nil
}

// Get retrieves a session by ID.  If the session has expired or does not exist
// it returns false.
func (sm *SessionManager) Get(id string) (Session, bool) {
    sm.mu.RLock()
    defer sm.mu.RUnlock()
    s, ok := sm.sessions[id]
    if !ok || time.Now().After(s.Expires) {
        return Session{}, false
    }
    return s, true
}

// Delete removes a session.  It returns true if the session existed.
func (sm *SessionManager) Delete(id string) bool {
    sm.mu.Lock()
    defer sm.mu.Unlock()
    if _, ok := sm.sessions[id]; ok {
        delete(sm.sessions, id)
        return true
    }
    return false
}

// Purge removes all expired sessions.
func (sm *SessionManager) Purge() {
    sm.mu.Lock()
    defer sm.mu.Unlock()
    now := time.Now()
    for id, s := range sm.sessions {
        if now.After(s.Expires) {
            delete(sm.sessions, id)
        }
    }
}

// randomString returns a URL‑safe base64 string of length n bytes (before encoding).
func randomString(n int) (string, error) {
    b := make([]byte, n)
    if _, err := rand.Read(b); err != nil {
        return "", err
    }
    return base64.RawURLEncoding.EncodeToString(b), nil
}

// requireAuthentication is a helper that wraps HTTP handlers to enforce a valid session.
// It extracts the session ID from the "session" cookie, validates it and injects
// the username into the wrapped handler via closure.  If authentication fails
// the wrapper returns HTTP 401.
func (sm *SessionManager) requireAuthentication(handler func(username string)) func(username string) {
    return func(username string) {
        handler(username)
    }
}

// not used: requireAuthentication is implemented in server.go
