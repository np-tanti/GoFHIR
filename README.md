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
| `GOFHIR_SESSION_IDLE_TIMEOUT` | `900` | Session idle timeout (seconds) |
| `GOFHIR_SESSION_ABSOLUTE_TIMEOUT` | `28800` | Session absolute timeout (seconds) |
| `GOFHIR_DB_ENCRYPTION_KEY` | *(optional)* | AES-256-GCM key (64 hex chars) for at-rest encryption |

### FHIR-Core

| Variable | Default | Description |
|---|---|---|
| `GOFHIR_FHIR_DB_PATH` | `data/gofhir_fhir.db` | FHIR database path |
| `GOFHIR_CORS_ORIGIN` | `` (empty) | CORS origin header (set to specific origin for production) |

---

## Security Features

### Zero Trust
- Every request is **authenticated**, **authorized**, and **audited**
- No direct access to FHIR-Core from outside (only via Gateway-Auth)

### Multi-Factor Authentication (MFA)
- TOTP-based MFA for privileged roles (`admin`, `auditor`, `system`)
- Endpoints: `POST /auth/mfa/setup`, `POST /auth/mfa/verify`
- Login flow requires `totp_code` if MFA is enabled for the user
- TOTP secrets encrypted at rest using AES-256-GCM

### Session Timeout Enforcement
- **Idle timeout**: Sessions expire after `GOFHIR_SESSION_IDLE_TIMEOUT` seconds of inactivity (default: 900s / 15 min)
- **Absolute timeout**: Sessions expire after `GOFHIR_SESSION_ABSOLUTE_TIMEOUT` seconds regardless of activity (default: 28800s / 8h)
- Session activity tracked via `last_active_at` column
- Background cleanup ticker runs every 5 minutes

### Patient-Level Access Control
- **Organizational/ward model**: Nurses see only patients assigned to them
- **Admins/system**: See all patients (no filtering)
- Assignment table: `patient_assignments (patient_id, user_id, role)`
- FHIR search automatically filters by assigned patients for non-admin roles

### At-Rest Encryption
- Field-level encryption for sensitive data (TOTP secrets)
- AES-256-GCM encryption using `GOFHIR_DB_ENCRYPTION_KEY`
- Backward compatible: operates unencrypted if key not set

### Immutable Audit Log
- Cryptographic chain (SHA-256 hash of previous entry)
- HMAC-SHA256 for integrity verification
- Append-only (SQLite triggers prevent UPDATE/DELETE)
- Offline verification tool: `bin/audit-verify`
- **FHIR R4 compliant** AuditEvent structure
- Tracks login/logout with credential type, remote address, and user agent
- Comprehensive audit reports with date range filtering
- Export as FHIR Bundle or with detached HMAC signature

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

## Medical Compliance

### FHIR R4 AuditEvent

GoFHIR implements audit logging according to the **HL7 FHIR R4 AuditEvent** standard:
- **Standard**: HL7 FHIR R4 (Release 4)
- **Resource**: `AuditEvent` (https://www.hl7.org/fhir/auditevent.html)
- **Compliance**: HIPAA-compliant audit trail

### Session Timeout Enforcement

GoFHIR enforces both idle and absolute session timeouts:
- **Idle timeout**: Sessions expire after `GOFHIR_SESSION_IDLE_TIMEOUT` seconds of inactivity (default: 900s / 15 min)
- **Absolute timeout**: Sessions expire after `GOFHIR_SESSION_ABSOLUTE_TIMEOUT` seconds regardless of activity (default: 28800s / 8h)
- **Session tracking**: `last_active_at` column tracks session activity
- **Background cleanup**: Ticker runs every 5 minutes to delete expired/idle sessions

### Multi-Factor Authentication (MFA)

GoFHIR supports TOTP-based MFA for privileged roles:
- **TOTP enforcement**: Required for `admin`, `auditor`, and `system` roles
- **Endpoints**:
  - `POST /auth/mfa/setup` - Generate TOTP secret and provisioning URI
  - `POST /auth/mfa/verify` - Validate TOTP code and enable MFA
  - `POST /auth/login` - Requires `totp_code` field if MFA is enabled
- **Seed**: TOTP secrets auto-generated for `admin-1` and `auditor-1` during seeding
- **Encryption**: TOTP secrets encrypted at rest using AES-256-GCM

### Patient-Level Access Control

GoFHIR implements organizational/ward-based patient access control:
- **Model**: Nurses see only patients assigned to them via `patient_assignments` table
- **Admins/system**: See all patients (no filtering)
- **Table**: `patient_assignments (patient_id, user_id, role)`
- **Endpoints**: `POST /fhir/assign` (admin only) to assign patients
- **Search filtering**: Non-admin FHIR searches automatically filter by assigned patients

### At-Rest Encryption

Sensitive data is encrypted at rest:
- **Field-level encryption**: TOTP secrets encrypted using AES-256-GCM
- **Key management**: `GOFHIR_DB_ENCRYPTION_KEY` (32-byte hex) via environment or secrets manager
- **Backward compatible**: If key not set, operates unencrypted

### Audit Export

GoFHIR supports standards-compliant audit export:
- **FHIR Bundle**: `GET /audit/export?format=fhir` returns Bundle of AuditEvent resources
- **Detached signature**: `GET /audit/export?format=detached` includes HMAC-SHA256 signature in `X-Audit-Signature` header
- **Compliance**: Ready for off-site archival and regulatory review

### Audit Report Format

The `/audit/report` endpoint generates comprehensive reports:

```json
{
  "report_period": {
    "start": "2026-01-01T00:00:00Z",
    "end": "2026-01-31T23:59:59Z"
  },
  "summary": {
    "total_entries": 1500,
    "unique_users": 25,
    "login_success": 1450,
    "login_failure": 50,
    "actions": {
      "login": 500,
      "logout": 480,
      "fhir.read": 300,
      "fhir.create": 120,
      "fhir.update": 80
    }
  },
  "compliance": {
    "standard": "FHIR R4",
    "hipaa_compliant": true,
    "audit_chain_intact": true
  }
}
```

### Audit Chain Verification

The audit log uses cryptographic chaining to ensure integrity:
- Each entry contains a SHA-256 hash of the previous entry
- HMAC-SHA256 signatures prevent tampering
- Append-only database triggers prevent modification
- Use `/audit/verify` to verify chain integrity

---

## API Endpoints

### FHIR R4 API (via Gateway-Auth → FHIR-Core)

| Method | Path | Description |
|---|---|---|
| `GET` | `/fhir/` | CapabilityStatement |
| `POST` | `/fhir/` | Create Patient/Observation |
| `GET` | `/fhir/{type}/{id}` | Read resource (access controlled) |
| `PUT` | `/fhir/{type}/{id}` | Update resource (access controlled) |
| `DELETE` | `/fhir/{type}/{id}` | Delete resource (soft, access controlled) |
| `GET` | `/fhir/{type}` | Search resources (filtered by patient assignments) |
| `GET` | `/fhir/{type}/{id}/_history` | Version history |
| `GET` | `/fhir/{type}/{id}/_history/{version}` | Read specific version |

**Note**: Patient-level access control enforced for `Patient` and `Observation` resources. Non-admin users see only assigned patients.

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
| `POST` | `/auth/login` | User login (audit logged, MFA required for privileged roles) |
| `POST` | `/auth/logout` | User logout (audit logged) |
| `POST` | `/auth/mfa/setup` | Generate TOTP secret (requires auth) |
| `POST` | `/auth/mfa/verify` | Validate TOTP code and enable MFA |
| `GET` | `/audit/entries` | Read audit log with filtering |
| `GET` | `/audit/report` | Generate comprehensive audit report |
| `GET` | `/audit/verify` | Verify audit chain integrity |
| `GET` | `/audit/export` | Export audit as FHIR Bundle (`?format=fhir`) or with detached signature (`?format=detached`) |

#### Audit Endpoints

**List audit entries:**
```bash
# Basic listing
curl -X GET https://localhost/audit/entries \
  -H "Authorization: Bearer <token>"

# Filter by action and user
curl -X GET "https://localhost/audit/entries?action=login&actor_id=user-123" \
  -H "Authorization: Bearer <token>"

# Get FHIR-formatted entries
curl -X GET "https://localhost/audit/entries?format=fhir" \
  -H "Authorization: Bearer <token>"
```

**Generate audit report:**
```bash
# Last 30 days (default)
curl -X GET https://localhost/audit/report \
  -H "Authorization: Bearer <token>"

# Custom date range
curl -X GET "https://localhost/audit/report?start=2026-01-01T00:00:00Z&end=2026-01-31T23:59:59Z" \
  -H "Authorization: Bearer <token>"

# Filter by action
curl -X GET "https://localhost/audit/report?action=login" \
  -H "Authorization: Bearer <token>"
```

**Verify audit chain:**
```bash
curl -X GET https://localhost/audit/verify \
  -H "Authorization: Bearer <token>"
```

#### Audit Event Structure (FHIR R4 Compliant)

The audit system now logs events according to FHIR R4 AuditEvent standards:

```json
{
  "resourceType": "AuditEvent",
  "type": {
    "system": "http://terminology.hl7.org/CodeSystem/audit-event-type",
    "code": "rest",
    "display": "RESTful Interaction"
  },
  "subtype": [{
    "code": "login",
    "display": "Login"
  }],
  "action": "E",
  "recorded": "2026-01-15T10:30:00Z",
  "outcome": "0",
  "outcomeDesc": "Login successful",
  "agent": [{
    "requestor": true,
    "userId": {
      "system": "urn:ietf:rfc:3986",
      "value": "user-123"
    },
    "name": "nurse-1",
    "network": {
      "address": "192.168.1.100",
      "type": "1"
    }
  }],
  "source": {
    "type": [{
      "code": "3",
      "display": "Web Server"
    }]
  },
  "entity": [{
    "detail": [
      {"type": "credentialType", "valueString": "password"},
      {"type": "sessionId", "valueString": "abc123"},
      {"type": "userAgent", "valueString": "Mozilla/5.0..."}
    ]
  }]
}
```

#### Credential Types Tracked

| Type | Description |
|------|-------------|
| `password` | Username/password authentication |
| `jwt` | JWT token authentication |
| `api-key` | API key authentication |
| `mtls` | Mutual TLS client certificate |
| `session` | Session cookie authentication |

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

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

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
