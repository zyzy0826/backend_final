#!/bin/sh
# Generate the EC P-256 key pair used to sign JWTs (ES256).
#
# This runs inside the app image, which ships openssl, so no OpenSSL install is
# needed on the host. Compose runs it once before the server starts and the keys
# land in the shared `keys` volume.
#
# Existing keys are kept: regenerating them would invalidate every JWT already
# handed out.
set -e

KEY_DIR="${KEY_DIR:-/app/keys}"
mkdir -p "$KEY_DIR"

if [ -s "$KEY_DIR/private.pem" ] && [ -s "$KEY_DIR/public.pem" ]; then
	echo "gen-keys: key pair already present in $KEY_DIR, keeping it"
	exit 0
fi

openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:P-256 -out "$KEY_DIR/private.pem"
openssl pkey -in "$KEY_DIR/private.pem" -pubout -out "$KEY_DIR/public.pem"
chmod 600 "$KEY_DIR/private.pem"

echo "gen-keys: generated EC P-256 key pair in $KEY_DIR"
