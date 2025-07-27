package main

import (
    "encoding/json"
    "errors"
    "fmt"
    "io/ioutil"
    "os"
    "sync"
)

// configPath is the default filename for persisted configuration.
const configPath = "config.json"

// ConfigManager wraps the loaded configuration and a mutex for concurrent access.
// When modifying configuration through the HTTP API, always call Save() to
// persist changes.
type ConfigManager struct {
    mu     sync.RWMutex
    cfg    Config
    loaded bool
}

// Load reads configuration from disk.  If the file does not exist, a default
// configuration is created with a single admin user (password: "admin", which
// you should change immediately) and persisted to disk.
func (cm *ConfigManager) Load() error {
    cm.mu.Lock()
    // If the config is already loaded in memory, release the lock and return.
    if cm.loaded {
        cm.mu.Unlock()
        return nil
    }
    // Attempt to read config.json
    data, err := ioutil.ReadFile(configPath)
    if err != nil {
        if os.IsNotExist(err) {
            // Create a default configuration
            defaultCfg := Config{
                HTTPPort: 8443,
                CertFile: "server.crt",
                KeyFile:  "server.key",
                Zones:    []Zone{},
                ArmModes: []ArmMode{
                    {Name: "Away", ActiveZones: []int{}},
                    {Name: "Home", ActiveZones: []int{}},
                },
                Users: []User{
                    {Username: "admin", PasswordHash: hashPassword("admin"), Admin: true},
                },
                LogFile: "events.log",
                Alerts: []AlertConfig{{Type: "log"}},
                ExitDelay: 30,
                EntryDelay: 30,
            }
            cm.cfg = defaultCfg
            cm.loaded = true
            // Release the write lock before saving to avoid deadlock: Save acquires
            // a read lock on the same mutex.
            cm.mu.Unlock()
            return cm.Save()
        }
        // Some other error reading config.json
        cm.mu.Unlock()
        return fmt.Errorf("unable to read config: %w", err)
    }
    // Unmarshal existing config
    if err := json.Unmarshal(data, &cm.cfg); err != nil {
        cm.mu.Unlock()
        return fmt.Errorf("invalid config.json: %w", err)
    }
    cm.loaded = true
    cm.mu.Unlock()
    return nil
}

// Save writes the configuration to disk.  Call this after any changes to
// configuration via the API.
func (cm *ConfigManager) Save() error {
    cm.mu.RLock()
    defer cm.mu.RUnlock()
    
    bytes, err := json.MarshalIndent(cm.cfg, "", "  ")
    if err != nil {
        return err
    }
    tmpPath := configPath + ".tmp"
    if err := ioutil.WriteFile(tmpPath, bytes, 0600); err != nil {
        return err
    }
    return os.Rename(tmpPath, configPath)
}

// Get returns a copy of the current configuration.  Callers must treat the
// returned Config as immutable.
func (cm *ConfigManager) Get() Config {
    cm.mu.RLock()
    defer cm.mu.RUnlock()
    return cm.cfg
}

// Update applies a user supplied function to modify the configuration.  It
// holds the write lock, calls the supplied function with a pointer to the
// internal config, and then persists the change.  The updater must not
// capture the pointer beyond the scope of the function.
func (cm *ConfigManager) Update(fn func(*Config) error) error {
    cm.mu.Lock()
    // Apply the update while holding the write lock.
    if err := fn(&cm.cfg); err != nil {
        cm.mu.Unlock()
        return err
    }
    // Release the lock before saving to avoid deadlock: Save acquires a read
    // lock on the same mutex.
    cm.mu.Unlock()
    return cm.Save()
}

// FindUser returns a user and its index by username.  If not found, index
// will be -1.
func (cm *ConfigManager) FindUser(username string) (User, int) {
    cm.mu.RLock()
    defer cm.mu.RUnlock()
    for i, u := range cm.cfg.Users {
        if u.Username == username {
            return u, i
        }
    }
    return User{}, -1
}

// Authenticate checks whether the provided username and password are valid.  It
// returns the user object if authentication succeeds.
func (cm *ConfigManager) Authenticate(username, password string) (User, error) {
    user, _ := cm.FindUser(username)
    if user.Username == "" {
        return User{}, errors.New("invalid credentials")
    }
    if err := checkPasswordHash(password, user.PasswordHash); err != nil {
        return User{}, errors.New("invalid credentials")
    }
    return user, nil
}