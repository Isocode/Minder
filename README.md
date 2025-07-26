# Minder Alarm System

This project implements a modular burglar alarm system designed to run on a **Raspberry Pi**.  Written in Go, the application monitors multiple sensor *zones* connected to the Pi’s GPIO pins, exposes a secure web interface for configuration and control, and supports multiple arm profiles (e.g. *Away*, *Home* or *Night*).

## Features

* **Multiple Zones** – define door contacts, PIR motion detectors or other sensor types, and group them into arm profiles.
* **Arm Modes** – create profiles such as *Away* (all zones active) or *Home* (interior motion sensors ignored).
* **Web UI** – a React‑based SPA served over HTTPS allows users to arm/disarm the system, view status, configure zones and manage user accounts.  It includes built‑in help, event log viewing and test modes.
* **User Management** – administrator accounts can add or remove users, assign roles and change passwords.
* **Logging** – all significant events (login, arm, disarm, zone triggers, configuration changes) are recorded to a rolling log file.  The UI exposes a Logs page to view recent entries.
* **Test Modes** – two special arm modes allow safe testing.  *Test Soft* lets you trigger zones via the UI to verify alerts.  *Test Wiring* monitors sensors and logs triggers without sending alerts, so you can check wiring without disturbing your household.
* **Alerting** – the system supports pluggable alerts.  By default it logs alerts to the event log; you can enable email alerts by editing `config.json`.  Additional alert handlers can be added by implementing the `AlertHandler` interface.
* **TLS by Default** – the HTTP server requires a certificate and private key.  A helper script for generating a self‑signed cert is included.  You can also use Let’s Encrypt if the Pi is publicly reachable.
* **Extensible** – the code structure makes it easy to add new zone types or alert mechanisms (email, SMS, etc.).

## Prerequisites

* **Go 1.20** or later on your development machine.  To cross‑compile for the Raspberry Pi (ARMv7), set `GOOS=linux` and `GOARCH=arm`.
* A **Raspberry Pi** with the appropriate sensors wired to its GPIO pins.  You may need to run the binary as root to access GPIO.
* A valid TLS certificate (`server.crt`) and key (`server.key`).  See the *TLS setup* section below.

## Building

1. Clone this repository and change into the project directory:

   ```sh
   git clone https://github.com/your-user/Minder.git
   cd Minder/minder
   ```

2. Initialise the Go module and download dependencies:

   ```sh
   go mod tidy
   ```

3. To build for your local machine:

   ```sh
   go build -o minder
   ```

   To build for a Raspberry Pi (32‑bit ARM):

   ```sh
   GOOS=linux GOARCH=arm go build -o minder
   ```

4. Copy `minder`, `config.json`, `server.crt` and `server.key` to your Pi, then run:

   ```sh
   ./minder
   ```

   The server listens on port **8443** by default.  Visit `https://<pi-ip-address>:8443` in a browser.

## TLS Setup

For security the API and web UI are served **only** over HTTPS.  The application expects a certificate/key pair at paths specified in `config.json` (defaults are `server.crt` and `server.key` in the working directory).  If these files are missing the server will refuse to start.

### Generating a Self‑signed Certificate

The simplest way to get started is to generate a self‑signed certificate.  A helper script is provided:

```sh
cd Minder/minder/scripts
./generate_cert.sh
```

This uses `openssl` to generate `server.crt` and `server.key` valid for 365 days.  When you browse to the server you will need to accept the certificate warning.

### Using Let’s Encrypt

If your Pi is publicly reachable with a domain name, you can obtain a free certificate from Let’s Encrypt.  On the Pi run:

```sh
sudo apt install certbot
sudo certbot certonly --standalone -d your-domain.example
```

When complete, copy the resulting `fullchain.pem` to `server.crt` and `privkey.pem` to `server.key`, or update `config.json` to point to the correct paths.  Restart the server.

## Configuration

Configuration is stored in **config.json**.  The file is created automatically the first time the program runs.  It contains zones, arm modes, users and TLS settings.  You can edit this file by hand or via the web UI.  Here is an example:

```json
{
  "http_port": 8443,
  "cert_file": "server.crt",
  "key_file": "server.key",
  "zones": [
    {"id":1, "name":"Front Door", "type":"contact", "pin":17, "enabled":true},
    {"id":2, "name":"Lounge PIR", "type":"pir", "pin":27, "enabled":true}
  ],
  "arm_modes": [
    {"name":"Away", "active_zones":[1,2]},
    {"name":"Home", "active_zones":[1]}
  ],
  "users": [
    {"username":"admin", "password_hash":"$2a$10$...", "admin":true}
  ]
}
```

## GPIO Access

The `hal.go` file wraps access to the Raspberry Pi GPIO pins.  During development on your desktop the GPIO functions are stubbed out so you can run and test the web UI without hardware attached.  When compiled on the Pi you can enable real GPIO by using a build tag (see comments in `hal.go`) and adding an appropriate driver such as [go‑rpio](https://github.com/stianeikeland/go-rpio) to `go.mod`.

## Running as a Service

On the Pi you may want the alarm to start automatically on boot.  Create a systemd service unit:

```
[Unit]
Description=Minder Alarm System
After=network.target

[Service]
ExecStart=/home/pi/minder
WorkingDirectory=/home/pi
User=pi
Restart=always

[Install]
WantedBy=multi-user.target
```

Save this as `/etc/systemd/system/minder.service`, reload systemd (`sudo systemctl daemon-reload`), enable the service (`sudo systemctl enable minder`), then start it (`sudo systemctl start minder`).

## Getting Help

The web application includes a **Help** page accessible from the navigation bar once you log in.  It provides step‑by‑step guidance for everyday tasks such as arming/disarming, configuring zones and users, using test modes and reading the event log.  For developers and tinkerers looking to extend the system, see the file `DEVELOPMENT.md` in this repository.

---

Contributions and bug reports are welcome!  See `design.md` for more technical details on the architecture.