#!/bin/sh
#
# generate_cert.sh
#
# A helper script to generate a self-signed TLS certificate for the Minder alarm system.
# It creates `server.key` and `server.crt` valid for one year.  Use this
# only for development or if you do not have a domain name.  Browsers will
# display a warning because the certificate is not signed by a trusted
# certificate authority.

set -e

# Determine output directory relative to this script (../)
OUT_DIR="$(dirname "$0")/.."
KEY_FILE="$OUT_DIR/server.key"
CRT_FILE="$OUT_DIR/server.crt"

echo "Generating self-signed certificate into $CRT_FILE and $KEY_FILE..."

openssl req -x509 -newkey rsa:2048 \
  -keyout "$KEY_FILE" -out "$CRT_FILE" \
  -days 365 -nodes \
  -subj "/C=US/ST=State/L=City/O=Minder/OU=Alarm/CN=localhost"

chmod 600 "$KEY_FILE"

echo "Self-signed certificate created. Copy $CRT_FILE and $KEY_FILE to your Raspberry Pi and update config.json if necessary."