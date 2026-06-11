#!/usr/bin/env bash
set -euo pipefail

# Test script for GoFHIR microservices
# This script tests the inter-service communication

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

echo "=== GoFHIR Microservices Test ==="
echo ""

# Create temp directory for sockets
TEST_DIR="/tmp/gofhir_test_$$"
mkdir -p "$TEST_DIR"
chmod 0750 "$TEST_DIR"

cleanup() {
    echo ""
    echo "Cleaning up..."
    rm -rf "$TEST_DIR"
    pkill -f "bin/(audit-service|fhir-core|gateway-auth|tls-proxy)" 2>/dev/null || true
}
trap cleanup EXIT

# Generate keys using Go
echo "Generating keys..."
HMAC_KEY=$(cd "$PROJECT_DIR" && nix develop -c go run -e 'package main; import ("crypto/rand"; "fmt"; "encoding/hex"); func main() { b := make([]byte, 32); rand.Read(b); fmt.Println(hex.EncodeToString(b)) }' 2>/dev/null || head -c 32 /dev/urandom | xxd -p -c 64)

if [ -z "$HMAC_KEY" ]; then
    echo "Using random HMAC key"
    HMAC_KEY=$(head -c 32 /dev/urandom | xxd -p -c 64 2>/dev/null || echo "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899")
fi

JWT_SECRET=$(head -c 32 /dev/urandom | xxd -p -c 64 2>/dev/null || echo "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")

export GOFHIR_RUNTIME_DIR="$TEST_DIR"
export GOFHIR_AUDIT_HMAC_KEY="$HMAC_KEY"
export GOFHIR_JWT_SECRET="$JWT_SECRET"
export GOFHIR_DB_PATH="$TEST_DIR/gofhir.db"
export GOFHIR_FHIR_DB_PATH="$TEST_DIR/gofhir_fhir.db"
export GOFHIR_GK_DB_PATH="$TEST_DIR/gatekeeper.db"

echo "Socket directory: $TEST_DIR"
echo ""

# Build if needed
if [ ! -f "$PROJECT_DIR/bin/audit-service" ]; then
    echo "Building binaries..."
    cd "$PROJECT_DIR"
    make build
fi

# Seed databases
echo "Seeding databases..."
cd "$PROJECT_DIR"
./bin/seed

# Start Audit-Service
echo "Starting Audit-Service..."
./bin/audit-service &
AUDIT_PID=$!
sleep 1

# Check if Audit-Service is running
if ! kill -0 $AUDIT_PID 2>/dev/null; then
    echo "ERROR: Audit-Service failed to start"
    exit 1
fi
echo "  Audit-Service PID: $AUDIT_PID"

# Test Audit-Service health
echo "Testing Audit-Service health..."
if ! curl -s --unix-socket "$TEST_DIR/audit.sock" http://localhost/live | grep -q "OK"; then
    echo "ERROR: Audit-Service health check failed"
    exit 1
fi
echo "  /live: OK"

# Start FHIR-Core
echo "Starting FHIR-Core..."
./bin/fhir-core &
FHIR_PID=$!
sleep 1

# Check if FHIR-Core is running
if ! kill -0 $FHIR_PID 2>/dev/null; then
    echo "ERROR: FHIR-Core failed to start"
    exit 1
fi
echo "  FHIR-Core PID: $FHIR_PID"

# Test FHIR-Core health
echo "Testing FHIR-Core health..."
if ! curl -s --unix-socket "$TEST_DIR/fhir.sock" http://localhost/live | grep -q "OK"; then
    echo "ERROR: FHIR-Core health check failed"
    exit 1
fi
echo "  /live: OK"

# Start Gateway-Auth
echo "Starting Gateway-Auth..."
./bin/gateway-auth &
AUTH_PID=$!
sleep 1

# Check if Gateway-Auth is running
if ! kill -0 $AUTH_PID 2>/dev/null; then
    echo "ERROR: Gateway-Auth failed to start"
    exit 1
fi
echo "  Gateway-Auth PID: $AUTH_PID"

# Test Gateway-Auth health
echo "Testing Gateway-Auth health..."
if ! curl -s --unix-socket "$TEST_DIR/auth.sock" http://localhost/live | grep -q "OK"; then
    echo "ERROR: Gateway-Auth health check failed"
    exit 1
fi
echo "  /live: OK"

echo ""
echo "=== All services started successfully ==="
echo ""
echo "Socket files:"
ls -la "$TEST_DIR"/*.sock 2>/dev/null || echo "  (no sockets found)"

echo ""
echo "Testing inter-service communication..."

# Test FHIR API through Gateway-Auth
echo "  Testing FHIR CapabilityStatement..."
RESP=$(curl -s --unix-socket "$TEST_DIR/auth.sock" http://localhost/fhir/)
if echo "$RESP" | grep -q "CapabilityStatement"; then
    echo "    OK: CapabilityStatement returned"
else
    echo "    ERROR: CapabilityStatement not returned"
    echo "    Response: $RESP"
fi

echo ""
echo "=== Test complete ==="
echo ""
echo "To manually test:"
echo "  export GOFHIR_RUNTIME_DIR=$TEST_DIR"
echo "  curl --unix-socket $TEST_DIR/auth.sock http://localhost/live"
echo ""
echo "Press Ctrl+C to stop all services and exit."

# Wait for interrupt
wait
