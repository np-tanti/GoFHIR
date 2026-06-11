# GoFHIR - Micro-Gateway Architecture

![Version](https://img.shields.io/badge/version-0.2.0-blue)
![FHIR](https://img.shields.io/badge/FHIR-R4-orange)
![Go](https://img.shields.io/badge/Go-1.25+-green)

## Overview

GoFHIR is a FHIR R4 medical data gateway designed for Emergency Department triage. It has been refactored from a monolithic binary into **4 specialized microservices** that communicate via **Unix domain sockets** for secure local IPC.

## Capability Highlight: Real-Time Triage Board

GoFHIR provides a real-time Emergency Department triage board with Server-Sent Events (SSE) for live updates. Multiple users can monitor patient flow simultaneously.

**Check in a patient:**
```bash
curl -X POST https://localhost/triage/checkin \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"patient_id": "pat-001", "chief_complaint": "Chest pain"}'
```

**Subscribe to live updates:**
```javascript
const eventSource = new EventSource('/events');
eventSource.onmessage = (e) => {
  const data = JSON.parse(e.data);
  console.log('Triage update:', data);
};
```

The triage board automatically organizes patients by ESI (Emergency Severity Index) levels 1-5, with real-time updates pushed to all connected clients via SSE.

---

## Architecture

```
┌──────────┐  Unix socket  ┌──────────────┐  Unix socket  ┌────────────┐
│ TLS-Proxy ├──────────────►│ Gateway-Auth ├──────────────►│ FHIR-Core │
│ :443     │  /run/gofhir/ │ :8081        │  /run/gofhir/ │ :8082     │
└──────────┘  proxy.sock   └──────┬───────┘  fhir.sock   └────┬───────┘
                                    │                            │
                             Unix socket                   Unix socket
                              /run/gofhir/                 /run/gofhir/
                               audit.sock                   audit.sock
                                    │                            │
                                    ▼                            ▼
                             ┌──────────────┐            ┌───────┴───────┐
                             │ Audit-Service │            │  (audit log)  │
                             │ :8083         │            └───────────────┘
                             └──────────────┘
```

### Services

| Service | Binary | Purpose | Socket |
|---|---|---|---|
| **TLS-Proxy** | `bin/tls-proxy` | TLS 1.3 termination, mTLS support | Forwards to `auth.sock` |
| **Gateway-Auth** | `bin/gateway-auth` | JWT/OIDC verification, rate limiting, RBAC | Listens on `auth.sock`, forwards to `fhir.sock` |
| **FHIR-Core** | `bin/fhir-core` | FHIR R4 API, versioned storage, triage board, SSE | Listens on `fhir.sock` |
| **Audit-Service** | `bin/audit-service` | Immutable audit log with cryptographic chaining | Listens on `audit.sock` |

---

## Quick Start

### Prerequisites

- Go 1.26+ (or use Nix dev shell: `nix develop`)
- SQLite3
- OpenSSL (for generating keys)

### Build

```bash
nix develop  # Enter Nix dev shell
make build      # Build all binaries to bin/
```

### Run All Services (Development)

```bash
export GOFHIR_AUDIT_HMAC_KEY=$(openssl rand -hex 32)
export GOFHIR_JWT_SECRET=$(openssl rand -hex 32)

# Seed databases
bin/seed

# Start all services
./scripts/start-all.sh
```

### Manual Start (Production)

```bash
# 1. Create socket directory
sudo mkdir -p /run/gofhir
sudo chown $(whoami):$(whoami) /run/gofhir
sudo chmod 0750 /run/gofhir

# 2. Start Audit-Service (first, others depend on it)
GOFHIR_DB_PATH=data/gofhir.db \
GOFHIR_AUDIT_HMAC_KEY=... \
./bin/audit-service &

# 3. Start FHIR-Core
GOFHIR_FHIR_DB_PATH=data/gofhir_fhir.db \
./bin/fhir-core &

# 4. Start Gateway-Auth
GOFHIR_GK_DB_PATH=data/gatekeeper.db \
GOFHIR_JWT_SECRET=... \
./bin/gateway-auth &

# 5. Start TLS-Proxy (requires TLS certs)
GOFHIR_TLS_CERT=/path/to/cert.pem \
GOFHIR_TLS_KEY=/path/to/key.pem \
./bin/tls-proxy &
```

---

## Environment Variables

### All Services

| Variable | Default | Description |
|---|---|---|
| `GOFHIR_RUNTIME_DIR` | `/run/gofhir` | Directory for Unix sockets |

### TLS-Proxy

| Variable | Default | Description |
|---|---|---|
| `GOFHIR_TLS_PORT` | `443` | HTTPS listen port |
| `GOFHIR_TLS_CERT` | **required** | Path to TLS certificate |
| `GOFHIR_TLS_KEY` | **required** | Path to TLS private key |
| `GOFHIR_TLS_CA` | *optional* | Client CA for mTLS |
| `GOFHIR_MTLS_ENABLED` | `false` | Enable mTLS |

### Gateway-Auth

| Variable | Default | Description |
|---|---|---|
| `GOFHIR_GK_DB_PATH` | `data/gatekeeper.db` | Gatekeeper database path |
| `GOFHIR_JWT_SECRET` | *optional* | Ed25519 secret (32 hex bytes) |
| `GOFHIR_AUDIT_HMAC_KEY` | **required** | HMAC key for audit log |
| `GOFHIR_RL_UNAUTH` | `10` | Rate limit for unauthenticated requests |
| `GOFHIR_RL_AUTH` | `100` | Rate limit for authenticated requests |
| `GOFHIR_RL_BURST` | `50` | Burst limit |

### Audit-Service

| Variable | Default | Description |
|---|---|---|
| `GOFHIR_DB_PATH` | `data/gofhir.db` | Audit database path |
| `GOFHIR_AUDIT_HMAC_KEY` | **required** | HMAC key for audit chain |

### FHIR-Core

| Variable | Default | Description |
|---|---|---|
| `GOFHIR_FHIR_DB_PATH` | `data/gofhir_fhir.db` | FHIR database path |
| `GOFHIR_CORS_ORIGIN` | `*` | CORS origin header |

---

## Security Features

### Zero Trust
- Every request is **authenticated**, **authorized**, and **audited**
- No direct access to FHIR-Core from outside (only via Gateway-Auth)

### Immutable Audit Log
- Cryptographic chain (SHA-256 hash of previous entry)
- HMAC-SHA256 for integrity verification
- Append-only (SQLite triggers prevent UPDATE/DELETE)
- Offline verification tool: `bin/audit-verify`

### Fail-Closed
- Gateway-Auth **refuses all requests** if Audit-Service is unreachable
- Patient data must never be accessed without an audit trail

### Unix Socket Permissions
- Sockets created with `0660` permissions
- Only the `gofhir` group can access sockets
- No network exposure

### TLS 1.3
- Enforced by TLS-Proxy
- Optional mTLS for machine-to-machine auth

---

## API Endpoints

### FHIR R4 API (via Gateway-Auth → FHIR-Core)

| Method | Path | Description |
|---|---|---|
| `GET` | `/fhir/` | CapabilityStatement |
| `POST` | `/fhir/` | Create Patient |
| `GET` | `/fhir/Patient/{id}` | Read Patient |
| `PUT` | `/fhir/Patient/{id}` | Update Patient |
| `DELETE` | `/fhir/Patient/{id}` | Delete Patient (soft) |
| `GET` | `/fhir/Patient` | Search Patients |
| `GET` | `/fhir/Patient/{id}/_history` | Version history |

### Triage Board (via Gateway-Auth → FHIR-Core)

| Method | Path | Description |
|---|---|---|
| `GET` | `/triage/board` | All active patients |
| `POST` | `/triage/checkin` | Check in patient |
| `POST` | `/triage/checkout` | Check out patient |
| `POST` | `/triage/esi` | Set ESI level (1-5) |
| `GET` | `/events` | SSE stream |

### Audit (via Gateway-Auth → Audit-Service)

| Method | Path | Description |
|---|---|---|
| `POST` | `/audit/event` | Append audit entry (internal) |
| `GET` | `/audit/entries` | Read audit log (auditor-only) |

### Health Checks

| Method | Path | Service |
|---|---|---|
| `GET` | `/live` | All services |
| `GET` | `/ready` | All services |

---

## Development

### Project Structure

```
GoFHIR/
├── cmd/
│   ├── tls-proxy/main.go          # TLS termination
│   ├── gateway-auth/main.go        # Auth + rate limiting
│   ├── audit-service/main.go       # Immutable audit
│   ├── fhir-core/main.go          # FHIR API + triage
│   ├── gateway/main.go             # Monolithic (deprecated)
│   ├── seed/main.go               # Seed test data
│   ├── migrate/main.go            # DB migrations
│   └── audit-verify/main.go       # Offline audit verification
│
├── internal/
│   ├── netutil/unixsocket.go      # Unix socket transport
│   ├── tls/                      # TLS termination logic
│   ├── gatekeeper/                # Auth, RBAC, rate limiting
│   ├── auditor/                   # Audit chain + storage
│   ├── fhir/storage/             # FHIR resource storage
│   ├── triage/                    # Triage board + SSE
│   └── config/config.go           # Environment-based config
│
├── web/
│   └── er-dashboard/             # Static web UI
│       ├── index.html             # ER Triage Board
│       ├── reception.html         # Patient Registration
│       ├── app.js                 # Triage logic + SSE
│       └── reception.js           # Registration form logic
│
├── scripts/
│   └── start-all.sh              # Start all services (dev)
│
├── data/                          # SQLite databases
├── flake.nix                     # Nix dev shell
├── Makefile                      # Build targets
└── README.md
```

### Testing

```bash
# Run all tests
nix develop -c go test ./... -count=1

# Build and test
make build
make test

# Lint
make lint
```

### Seeding Test Data

```bash
export GOFHIR_AUDIT_HMAC_KEY=$(openssl rand -hex 32)

bin/seed
```

Creates:
- Users: `nurse-1/nurse123` (nurse), `admin-1/admin123` (admin), `auditor-1/auditor123` (auditor)
- API keys for each user
- Sample patients: `pat-001`, `pat-002`
- Sample observations: `obs-001`, `obs-002`

---

## Migration from Monolith

The monolithic `cmd/gateway/main.go` is still available but deprecated. To migrate:

1. **Stop the monolith**
   ```bash
   pkill -f 'bin/gateway'
   ```

2. **Start microservices**
   ```bash
   ./scripts/start-all.sh
   ```

3. **Verify functionality**
   - Open `https://localhost/` (ER Triage Board)
   - Open `https://localhost/reception` (Patient Registration)
   - Log in with `nurse-1/nurse123`

---

## License

[Add your license here]

---

## Contributing

1. Fork the repository
2. Create a feature branch
3. Run tests: `make test`
4. Run linter: `make lint`
5. Submit a pull request

---

## Support

For issues and questions:
- GitHub Issues: [link]
- Documentation: See `ARCHITECTURE.md` for detailed design
