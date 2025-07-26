package main

// ZoneType enumerates the types of sensors supported by the system.
// For now we support "contact" (magnetic door/window sensor) and "pir" (passive infrared motion detector).
type ZoneType string

const (
    ZoneTypeContact ZoneType = "contact"
    ZoneTypePIR     ZoneType = "pir"
)

// Zone represents a physical or logical area monitored by one or more sensors.
// Each zone is associated with a GPIO pin on the Raspberry Pi.  Additional
// fields could be added to support multiple pins per zone or alternative sensor types.
type Zone struct {
    ID      int      `json:"id"`      // unique numeric identifier
    Name    string   `json:"name"`    // human‑readable name (e.g. "Front Door")
    Type    ZoneType `json:"type"`    // sensor type: "contact" or "pir"
    Pin     int      `json:"pin"`     // GPIO pin number (BCM numbering)
    Enabled bool     `json:"enabled"` // if false the zone is ignored
    Mode    string   `json:"mode,omitempty"` // input mode: "NO" (normally open), "NC" (normally closed), "EOL" (end of line)
}

// ArmMode associates a name with a list of zone IDs that should be monitored when this mode is active.
// Examples: Away (all zones), Home (perimeter only), Night (custom subset).
type ArmMode struct {
    Name       string `json:"name"`
    ActiveZones []int  `json:"active_zones"`
}

// User represents an account that can log in to the web UI.
// Passwords are stored as bcrypt hashes.  The Admin flag indicates
// whether the user may manage zones and other user accounts.
type User struct {
    Username     string `json:"username"`
    PasswordHash string `json:"password_hash"`
    Admin        bool   `json:"admin"`
}

// Config is the top‑level structure serialized to config.json.  It contains
// all persisted system state except for session tokens.  Additional fields
// can be added (e.g. alert settings) without breaking backward compatibility.
type Config struct {
    HTTPPort int     `json:"http_port"` // port to listen on (default 8443)
    CertFile string  `json:"cert_file"` // path to PEM encoded certificate
    KeyFile  string  `json:"key_file"`  // path to PEM encoded key
    Zones    []Zone  `json:"zones"`
    ArmModes []ArmMode `json:"arm_modes"`
    Users    []User  `json:"users"`
    LogFile  string  `json:"log_file,omitempty"` // path to event log file
    // Alerts define how the system should notify when a zone is triggered.
    // If empty, a default log alert will be used.  Each alert configuration
    // may define an email transport or other mechanism.  See AlertConfig for
    // details.
    Alerts   []AlertConfig `json:"alerts,omitempty"`
}

// AlertConfig specifies the configuration for a single alerting mechanism.  The
// Type field selects the handler: currently "log" writes to the event log and
// "email" sends an email via SMTP.  When Type is "email", the SMTP fields
// must be provided.
type AlertConfig struct {
    Type       string `json:"type"`        // "log" or "email"
    SMTPServer string `json:"smtp_server,omitempty"`
    SMTPPort   int    `json:"smtp_port,omitempty"`
    Username   string `json:"username,omitempty"`
    Password   string `json:"password,omitempty"`
    From       string `json:"from,omitempty"`
    To         string `json:"to,omitempty"`
    Subject    string `json:"subject,omitempty"`
}