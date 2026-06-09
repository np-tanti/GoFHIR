# GoFHIR Architecture & Integration Plan

## Philosophy

- Zero trust: every request is authenticated, authorized, audited
- Stdlib-only networking: `crypto/tls`, `net/http`, `crypto/hmac` — no framework deps
- Immutable logs: cryptographic chain guarantees audit integrity
- Minimal surface: single statically-linked binary, no shell, no runtime deps

## System Context

```
                    ┌──────────────────────────────┐
                    │       NixOS Kiosk (HW)        │
                    │  (Cage + Chromium kiosk mode) │
                    └──────────┬───────────────────┘
                               │ HTTPS (TLS 1.3)
                               ▼
               ┌───────────────────────────────────────┐
               │          gofhir (single binary)        │
               │                                       │
               │  ┌──────────┐  ┌─────────────────┐    │
               │  │ TLS Term │  │   Gatekeeper     │    │
               │  │ (mTLS)   │→ │ (auth+rl+rbac)   │    │
               │  └──────────┘  └────────┬────────┘    │
               │                         │              │
               │  ┌──────────────┐       │              │
               │  │   Auditor    │←──────┘              │
               │  │ (write-only) │                      │
               │  └──────┬───────┘                      │
               │         │                              │
               │  ┌──────▼───────┐  ┌───────────────┐   │
               │  │  FHIR-Core   │  │ Triage (mem)  │   │
               │  │ (CRUD + DB)  │  │ + SSE Hub     │   │
               │  └──────┬───────┘  └───────┬───────┘   │
               │         │                  │           │
               │  ┌──────▼──────────────────▼───────┐   │
               │  │  Static Web (embedded via embed)  │   │
               │  │  / → ER Triage Board              │   │
               │  │  /reception → Patient Registration│   │
               │  └──────────────────────────────────┘   │
               └───────────────────────────────────────┘
```

All modules live in a **single Go module** at `github.com/graphic/gofhir`. They communicate via internal Go interfaces — no RPC, no HTTP between modules.

---

## 1. TLS Termination

**Purpose**: Edge termination of TLS 1.3, client certificate validation (mTLS), ACME auto-cert management.

**Structure**:
```
internal/
  tls/
    terminator.go      # net/http server with tls.Config
    certstore.go       # Disk-based cert store with hot-reload
    acme.go            # Let's Encrypt auto-renewal (golang.org/x/crypto/acme/autocert)
```

**Interface**:
```go
type Terminator interface {
    ServeTLS(handler http.Handler) error
    ReloadCert() error
    ClientCAs() *x509.CertPool
}
```

**Integration**:
- Wraps the entire HTTP pipeline with `crypto/tls`
- Extracts client cert CN → passes as `X-Client-CN` header to Gatekeeper
- Supports mutual TLS (mTLS) for machine-to-machine auth

---

## 2. Gatekeeper

**Purpose**: Authentication, authorization, rate limiting.

**Structure**:
```
internal/
  gatekeeper/
    gatekeeper.go       # Middleware chain
    auth.go             # JWT/API key verification
    rbac.go             # Role-based access (nurse, admin, system, auditor)
    ratelimit.go        # Per-IP, per-role token bucket
    store.go            # Session/API key store (SQLite-backed)
```

**Auth methods** (configurable):
| Method | Use Case |
|---|---|
| Bearer JWT (Ed25519) | Dashboard users, delegated auth |
| `X-API-Key` header | Machine-to-machine, kiosk |
| mTLS client cert | Internal service auth |
| Session cookie | Nurse dashboard (password + bcrypt) |

**RBAC roles**:
```
admin       → full access, auditor read
nurse       → read/write patients
system      → machine accounts, API key only
auditor     → read-only audit log
```

**Rate limiting**:
- Token bucket per IP (std `golang.org/x/time/rate`)  
- Burst limit: 20, Sustained: 5/s for unauth, 50/s for authenticated

**Integration**:
- Wraps all HTTP handlers
- On auth success, enriches context with `ctxutil.User{ID, Role, SessionID}`
- Public paths: `/`, `/reception`, `/static/`, `/auth/login`, `/auth/logout`, `/live`, `/ready`

---

## 3. Immutable Auditor

**Purpose**: Write-only append log with cryptographic chaining.

**Structure**:
```
internal/
  auditor/
    chain.go            # Cryptographic chain logic
    entry.go            # Log entry schema
    store.go            # SQLite-backed append-only store
```

**Cryptographic chain**:
```go
type Entry struct {
    Seq       uint64    // Monotonic sequence
    PrevHash  [32]byte  // SHA256 of previous entry
    Timestamp int64     // Unix nano
    Action    string    // "login", "patient.create", etc.
    ActorID   string
    SessionID string
    Payload   []byte
    HMAC      [32]byte  // HMAC-SHA256(key, ...)
}
```

**Verifier binary** (`cmd/audit-verify`): Reads the audit DB and reports the first broken chain link.

---

## 4. FHIR-Core

**Purpose**: FHIR R4 resource processing, versioned storage, search.

**Structure**:
```
internal/
  fhir/
    storage/
      store.go           # Versioned SQLite (CGO-free with modernc.org/sqlite)
      store_test.go
      search_test.go
```

**Key operations**: Create, Read (latest + specific version), Update, SoftDelete, Search (by _id, name, code, subject, _lastUpdated), History.

**Integration**: FHIR-Core handler is wrapped by Gatekeeper middleware. User info extracted from context via `ctxutil.UserFrom(ctx)`.

---

## 5. Triage Board (In-Memory + SSE)

**Purpose**: Real-time Emergency Department triage board with Server-Sent Events.

**Structure**:
```
internal/
  triage/
    store.go       # Thread-safe in-memory patient board
    sse.go         # SSE hub for live broadcasting
```

**TriageStore**: In-memory map of checked-in patients. Each patient has:
- PatientID, PatientName, Gender, Age
- ESI level (1-5, auto-computed from vitals)
- Chief complaint, check-in/out timestamps
- Current vital signs (BP, HR, RR, SpO2, temp)

**SSE Hub**: Manages client subscriptions. On every check-in, check-out, ESI update, or vitals recording, broadcasts an event to all connected clients. Slow clients are dropped after buffer overflow.

**API endpoints**:

| Method | Path | Description |
|---|---|---|
| GET | `/triage/board` | All active (checked-in) patients |
| POST | `/triage/checkin` | Check in a FHIR patient by ID |
| POST | `/triage/checkout` | Check out / discharge a patient |
| POST | `/triage/esi` | Override ESI score for a patient |
| GET | `/events` | SSE stream (checkin, checkout, esi-update events) |

**SSE event types**:
- `checkin` — patient added to board
- `checkout` — patient discharged
- `esi-update` — ESI level changed
- `connected` — confirmation of SSE connection

**Integration**: Triage handler references the FHIR store to resolve patient details on check-in. No persistence — board resets on gateway restart.

---

## 6. Reception (Patient Registration)

**Purpose**: Registration desk UI for creating new FHIR Patient resources.

**Structure**:
```
web/
  er-dashboard/
    reception.html     # Registration form page
    reception.js       # Form handling, FHIR POST, patient search
```

**Features**:
- Full patient registration form (identity, demographics, contact, emergency contact)
- Live FHIR JSON preview card updates on every keystroke
- POST to `/fhir/` to create the resource
- Patient search sidebar (load existing patients into form)
- Validation of required fields before submission

**API endpoints consumed**:

| Method | Path | Description |
|---|---|---|
| POST | `/fhir/` | Create Patient resource |
| GET | `/fhir/patient` | List/search existing patients |
| GET | `/fhir/patient?_id={id}` | Search by patient ID |
| POST | `/auth/login` | Session login |

---

## 7. ER Nurse Dashboard

**Purpose**: Fast triage dashboard for Emergency Room nurses.

**Structure**:
```
web/
  er-dashboard/
    index.html            # SPA shell with login + triage board
    app.js                # Vanilla JS: SSE, check-in/out, vitals, ESI scoring
    triage.css            # ESI 1-5 acuity color coding
```

**Frontend** (vanilla JS, zero deps):
- ESI 1-5 priority columns with color-coded patient cards
- Real-time updates via SSE (`/events`)
- Patient search (board + FHIR store)
- Vitals recording (POST to `/fhir/` + auto-ESI computed server-side)
- Check-in / check-out workflow
- Inline ESI override dropdown
- Navigation link to Reception page

**ESI Auto-Scoring** (computed on vitals submit):
| ESI | Criteria |
|---|---|
| 1 | SpO2 < 90%, or SBP < 90 with HR > 120, or RR < 8 or > 30, or HR > 180 or < 40 |
| 2 | SpO2 90-93%, or SBP < 100, or RR > 24, or HR > 140, or temp > 39.5°C |
| 3 | Default — does not meet 1, 2, 4, or 5 criteria |
| 4 | Normal vitals (SBP 100-139, HR 60-100, RR 12-20, SpO2 ≥ 95%, temp 36.5-38.0°C) |
| 5 | No distress, minimal workup needed |

---

## 8. Seed Data

**File**: `cmd/seed/main.go`

Creates three test users with bcrypt-passwords, API keys, sample FHIR resources, and an audit seed entry:

| Username | Password | Role |
|---|---|---|
| `nurse-1` | `nurse123` | nurse |
| `admin-1` | `admin123` | admin |
| `auditor-1` | `auditor123` | auditor |

Also creates `pat-001`, `pat-002` (Patient resources) and `obs-001`, `obs-002` (Observation resources). All operations are idempotent.

---

## Project Structure

```
GoFHIR/
├── go.mod
├── webui_root.go                # //go:embed web/er-dashboard (package webui)
├── ARCHITECTURE.md
│
├── cmd/
│   ├── gateway/main.go          # Production entry point — wires all modules
│   ├── seed/main.go             # Seed test data (users, FHIR resources)
│   ├── migrate/main.go          # DB schema migration
│   └── audit-verify/main.go     # Offline audit chain integrity checker
│
├── internal/
│   ├── tls/
│   │   └── terminator.go        # TLS 1.3 with optional mTLS
│   │
│   ├── gatekeeper/
│   │   ├── gatekeeper.go        # Middleware: auth + rate limit + RBAC
│   │   ├── auth.go              # JWT (Ed25519), bcrypt, API key generation
│   │   ├── rbac.go              # Role/permission definitions
│   │   ├── ratelimit.go         # Token bucket per IP
│   │   ├── store.go             # SQLite store: users, sessions, API keys
│   │   ├── auth_test.go
│   │   ├── rbac_test.go
│   │   ├── ratelimit_test.go
│   │   └── store_test.go
│   │
│   ├── auditor/
│   │   ├── chain.go             # SHA256 chain + HMAC
│   │   ├── entry.go             # Entry schema
│   │   ├── store.go             # Append-only SQLite
│   │   ├── chain_test.go
│   │   └── store_test.go
│   │
│   ├── fhir/
│   │   └── storage/
│   │       ├── store.go         # Versioned FHIR resource SQLite store
│   │       ├── store_test.go
│   │       └── search_test.go
│   │
│   ├── triage/
│   │   ├── store.go             # In-memory triage board
│   │   └── sse.go               # Server-Sent Events hub
│   │
│   ├── handler/
│   │   ├── fhir.go              # FHIR HTTP handlers (CRUD + search)
│   │   ├── auth.go              # POST /auth/login (JSON + form-encoded)
│   │   ├── auth.go              # POST /auth/logout
│   │   ├── audit.go             # GET /audit/entries
│   │   ├── health.go            # GET /live, GET /ready, CORS middleware
│   │   ├── triage.go            # Check-in/out/ESI/board handlers
│   │   └── static.go            # Embedded dashboard + reception serving
│   │
│   ├── ctxutil/
│   │   └── context.go           # User{ID, Role, SessionID} in request context
│   │
│   └── config/
│       └── config.go            # Environment-based config (16 vars)
│
├── web/
│   └── er-dashboard/
│       ├── index.html           # ER Triage Board SPA
│       ├── reception.html       # Patient Registration SPA
│       ├── app.js               # Triage board logic + SSE client
│       ├── reception.js         # Registration form logic
│       └── triage.css           # ESI 1-5 colors + layout
│
├── data/                        # SQLite DB mount point (gitignored)
├── flake.nix                    # Nix dev shell (Go 1.26, gopls, golangci-lint)
├── Makefile
└── .gitignore
```

---

## Data Flow: Patient Check-In

```
Nurse opens ER Dashboard (/)
  │
  │ GET /  → loads index.html + app.js
  │ POST /auth/login → session cookie set
  ▼
┌─────────────────────────────────────────────┐
│ Gatekeeper Middleware                        │
│ • Authenticate via session cookie            │
│ • RBAC: nurse → allow triage:write          │
│ • Rate limit: check token bucket            │
│ → Inject user context                       │
└────────────────┬────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────┐
│ Auditor Middleware                           │
│ → Log triage.checkin action                 │
└────────────────┬────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────┐
│ TriageHandler.CheckIn                        │
│ 1. Read Patient from FHIR store by ID       │
│ 2. Extract name, age, gender                │
│ 3. Add to in-memory TriageStore             │
│ 4. Broadcast "checkin" event via SSE hub    │
│ 5. Return triage.Patient JSON               │
└─────────────────────────────────────────────┘
                 │
                 ▼
           SSE broadcast → all connected browsers update board
```

---

## Dependencies

| Module | External Deps | Rationale |
|---|---|---|
| `crypto/tls` | stdlib | TLS 1.3 |
| `net/http` | stdlib | HTTP server |
| `database/sql` | stdlib | DB interface |
| `modernc.org/sqlite` | **pure Go** | SQLite driver (CGO-free) |
| `golang.org/x/time/rate` | **x库** | Token bucket rate limiter |
| `golang.org/x/crypto` | **x库** | Ed25519, bcrypt |
| `encoding/json` | stdlib | JSON |
| `crypto/hmac` + `crypto/sha256` | stdlib | Audit chain |
| `embed` | stdlib | Static file embedding |

**Total external**: 3 packages. **Router**: std `net/http` mux (Go 1.22+ pattern matching).

---

## Security Properties

| Threat | Mitigation |
|---|---|
| TLS eavesdropping | TLS 1.3, HSTS, mTLS for machine clients |
| Token theft | Short-lived session (8h), HttpOnly cookies, Secure flag |
| Audit tampering | SHA256 chain + HMAC per entry, offline verifier |
| Rate limit bypass | Token bucket per IP, per session |
| SQL injection | Parameterized queries throughout |
| Supply chain | `go mod verify`, no CGO |
| Log forgery | HMAC key in env only, never logged |

---

## Launch

```bash
nix develop

mkdir -p data
KEY=$(head -c 32 /dev/urandom | od -A n -t x1 | tr -d ' \n')
GOFHIR_AUDIT_HMAC_KEY=$KEY go run ./cmd/seed
GOFHIR_AUDIT_HMAC_KEY=$KEY go run ./cmd/gateway
```

Then open `http://localhost:8080` (ER Triage Board) or `http://localhost:8080/reception` (Registration Desk). Log in with `nurse-1` / `nurse123`.

---

## Test

```bash
CGO_ENABLED=0 go test ./... -count=1
```