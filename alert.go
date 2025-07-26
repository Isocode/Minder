package main

// This file defines pluggable alert handlers for when a sensor is triggered.

import (
    "fmt"
    "net/smtp"
)

// AlertHandler represents a mechanism that can send an alert when a zone is
// triggered.  Implementations may deliver notifications via email, SMS or
// other channels.  The Send method receives the zone that was triggered and
// a logger to record any diagnostics.  If an error is returned, the caller
// should log it but continue operation.
type AlertHandler interface {
    Name() string
    Send(zone Zone, logger *EventLogger) error
}

// LogAlert logs a simple message to the event logger when a zone triggers.
// This is the default alert handler if no other alerts are configured.
type LogAlert struct{}

// Name returns the type name of the alert handler.
func (LogAlert) Name() string { return "log" }

// Send writes an alert to the event log.
func (LogAlert) Send(zone Zone, logger *EventLogger) error {
    logger.Log("alert: zone %d (%s) triggered", zone.ID, zone.Name)
    return nil
}

// EmailAlert sends an email via an SMTP server when a zone triggers.  All
// configuration values are supplied via the corresponding AlertConfig in
// config.json.  The subject defaults to "Minder alert" if empty.
type EmailAlert struct {
    SMTPServer string
    SMTPPort   int
    Username   string
    Password   string
    From       string
    To         string
    Subject    string
}

// Name returns the type name of the alert handler.
func (EmailAlert) Name() string { return "email" }

// Send dispatches an email.  It composes a minimal plaintext message with a
// subject and body describing the triggered zone.  Errors from smtp.SendMail
// are returned directly so the caller can log them.
func (e EmailAlert) Send(zone Zone, logger *EventLogger) error {
    subject := e.Subject
    if subject == "" {
        subject = "Minder alert"
    }
    body := fmt.Sprintf("Zone %s (ID %d) has been triggered", zone.Name, zone.ID)
    // Compose headers and body.  RFC 5322 requires CRLF line endings.
    msg := fmt.Sprintf("To: %s\r\nSubject: %s\r\n\r\n%s\r\n", e.To, subject, body)
    addr := fmt.Sprintf("%s:%d", e.SMTPServer, e.SMTPPort)
    auth := smtp.PlainAuth("", e.Username, e.Password, e.SMTPServer)
    return smtp.SendMail(addr, auth, e.From, []string{e.To}, []byte(msg))
}