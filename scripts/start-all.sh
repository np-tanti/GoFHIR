#!/usr/bin/env bash
set -euo pipefail

# Start all GoFHIR microservices for local development
# This script creates the socket directory and starts all services

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

# Default socket directory
RUNTIME_DIR="${GOFHIR_RUNTIME_DIR:-/tmp/gofhir}"

# Create runtime directory
mkdir -p "$RUNTIME_DIR"
chmod 0750 "$RUNTIME_DIR"

echo "Starting GoFHIR microservices..."
echo "Runtime directory: $RUNTIME_DIR"
echo ""

# Build binaries if they don't exist
if [ ! -f "$PROJECT_DIR/bin/fhir-core" ]; then
    echo "Building binaries..."
    cd "$PROJECT_DIR"
    make build
fi

# Export environment variables
export GOFHIR_RUNTIME_DIR="$RUNTIME_DIR"
export GOFHIR_AUDIT_HMAC_KEY="${GOFHIR_AUDIT_HMAC_KEY:-$(openssl rand -hex 32)}"
export GOFHIR_JWT_SECRET="${GOFHIR_JWT_SECRET:-$(openssl rand -hex 32)}"
export GOFHIR_DB_PATH="${GOFHIR_DB_PATH:-$PROJECT_DIR/data/gofhir.db}"
export GOFHIR_FHIR_DB_PATH="${GOFHIR_FHIR_DB_PATH:-$PROJECT_DIR/data/gofhir_fhir.db}"
export GOFHIR_GK_DB_PATH="${GOFHIR_GK_DB_PATH:-$PROJECT_DIR/data/gatekeeper.db}"

# Seed databases if needed
if [ ! -f "$GOFHIR_DB_PATH" ]; then
    echo "Seeding databases..."
    "$PROJECT_DIR/bin/seed"
fi

# Start Audit-Service
echo "Starting Audit-Service..."
"$PROJECT_DIR/bin/audit-service" &
AUDIT_PID=$!
echo "  Audit-Service PID: $AUDIT_PID"

# Wait for audit socket
sleep 1

# Start FHIR-Core
echo "Starting FHIR-Core..."
"$PROJECT_DIR/bin/fhir-core" &
FHIR_PID=$!
echo "  FHIR-Core PID: $FHIR_PID"

# Wait for fhir socket
sleep 1

# Start Gateway-Auth
echo "Starting Gateway-Auth..."
"$PROJECT_DIR/bin/gateway-auth" &
AUTH_PID=$!
echo "  Gateway-Auth PID: $AUTH_PID"

# Wait for auth socket
sleep 1

# Start TLS-Proxy (requires TLS certs)
if [ -n "${GOFHIR_TLS_CERT:-}" ] && [ -n "${GOFHIR_TLS_KEY:-}" ]; then
    echo "Starting TLS-Proxy..."
    "$PROJECT_DIR/bin/tls-proxy" &
    TLS_PID=$!
    echo "  TLS-Proxy PID: $TLS_PID"
else
    echo "  TLS-Proxy: SKIPPED (set GOFHIR_TLS_CERT and GOFHIR_TLS_KEY)"
fi

echo ""
echo "All services started!"
echo "  Audit-Service: $AUDIT_PID"
echo "  FHIR-Core: $FHIR_PID"
echo "  Gateway-Auth: $AUTH_PID"
[ -n "${TLS_PID:-}" ] && echo "  TLS-Proxy: $TLS_PID"
echo ""
echo "Socket directory: $RUNTIME_DIR"
echo "  Audit socket: $RUNTIME_DIR/audit.sock"
echo "  FHIR socket: $RUNTIME_DIR/fhir.sock"
echo "  Auth socket: $RUNTIME_DIR/auth.sock"
echo ""
echo "Press Ctrl+C to stop all services"

# Wait for interrupt
trap "echo ''; echo 'Stopping services...'; kill $AUDIT_PID $FHIR_PID $AUTH_PID ${TLS_PID:-} 2>/dev/null; exit 0" INT TERM

wait
