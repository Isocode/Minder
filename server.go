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
    "sync"
    "time"
    "os"
)

//go:embed web/dist/*
var embeddedFiles embed.FS

// Server holds global state for the HTTP server and the alarm logic.
type Server struct {
    cfgMgr    *ConfigManager
    sessions  *SessionManager
    currentMode string        // name of currently active arm mode ("Disarmed" if none)
    triggered map[int]bool    // zones that have been triggered since last arm
    logger    *EventLogger    // event logger
    testMode  int             // 0 = normal, 1 = TestSoft, 2 = TestWiring
    alerts    []AlertHandler  // configured alert handlers
    triggerMu sync.Mutex      // guards concurrent access to triggered map
}

// NewServer constructs a new Server and initialises GPIO.
func NewServer(cfgMgr *ConfigManager) (*Server, error) {
    if err := initGPIO(); err != nil {
        return nil, err
    }
    cfg := cfgMgr.Get()
    logger := NewEventLogger(cfg.LogFile)
    s := &Server{
        cfgMgr:     cfgMgr,
        sessions:   NewSessionManager(),
        currentMode: "Disarmed",
        triggered:  make(map[int]bool),
        logger:     logger,
        testMode:   0,
    }
    // Initialise alert handlers based on configuration.  If no alerts are
    // configured, a default LogAlert is used.
    s.alerts = initAlertHandlers(cfg, logger)
    // Start polling sensors in the background.  The goroutine will idle
    // while the system is disarmed or in TestSoft mode.
    go s.pollSensors()
    return s, nil
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
    mux.HandleFunc("/api/logs", s.withAuth(s.handleLogs))
    mux.HandleFunc("/api/test_trigger", s.withAuth(s.handleTestTrigger))
    
    // Static files.  The front‑end is built into web/dist by Vite.  Ensure you
    // run `npm run build` in the web folder before building the Go binary so
    // that web/dist exists.  We strip the "dist" prefix so that index.html is
    // served at the root.
    fs := http.FS(embeddedFiles)
    fileServer := http.FileServer(fs)
    mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
        // Always serve index.html for directories or missing files to support SPA routing
        path := r.URL.Path
        if path == "/" || strings.HasPrefix(path, "/index.html") {
            r.URL.Path = "/index.html"
        }
        fileServer.ServeHTTP(w, r)
    })

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
    s.logger.Log("login %s", user.Username)
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
    s.logger.Log("logout")
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
    mode := strings.TrimSpace(req.Mode)
    cfg := s.cfgMgr.Get()
    // Handle special test modes
    lower := strings.ToLower(mode)
    if lower == "testsoft" || lower == "test soft" {
        s.currentMode = "TestSoft"
        s.testMode = 1
        s.triggerMu.Lock()
        s.triggered = make(map[int]bool)
        s.triggerMu.Unlock()
        s.logger.Log("arm TestSoft by %s", user.Username)
        w.WriteHeader(http.StatusNoContent)
        return
    }
    if lower == "testwiring" || lower == "test wiring" {
        s.currentMode = "TestWiring"
        s.testMode = 2
        s.triggerMu.Lock()
        s.triggered = make(map[int]bool)
        s.triggerMu.Unlock()
        s.logger.Log("arm TestWiring by %s", user.Username)
        w.WriteHeader(http.StatusNoContent)
        return
    }
    // Validate normal arm mode exists
    var activeZones []int
    for _, am := range cfg.ArmModes {
        if strings.EqualFold(am.Name, mode) {
            activeZones = am.ActiveZones
            break
        }
    }
    if activeZones == nil {
        http.Error(w, "unknown arm mode", http.StatusBadRequest)
        return
    }
    s.currentMode = mode
    s.testMode = 0
    // Reset triggered flags
    s.triggerMu.Lock()
    s.triggered = make(map[int]bool)
    s.triggerMu.Unlock()
    log.Printf("System armed in %s mode (active zones: %v)\n", s.currentMode, activeZones)
    s.logger.Log("arm %s by %s", s.currentMode, user.Username)
    w.WriteHeader(http.StatusNoContent)
}

// handleDisarm disarms the system and resets triggered flags.
func (s *Server) handleDisarm(w http.ResponseWriter, r *http.Request, user User) {
    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }
    s.currentMode = "Disarmed"
    s.testMode = 0
    s.triggerMu.Lock()
    s.triggered = make(map[int]bool)
    s.triggerMu.Unlock()
    log.Println("System disarmed")
    s.logger.Log("disarm by %s", user.Username)
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
        s.logger.Log("create zone %s (id=%d) by %s", z.Name, z.ID, user.Username)
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
        s.logger.Log("update zone id=%d by %s", id, user.Username)
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
        s.logger.Log("delete zone id=%d by %s", id, user.Username)
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
        s.logger.Log("create user %s by %s", req.Username, user.Username)
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
        s.logger.Log("update user %s by %s", username, user.Username)
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
        s.logger.Log("delete user %s by %s", username, user.Username)
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
        s.logger.Log("update arm mode %s by %s", req.Name, user.Username)
        w.WriteHeader(http.StatusCreated)
    default:
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
    }
}

// handleLogs returns the event log.  Admins only.  Accepts optional query parameter `lines=n` to limit number of lines returned.
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request, user User) {
    if !user.Admin {
        http.Error(w, "forbidden", http.StatusForbidden)
        return
    }
    linesParam := r.URL.Query().Get("lines")
    limit := 200
    if linesParam != "" {
        if n, err := strconv.Atoi(linesParam); err == nil && n > 0 {
            limit = n
        }
    }
    cfg := s.cfgMgr.Get()
    data, err := os.ReadFile(cfg.LogFile)
    if err != nil {
        http.Error(w, "log not found", http.StatusNotFound)
        return
    }
    allLines := strings.Split(string(data), "\n")
    // Drop empty trailing line
    if len(allLines) > 0 && allLines[len(allLines)-1] == "" {
        allLines = allLines[:len(allLines)-1]
    }
    start := 0
    if len(allLines) > limit {
        start = len(allLines) - limit
    }
    lines := allLines[start:]
    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(lines)
}

// handleTestTrigger allows an admin to simulate a zone trigger while in
// TestSoft mode.  Clients send a JSON body {"zone_id":<int>}.  If the
// specified zone exists and has not already been triggered, it will be marked
// as triggered, an event will be logged and the configured alert handlers will
// be invoked (even in test mode).  If the system is not currently in
// TestSoft mode (testMode != 1), the request is rejected.
func (s *Server) handleTestTrigger(w http.ResponseWriter, r *http.Request, user User) {
    if r.Method != http.MethodPost {
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
        return
    }
    if s.testMode != 1 {
        http.Error(w, "not in TestSoft mode", http.StatusBadRequest)
        return
    }
    var req struct{
        ZoneID int `json:"zone_id"`
    }
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "invalid JSON", http.StatusBadRequest)
        return
    }
    cfg := s.cfgMgr.Get()
    var zone *Zone
    for i := range cfg.Zones {
        if cfg.Zones[i].ID == req.ZoneID {
            zone = &cfg.Zones[i]
            break
        }
    }
    if zone == nil {
        http.Error(w, "zone not found", http.StatusNotFound)
        return
    }
    s.triggerMu.Lock()
    already := s.triggered[zone.ID]
    if !already {
        s.triggered[zone.ID] = true
        s.triggerMu.Unlock()
        s.logger.Log("test trigger zone id=%d (%s) by %s", zone.ID, zone.Name, user.Username)
        // Invoke all alert handlers even in TestSoft mode to allow testing the
        // configured notifications.  Errors are logged but do not propagate.
        for _, h := range s.alerts {
            if err := h.Send(*zone, s.logger); err != nil {
                s.logger.Log("alert handler %s error: %v", h.Name(), err)
            }
        }
    } else {
        s.triggerMu.Unlock()
    }
    w.WriteHeader(http.StatusNoContent)
}

// pollSensors continuously polls the GPIO pins for all zones that are active
// in the current arm mode.  When a new trigger is detected, it logs the event
// and notifies configured alert handlers.  In TestWiring mode the alert
// handlers are suppressed, but triggers are still logged.  The loop sleeps
// briefly between iterations to reduce CPU usage.  It relies on readPin and
// zoneTriggered defined in hal.go and sensor.go.
func (s *Server) pollSensors() {
    for {
        time.Sleep(200 * time.Millisecond)
        // Skip polling when disarmed or in TestSoft mode
        if s.currentMode == "Disarmed" || s.testMode == 1 {
            continue
        }
        cfg := s.cfgMgr.Get()
        var activeIDs []int
        if s.testMode == 2 {
            // In wiring test, monitor all zones
            for _, z := range cfg.Zones {
                activeIDs = append(activeIDs, z.ID)
            }
        } else {
            // Find active zones for the current mode
            for _, am := range cfg.ArmModes {
                if strings.EqualFold(am.Name, s.currentMode) {
                    activeIDs = am.ActiveZones
                    break
                }
            }
        }
        for _, id := range activeIDs {
            var zone *Zone
            for i := range cfg.Zones {
                if cfg.Zones[i].ID == id {
                    zone = &cfg.Zones[i]
                    break
                }
            }
            if zone == nil || !zone.Enabled {
                continue
            }
            if zoneTriggered(*zone) {
                s.triggerMu.Lock()
                already := s.triggered[zone.ID]
                if !already {
                    s.triggered[zone.ID] = true
                    s.triggerMu.Unlock()
                    s.logger.Log("trigger zone id=%d (%s)", zone.ID, zone.Name)
                    // Only send alerts if not in wiring test mode
                    if s.testMode == 0 {
                        for _, h := range s.alerts {
                            if err := h.Send(*zone, s.logger); err != nil {
                                s.logger.Log("alert handler %s error: %v", h.Name(), err)
                            }
                        }
                    }
                } else {
                    s.triggerMu.Unlock()
                }
            }
        }
    }
}

// initAlertHandlers constructs a slice of AlertHandler instances from the
// provided configuration.  If cfg.Alerts is empty, a single LogAlert is
// returned to ensure that triggered events are always recorded.  The logger
// parameter is passed to handlers that need to log internal diagnostics.
func initAlertHandlers(cfg Config, logger *EventLogger) []AlertHandler {
    if len(cfg.Alerts) == 0 {
        return []AlertHandler{LogAlert{}}
    }
    var handlers []AlertHandler
    for _, ac := range cfg.Alerts {
        switch strings.ToLower(ac.Type) {
        case "log":
            handlers = append(handlers, LogAlert{})
        case "email":
            handlers = append(handlers, EmailAlert{
                SMTPServer: ac.SMTPServer,
                SMTPPort:   ac.SMTPPort,
                Username:   ac.Username,
                Password:   ac.Password,
                From:       ac.From,
                To:         ac.To,
                Subject:    ac.Subject,
            })
        }
    }
    if len(handlers) == 0 {
        handlers = append(handlers, LogAlert{})
    }
    return handlers
}