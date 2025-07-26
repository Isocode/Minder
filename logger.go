package main

import (
    "fmt"
    "os"
    "sync"
    "time"
)

// EventLogger writes timestamped events to a file.  It is safe for concurrent use.
type EventLogger struct {
    filePath string
    mu       sync.Mutex
}

// NewEventLogger creates a logger writing to filePath.  If the directory does not
// exist it will be created.  File rotation by date can be added later.
func NewEventLogger(filePath string) *EventLogger {
    return &EventLogger{filePath: filePath}
}

// Log writes a single event with timestamp.  Errors are ignored but printed
// to standard error.
func (el *EventLogger) Log(format string, args ...any) {
    el.mu.Lock()
    defer el.mu.Unlock()
    msg := fmt.Sprintf(format, args...)
    ts := time.Now().Format(time.RFC3339)
    line := fmt.Sprintf("%s - %s\n", ts, msg)
    // Open file in append mode, create if not exists
    f, err := os.OpenFile(el.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
    if err != nil {
        fmt.Fprintf(os.Stderr, "log error: %v\n", err)
        return
    }
    defer f.Close()
    if _, err := f.WriteString(line); err != nil {
        fmt.Fprintf(os.Stderr, "log write error: %v\n", err)
    }
}