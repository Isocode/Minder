package main

import (
    "crypto/tls"
    "embed"
    "encoding/json"
    "errors"
    "fmt"
    "log"
    "net/http"
    "strconv"
    "strings"
    "time"
)

//go:embed web/*
var embeddedFiles embed.FS

// Server holds global state for the HTTP server and the alarm logic.
type Server struct {
    cfgMgr    *ConfigManager
    sessions  *SessionManager
    currentMode string        // name of currently active arm mode ("Disarmed" if none)
    triggered map[int]bool    // zones that have been triggered since last arm
}

// NewServer constructs a new Server and initialises GPIO.
func NewServer(cfgMgr *ConfigManager) (*Server, error) {
    if err := initGPIO(); err != nil {
        return nil, err
    }
    return &Server{
        cfgMgr:   cfgMgr,
        sessions: NewSessionManager(),
        currentMode: "Disarmed",
        triggered: make(map[int]bool),
    }, nil
}

// Start launches the HTTPS server.  It blocks until the server shuts down.
func (s *Server) Start() error {
    cfg := s.cfgMgr.Get()
    addr := fmt.Sprintf(":%d", cfg.HTTPPort)

    mux := http.NewServeMux()
    
    // API routes
    mux.HandleFunc("/api/login", s.handleLogin)
    mux.HandleFunc("/api/logout", s.handleLogout)
    mux.HandleFunc("/api/status", s.withAuth(s.handleStatus))
    mux.HandleFunc("/api/arm", s.withAuth(s.handleArm))
    mux.HandleFunc("/api/disarm", s.withAuth(s.handleDisarm))
    mux.HandleFunc("/api/zones", s.withAuth(s.handleZones))
    mux.HandleFunc("/api/zones/", s.withAuth(s.handleZoneByID))
    mux.HandleFunc("/api/users", s.withAuth(s.handleUsers))
    mux.HandleFunc("/api/users/", s.withAuth(s.handleUserByID))
    mux.HandleFunc("/api/arm_modes", s.withAuth(s.handleArmModes))
    
    // Static files
    fs := http.FS(embeddedFiles)
    mux.Handle("/", http.FileServer(fs))

    // TLS configuration: use modern defaults
    tlsConfig := &tls.Config{
        MinVersion: tls.VersionTLS12,
    }
    
    srv := &http.Server{
        Addr:      addr,
        Handler:   mux,
        TLSConfig: tlsConfig,
    }

    log.Printf("Listening on https://0.0.0.0%s\n", addr)
    return srv.ListenAndServeTLS(cfg.CertFile, cfg.KeyFile)
}

// withAuth wraps handlers that require a valid session.  If the request
// contains a valid "session" cookie, it calls the underlying handler with
// the username; otherwise it responds with 401.
func (s *Server) withAuth(handler func(http.ResponseWriter, *http.Request, User)) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        cookie, err := r.Cookie("session")
        if err != nil {
            http.Error(w, "unauthenticated", http.StatusUnauthorized)
            return
        }
        sess, ok := s.sessions.Get(cookie.Value)
        if !ok {
            http.Error(w, "session expired", http.StatusUnauthorized)
            return
        }
        user, _ := s.cfgMgr.FindUser(sess.Username)
        if user.Username == "" {
            http.Error(w, "unknown user", http.StatusUnauthorized)
            return
        }
        handler(w, r, user)
    }
}

// handleLogin authenticates a user and sets a session cookie.  Expected JSON:
// {"username":"...","password":"..."}
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }
    var creds struct {
        Username string `json:"username"`
        Password string `json:"password"`
    }
    if err := json.NewDecoder(r.Body).Decode(&creds); err != nil {
        http.Error(w, "invalid JSON", http.StatusBadRequest)
        return
    }
    user, err := s.cfgMgr.Authenticate(creds.Username, creds.Password)
    if err != nil {
        http.Error(w, "invalid credentials", http.StatusUnauthorized)
        return
    }
    // Create session valid for 24h
    sessID, _, err := s.sessions.Create(user.Username, 24*time.Hour)
    if err != nil {
        http.Error(w, "failed to create session", http.StatusInternalServerError)
        return
    }
    http.SetCookie(w, &http.Cookie{
        Name:     "session",
        Value:    sessID,
        Path:     "/",
        HttpOnly: true,
        Secure:   true,
        SameSite: http.SameSiteStrictMode,
        Expires:  time.Now().Add(24 * time.Hour),
    })
    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleLogout deletes the session cookie.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }
    cookie, err := r.Cookie("session")
    if err == nil {
        s.sessions.Delete(cookie.Value)
    }
    http.SetCookie(w, &http.Cookie{
        Name:     "session",
        Value:    "",
        Path:     "/",
        HttpOnly: true,
        Secure:   true,
        Expires:  time.Unix(0, 0),
    })
    w.WriteHeader(http.StatusNoContent)
}

// handleStatus returns the current arm mode and triggered zones.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request, user User) {
    type status struct {
        Mode     string     `json:"mode"`
        Triggered []int      `json:"triggered"`
        Zones    []ZoneInfo `json:"zones"`
    }
    cfg := s.cfgMgr.Get()
    triggered := []int{}
    for id, active := range s.triggered {
        if active {
            triggered = append(triggered, id)
        }
    }
    zones := make([]ZoneInfo, len(cfg.Zones))
    for i, z := range cfg.Zones {
        zones[i] = ZoneInfo{
            ID:      z.ID,
            Name:    z.Name,
            Type:    z.Type,
            Pin:     z.Pin,
            Enabled: z.Enabled,
            Active:  s.triggered[z.ID],
        }
    }
    resp := status{Mode: s.currentMode, Triggered: triggered, Zones: zones}
    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(resp)
}

// ZoneInfo extends Zone with an Active flag used in status responses.
type ZoneInfo struct {
    ID      int      `json:"id"`
    Name    string   `json:"name"`
    Type    ZoneType `json:"type"`
    Pin     int      `json:"pin"`
    Enabled bool     `json:"enabled"`
    Active  bool     `json:"active"`
}

// handleArm arms the system into a specified mode.  Body JSON: {"mode":"Home"}
func (s *Server) handleArm(w http.ResponseWriter, r *http.Request, user User) {
    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }
    var req struct {
        Mode string `json:"mode"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "invalid JSON", http.StatusBadRequest)
        return
    }
    cfg := s.cfgMgr.Get()
    // Validate mode exists
    var activeZones []int
    for _, am := range cfg.ArmModes {
        if strings.EqualFold(am.Name, req.Mode) {
            activeZones = am.ActiveZones
            break
        }
    }
    if activeZones == nil {
        http.Error(w, "unknown arm mode", http.StatusBadRequest)
        return
    }
    s.currentMode = req.Mode
    // Reset triggered flags
    s.triggered = make(map[int]bool)
    // TODO: when integrating real sensors, start polling only active zones
    log.Printf("System armed in %s mode (active zones: %v)\n", s.currentMode, activeZones)
    w.WriteHeader(http.StatusNoContent)
}

// handleDisarm disarms the system and resets triggered flags.
func (s *Server) handleDisarm(w http.ResponseWriter, r *http.Request, user User) {
    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }
    s.currentMode = "Disarmed"
    s.triggered = make(map[int]bool)
    log.Println("System disarmed")
    w.WriteHeader(http.StatusNoContent)
}

// handleZones handles GET and POST on /api/zones.  GET returns all zones.  POST
// creates a new zone (admin only).
func (s *Server) handleZones(w http.ResponseWriter, r *http.Request, user User) {
    switch r.Method {
    case http.MethodGet:
        cfg := s.cfgMgr.Get()
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(cfg.Zones)
    case http.MethodPost:
        if !user.Admin {
            http.Error(w, "forbidden", http.StatusForbidden)
            return
        }
        var z Zone
        if err := json.NewDecoder(r.Body).Decode(&z); err != nil {
            http.Error(w, "invalid JSON", http.StatusBadRequest)
            return
        }
        if z.Name == "" || z.Pin == 0 {
            http.Error(w, "missing name or pin", http.StatusBadRequest)
            return
        }
        // Assign ID: one greater than max existing ID
        s.cfgMgr.Update(func(c *Config) error {
            maxID := 0
            for _, existing := range c.Zones {
                if existing.ID > maxID {
                    maxID = existing.ID
                }
            }
            z.ID = maxID + 1
            c.Zones = append(c.Zones, z)
            return nil
        })
        w.WriteHeader(http.StatusCreated)
        _ = json.NewEncoder(w).Encode(z)
    default:
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
    }
}

// handleZoneByID handles PUT and DELETE on /api/zones/{id}.
func (s *Server) handleZoneByID(w http.ResponseWriter, r *http.Request, user User) {
    if !user.Admin {
        http.Error(w, "forbidden", http.StatusForbidden)
        return
    }
    parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
    if len(parts) < 3 {
        http.NotFound(w, r)
        return
    }
    idStr := parts[2]
    id, err := strconv.Atoi(idStr)
    if err != nil {
        http.Error(w, "invalid id", http.StatusBadRequest)
        return
    }
    switch r.Method {
    case http.MethodPut:
        var z Zone
        if err := json.NewDecoder(r.Body).Decode(&z); err != nil {
            http.Error(w, "invalid JSON", http.StatusBadRequest)
            return
        }
        err = s.cfgMgr.Update(func(c *Config) error {
            for i, existing := range c.Zones {
                if existing.ID == id {
                    z.ID = id
                    c.Zones[i] = z
                    return nil
                }
            }
            return errors.New("not found")
        })
        if err != nil {
            if err.Error() == "not found" {
                http.Error(w, "not found", http.StatusNotFound)
            } else {
                http.Error(w, "internal error", http.StatusInternalServerError)
            }
            return
        }
        w.WriteHeader(http.StatusNoContent)
    case http.MethodDelete:
        err = s.cfgMgr.Update(func(c *Config) error {
            for i, existing := range c.Zones {
                if existing.ID == id {
                    c.Zones = append(c.Zones[:i], c.Zones[i+1:]...)
                    return nil
                }
            }
            return errors.New("not found")
        })
        if err != nil {
            if err.Error() == "not found" {
                http.Error(w, "not found", http.StatusNotFound)
            } else {
                http.Error(w, "internal error", http.StatusInternalServerError)
            }
            return
        }
        w.WriteHeader(http.StatusNoContent)
    default:
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
    }
}

// handleUsers handles GET and POST on /api/users.  Only admins may manage users.
func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request, user User) {
    if !user.Admin {
        http.Error(w, "forbidden", http.StatusForbidden)
        return
    }
    switch r.Method {
    case http.MethodGet:
        cfg := s.cfgMgr.Get()
        // Do not expose password hashes to clients
        type userView struct {
            Username string `json:"username"`
            Admin    bool   `json:"admin"`
        }
        users := make([]userView, len(cfg.Users))
        for i, u := range cfg.Users {
            users[i] = userView{Username: u.Username, Admin: u.Admin}
        }
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(users)
    case http.MethodPost:
        var req struct {
            Username string `json:"username"`
            Password string `json:"password"`
            Admin    bool   `json:"admin"`
        }
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            http.Error(w, "invalid JSON", http.StatusBadRequest)
            return
        }
        if req.Username == "" || req.Password == "" {
            http.Error(w, "missing username or password", http.StatusBadRequest)
            return
        }
        err := s.cfgMgr.Update(func(c *Config) error {
            // Check for duplicate username
            for _, u := range c.Users {
                if u.Username == req.Username {
                    return errors.New("exists")
                }
            }
            c.Users = append(c.Users, User{Username: req.Username, PasswordHash: hashPassword(req.Password), Admin: req.Admin})
            return nil
        })
        if err != nil {
            if err.Error() == "exists" {
                http.Error(w, "user exists", http.StatusBadRequest)
            } else {
                http.Error(w, "internal error", http.StatusInternalServerError)
            }
            return
        }
        w.WriteHeader(http.StatusCreated)
    default:
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
    }
}

// handleUserByID handles PUT/DELETE on /api/users/{username}.
func (s *Server) handleUserByID(w http.ResponseWriter, r *http.Request, user User) {
    if !user.Admin {
        http.Error(w, "forbidden", http.StatusForbidden)
        return
    }
    parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
    if len(parts) < 3 {
        http.NotFound(w, r)
        return
    }
    username := parts[2]
    switch r.Method {
    case http.MethodPut:
        var req struct {
            Password *string `json:"password,omitempty"`
            Admin    *bool   `json:"admin,omitempty"`
        }
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            http.Error(w, "invalid JSON", http.StatusBadRequest)
            return
        }
        err := s.cfgMgr.Update(func(c *Config) error {
            for i, u := range c.Users {
                if u.Username == username {
                    if req.Password != nil {
                        c.Users[i].PasswordHash = hashPassword(*req.Password)
                    }
                    if req.Admin != nil {
                        c.Users[i].Admin = *req.Admin
                    }
                    return nil
                }
            }
            return errors.New("not found")
        })
        if err != nil {
            if err.Error() == "not found" {
                http.Error(w, "not found", http.StatusNotFound)
            } else {
                http.Error(w, "internal error", http.StatusInternalServerError)
            }
            return
        }
        w.WriteHeader(http.StatusNoContent)
    case http.MethodDelete:
        if username == "admin" {
            http.Error(w, "cannot delete default admin", http.StatusBadRequest)
            return
        }
        err := s.cfgMgr.Update(func(c *Config) error {
            for i, u := range c.Users {
                if u.Username == username {
                    c.Users = append(c.Users[:i], c.Users[i+1:]...)
                    return nil
                }
            }
            return errors.New("not found")
        })
        if err != nil {
            if err.Error() == "not found" {
                http.Error(w, "not found", http.StatusNotFound)
            } else {
                http.Error(w, "internal error", http.StatusInternalServerError)
            }
            return
        }
        w.WriteHeader(http.StatusNoContent)
    default:
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
    }
}

// handleArmModes handles GET/POST on /api/arm_modes.  Only admins may modify modes.
func (s *Server) handleArmModes(w http.ResponseWriter, r *http.Request, user User) {
    switch r.Method {
    case http.MethodGet:
        cfg := s.cfgMgr.Get()
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(cfg.ArmModes)
    case http.MethodPost:
        if !user.Admin {
            http.Error(w, "forbidden", http.StatusForbidden)
            return
        }
        var req ArmMode
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            http.Error(w, "invalid JSON", http.StatusBadRequest)
            return
        }
        if req.Name == "" {
            http.Error(w, "missing name", http.StatusBadRequest)
            return
        }
        err := s.cfgMgr.Update(func(c *Config) error {
            // Replace existing with same name or append new
            for i, am := range c.ArmModes {
                if strings.EqualFold(am.Name, req.Name) {
                    c.ArmModes[i] = req
                    return nil
                }
            }
            c.ArmModes = append(c.ArmModes, req)
            return nil
        })
        if err != nil {
            http.Error(w, "internal error", http.StatusInternalServerError)
            return
        }
        w.WriteHeader(http.StatusCreated)
    default:
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
    }
}