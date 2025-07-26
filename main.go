package main

import (
    "log"
)

// Entry point for the Minder alarm system
func main() {
    var cfgMgr ConfigManager
    if err := cfgMgr.Load(); err != nil {
        log.Fatalf("failed to load configuration: %v", err)
    }
    server, err := NewServer(&cfgMgr)
    if err != nil {
        log.Fatalf("initialisation error: %v", err)
    }
    if err := server.Start(); err != nil {
        log.Fatalf("server exited: %v", err)
    }
}