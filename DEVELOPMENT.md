# Minder Development Guide

This document is aimed at developers and tinkerers who want to understand how the Minder alarm system works under the hood, customise it or extend its capabilities.  It complements the end‑user help built into the web UI.

## Overview

The application consists of two main parts:

1. **Back‑end server** – written in Go.  It exposes a REST API and serves the compiled front‑end.  Core responsibilities include user authentication, configuration persistence, arm/disarm logic, sensor polling and alert dispatch.
2. **Front‑end client** – a single‑page application built with React and Vite.  It provides a responsive UI for users to manage zones, arm modes, users, test modes and to view logs.

### File Structure

```
minder/
  main.go            – entry point that loads the config and starts the HTTPS server.
  server.go          – HTTP handlers, session management, arm/disarm logic and sensor polling.
  config.go          – thread‑safe configuration manager for loading and saving `config.json`.
  model.go           – data structures representing zones, arm modes, users and alert configs.
  hal.go             – stubbed hardware abstraction layer (HAL) for GPIO.  Always returns false.
  hal_rpi.go         – Raspberry Pi implementation of the HAL using the periph.io libraries.  Enabled when building on a Pi (Linux/ARM) without the `disablegpio` build tag.
  sensor.go          – helper that interprets GPIO levels according to zone modes (NO, NC, EOL).
  alert.go           – pluggable alert interface with log and email implementations.
  logger.go          – event logger that writes timestamped entries to a rolling log file.
  web/               – React/Vite front‑end source code and build configuration.
  scripts/
    generate_cert.sh – helper script to create a self‑signed TLS certificate.
  config.json        – persisted configuration (created on first run).
  events.log         – default event log (created on first run).
```

For an architectural discussion see `design.md`.

## Building and Running

### Prerequisites

* **Go 1.20** or later with module support enabled.
* **Node.js** and **npm** for building the front‑end.
* On the Raspberry Pi you’ll also need the system GPIO configured and accessible (typically by running the binary as root).

### Building the Front‑end

The front‑end is not committed as prebuilt assets; you must compile it before building the Go binary so that the `web/dist` directory exists.  From the `minder/web` directory run:

```sh
npm install        # install dependencies
npm run build      # produce optimized assets in web/dist
```

These static files will be embedded in the Go binary when you build the back‑end.

### Building the Back‑end

Return to the `minder` directory and tidy dependencies:

```sh
go mod tidy
```

To build for your local machine (e.g. Linux/AMD64):

```sh
go build -o minder
```

To cross‑compile for the Raspberry Pi (32‑bit ARM) and enable real GPIO access via `hal_rpi.go`:

```sh
GOOS=linux GOARCH=arm go build -tags="" -o minder
```

If you wish to disable GPIO entirely (e.g. for development on a Pi without sensors) you can specify the `disablegpio` build tag:

```sh
GOOS=linux GOARCH=arm go build -tags=disablegpio -o minder
```

### TLS Certificates

The server **requires** a certificate/key pair to start.  See the main `README.md` for instructions on generating a self‑signed cert or using Let’s Encrypt.  Update `config.json` to point at your cert and key files before running the server.

## Configuration

`config.json` holds persistent state:

* **http_port** – port the HTTPS server listens on (default 8443).
* **cert_file**, **key_file** – paths to your TLS certificate and key.
* **zones** – array of zone definitions (ID, name, type, GPIO pin, enabled flag and mode).  `mode` controls how GPIO levels are interpreted: `NO` (normally open), `NC` (normally closed) or `EOL` (end‑of‑line).  See `sensor.go` for details.
* **arm_modes** – list of arm profiles mapping names (Away, Home, Night, etc.) to active zone IDs.
* **users** – administrator and regular user accounts with bcrypt password hashes.
* **log_file** – path to the rolling event log.
* **alerts** – optional array of alert configurations.  Each entry must have a `type`.  Supported types:
  * `log` – write an alert entry to the event log (default).
  * `email` – send an email via SMTP.  Provide `smtp_server`, `smtp_port`, `username`, `password`, `from`, `to` and optionally `subject`.

Any changes made via the API are persisted immediately.  You can also edit `config.json` by hand (stop the server first).

## Test Modes

Two special arm modes facilitate testing and development without disturbing occupants:

* **Test Soft** – Arms the system but ignores real sensors.  Instead, you can trigger zones manually from the Test page in the UI.  Use this to verify alert delivery and end‑to‑end behaviour.
* **Test Wiring** – Arms the system and polls all enabled zones.  When a zone goes active, the event is logged but alert handlers are suppressed.  Use this to check sensor wiring without sounding alarms.

To enter a test mode, click the corresponding button on the Status or Test page.  Disarm to exit.

## Adding New Alerts

Alerts are implemented via the `AlertHandler` interface in `alert.go`.  To add a new mechanism (e.g. SMS or push notifications):

1. Create a new struct implementing `Name() string` and `Send(zone Zone, logger *EventLogger) error`.
2. Add a `type` string to the `AlertConfig` struct in `model.go` and update `initAlertHandlers()` in `server.go` to construct your handler when the matching type appears in `config.json`.
3. Document the required configuration fields.

## Extending Sensor Support

The HAL is intentionally simple.  The stub in `hal.go` returns `false` for all pins so you can run the server on any machine.  The Raspberry Pi implementation in `hal_rpi.go` uses the [periph.io](https://periph.io/) libraries to read digital pins.  If you wish to support analogue sensors or other hardware:

* Update `readPin()` to read the hardware appropriately.
* Extend `Zone` with additional fields (e.g. ADC channel, threshold value).
* Modify `zoneTriggered()` in `sensor.go` to compute activation based on these fields.

## Development Tips

* Use the **Logs** page to see what the system is doing.  It records every login, arm/disarm, zone trigger and configuration change.  `tail -f events.log` in the console can also be useful.
* Keep your TLS certificate secure.  For production deployments, use a proper CA‑issued certificate rather than the self‑signed one.
* When testing email alerts, consider using a local mail sink such as [MailHog](https://github.com/mailhog/MailHog) to capture messages.
* If you modify the front‑end, always rerun `npm run build` before rebuilding the Go binary so that the embedded assets are up to date.

Enjoy hacking on Minder!  Contributions are welcome—feel free to open issues or pull requests.