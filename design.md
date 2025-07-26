# Alarm System Design for the Minder Repository

## Overview

This document proposes a high‑level design for a burglar alarm system that runs on a **Raspberry Pi**.  The system monitors multiple *zones* – each zone corresponds to one or more physical sensors (such as door contact switches or passive infrared (PIR) motion detectors).  A built‑in web interface allows authorised users to configure zones, arm or disarm the system, manage multiple arm profiles (e.g. *Stay*, *Away*, *Night*), and administer user accounts.

The implementation outlined below uses the **Go** programming language because it compiles to a single static binary that runs on ARM‑based Raspberry Pi hardware with minimal fuss.  Go’s standard library provides everything we need for HTTP(s) servers, TLS, JSON encoding/decoding and concurrent sensor polling without pulling in heavyweight frameworks.  If you prefer C# and ASP.NET Core you could achieve the same goals, but Go keeps the runtime footprint and installation process simple for an embedded device.

## Key Requirements

* **Multiple Zones:** Each zone represents a logical grouping of sensors.  Typical zone types include **door contacts** and **PIR motion detectors**.  Each zone will be associated with one or more GPIO pins on the Raspberry Pi.
* **Arm Modes:** The system must support different arm profiles.  Examples include:
  * **Disarmed** – no alarms are generated.
  * **Away** – all zones are monitored; any sensor triggers the alarm.
  * **Home/Stay** – perimeter sensors (doors and windows) are armed, but interior motion sensors are ignored so that occupants can move freely.
  * **Night** – similar to *Home*, but perhaps disables only certain zones.
* **Web Administration UI:** A browser‑based interface allows the user to:
  * View the current system state (armed/disarmed, triggered zones, timestamps, etc.).
  * Configure zones – add, delete or edit zone names, types, GPIO pin assignments and active/inactive status.
  * Configure arm profiles – define which zones are active in each mode.
  * Arm and disarm the system.
  * Manage user accounts (add/remove users, assign administrator privileges, change passwords).
* **HTTPS Support:** All web traffic must be encrypted.  The server should load an X.509 certificate/key pair from disk (e.g. `server.crt` and `server.key`).  To make certificate setup easy for end users we supply a helper script and document:
  * How to generate a self‑signed certificate with `openssl` or `mkcert`.
  * How to request a Let’s Encrypt certificate using `certbot` when the Pi is reachable from the public internet.
* **Extensibility:** The code should be modular enough to allow new zone types (e.g. glass‑break sensors), new communication channels (e.g. SMS or email alerts) or integration with home automation systems.

## Architecture

The system comprises three main layers:

1. **Hardware Abstraction Layer (HAL):** A small module abstracts access to the Raspberry Pi’s GPIO pins.  When compiled on a desktop for testing, the HAL can simulate sensor states in memory; on the Pi it uses a library such as `periph.io/x/periph` or `github.com/stianeikeland/go-rpio` to poll the real pins.  Each zone subscribes to state changes from the HAL.

2. **Core Logic:** Responsible for maintaining the overall system state.  It maintains:
   * A slice of `Zone` objects (name, ID, type, pin number, enabled flag).
   * The current `ArmMode`.
   * A list of `User` records with password hashes and role flags.
   * An in‑memory map of active user sessions.
   * A goroutine that continuously polls GPIO pins and raises events when sensors change state.  When armed, a triggered zone leads to an alarm condition.
   * Functions to arm/disarm the system and to update configuration (zones, users, arm modes).  Persistent configuration is stored in a JSON file (`config.json`) on disk.

3. **HTTP Server:** Provides both RESTful API endpoints and static web content.  It uses Go’s `net/http` package with TLS enabled.  Key endpoints include:
   * `POST /api/login` – authenticate a user and return a session token in an HTTP‑only cookie.
   * `GET /api/status` – return the current arm mode, triggered zones and system uptime.
   * `GET /api/zones` / `POST /api/zones` / `PUT /api/zones/{id}` / `DELETE /api/zones/{id}` – CRUD operations on zones.
   * `GET /api/arm_modes` / `POST /api/arm_modes` – list or modify arm profiles.
   * `POST /api/arm` – request arm mode change.  Only authenticated users can call this.
   * `POST /api/disarm` – disarm the system.
   * `GET /api/users` / `POST /api/users` / `PUT /api/users/{id}` / `DELETE /api/users/{id}` – user administration (admin only).
   * Static files (HTML, CSS, JS) served from an embedded `/web` folder using `fs.FS` so the binary bundles the UI.

Sessions are stored in memory with expiry timestamps.  Passwords are hashed using `bcrypt` from Go’s `golang.org/x/crypto/bcrypt` package.  The REST handlers enforce authentication and authorisation before performing sensitive actions.

## Certificate Management

The HTTPS server loads the certificate and key specified in the configuration file.  If the files do not exist at startup, the server refuses to start and prints instructions for the user.  Two approaches are recommended:

* **Self‑signed:** Use the provided helper script (`scripts/generate_cert.sh`) which calls `openssl` to generate `server.crt` and `server.key`.  Browsers will warn that the certificate is self‑signed; users must add a security exception.  The script is documented in `README.md`.
* **Let’s Encrypt:** If the Raspberry Pi is accessible on the public internet with a domain name, install `certbot` and run `sudo certbot certonly --standalone -d your-domain.example`.  Then copy the resulting `fullchain.pem` and `privkey.pem` into the project directory and update `config.json` to point to them.  Detailed instructions are included in `README.md`.

## Persisting Data

All persistent data (zones, users, arm modes and certificate paths) lives in `config.json`.  The server loads this file at startup.  On every configuration change via the API, it rewrites the file atomically.  Secrets such as password hashes are stored, but no plaintext passwords or session tokens are persisted.  In the future the file could be replaced with a small embedded database (e.g. BoltDB) without changing the API.

## Security Considerations

* Passwords are never stored or transmitted in plaintext; `bcrypt` ensures they are salted and hashed.  Sessions use high‑entropy random IDs stored in HTTP‑only cookies to mitigate cross‑site scripting.
* The API enforces role‑based access control; only administrators can manage users or zones.  Ordinary users can arm or disarm the system but cannot edit configuration.
* All HTTP endpoints are served over TLS.  Non‑TLS connections are refused.
* The server uses Go’s default TLS configuration, which disables insecure ciphers and protocols.  Administrators may adjust the TLS settings in code if required.

## Extending the System

The modular architecture allows easy expansion.  To add a new sensor type, implement a new `ZoneType` constant and update the HAL to recognise it.  To integrate with external notification services, add an `AlertHandler` interface and implement handlers for email, SMS or push notifications.  Additional arm modes can be added by extending the `ArmMode` enumeration and updating the configuration schema.

## Conclusion

This design provides a flexible, secure and extensible burglar alarm system tailored for Raspberry Pi.  It balances simplicity (using Go’s standard library) with the ability to grow.  The following codebase implements the core described above.  Developers and end users should read `README.md` for installation, configuration and certificate setup instructions.