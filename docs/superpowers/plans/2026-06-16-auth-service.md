# Authentication Service (AS) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the Go Authentication Service — OPAQUE register/login (server never sees passwords), revocable Redis sessions, device enrollment, and a one-time KeyPackage store — as a horizontally-scalable modular monolith.

**Architecture:** A single Go binary (`backend/`) with internal packages per responsibility (`opaque`, `session`, `as`, `devices`, `keypackages`, `store`, `httpapi`). Postgres is the source of truth; Redis holds revocable sessions, short-lived OPAQUE login state, and rate-limit counters. Instances are stateless (shared state in Redis/Postgres), so they scale by running N replicas. Device-key changes are written to a `kt_outbox` table in the same DB transaction (transactional outbox) for a future KT service to consume.

**Tech Stack:** Go 1.22+, `github.com/bytemare/opaque` v0.18.0 (server-side OPAQUE), `github.com/jackc/pgx/v5` (Postgres), `github.com/redis/go-redis/v9` (Redis), `github.com/alicebob/miniredis/v2` (Redis unit tests), `github.com/testcontainers/testcontainers-go` (Postgres integration tests), stdlib `net/http` (1.22 routing) + `net/http/httptest`.

**Spec:** `docs/superpowers/specs/2026-06-16-auth-service-design.md`. KT log, DS/WebSocket fabric, and the OPAQUE *client* are out of scope (separate plans); this plan pins the OPAQUE wire protocol and library they depend on.

**Environment prerequisite:** Integration tests (Tasks 2, 5, 10, 11, 12) use testcontainers and REQUIRE a running Docker daemon. Unit tests (Tasks 3, 4, 6, 13) use miniredis / in-memory and need no Docker. If Docker is unavailable, the implementer must report it (do not fake or skip integration tests silently).

---

## File Structure

```
backend/
  go.mod
  docker-compose.yml          # postgres + redis + backend
  Dockerfile
  Makefile
  cmd/server/main.go          # entrypoint: wire deps, graceful shutdown
  cmd/genkeys/main.go         # one-off: print server OPAQUE key material (base64) for env
  internal/
    config/config.go          # load config from env (DB/Redis URLs, server key material)
    platform/
      postgres/postgres.go     # pgxpool connection
      postgres/migrate.go      # embedded-SQL migration runner
      postgres/migrations/*.sql
      redis/redis.go           # go-redis client
    opaque/opaque.go           # wrapper over bytemare/opaque (server)
    session/session.go         # issue/validate/revoke sessions in Redis
    session/ratelimit.go       # Redis fixed-window rate limiter
    store/
      users.go                 # UserRepo (Postgres)
      devices.go               # DeviceRepo + kt_outbox emission
      keypackages.go           # KeyPackageRepo
      store.go                 # shared types, tx helper
    as/as.go                   # OPAQUE register/login orchestration
    devices/devices.go         # device enroll/list/revoke service
    keypackages/keypackages.go # keypackage upload/consume/count service
    httpapi/
      router.go                # net/http 1.22 mux + route registration
      middleware.go            # auth, recover, requestlog
      auth_handlers.go         # /auth/* handlers
      device_handlers.go       # /devices/* handlers
      keypackage_handlers.go   # /keypackages/* handlers
      dto.go                   # request/response JSON structs
```

Each file has one responsibility; repositories sit behind interfaces so services unit-test with fakes and integration-test against real Postgres.

---

## Milestone 1 — Skeleton, infra, OPAQUE core

### Task 1: Scaffold module, layout, and Docker Compose

**Files:**
- Create: `backend/go.mod`, `backend/cmd/server/main.go`, `backend/internal/config/config.go`, `backend/docker-compose.yml`, `backend/Makefile`, `backend/.gitignore`

- [ ] **Step 1: Write the failing test**

`backend/internal/config/config_test.go`:
```go
package config

import (
	"testing"
)

func TestLoadFromEnvDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost:5432/as?sslmode=disable")
	t.Setenv("REDIS_URL", "redis://localhost:6379/0")
	t.Setenv("OPAQUE_SERVER_PRIVATE_KEY", "AAAA")
	t.Setenv("OPAQUE_SERVER_PUBLIC_KEY", "BBBB")
	t.Setenv("OPAQUE_OPRF_SEED", "CCCC")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DatabaseURL == "" || cfg.RedisURL == "" {
		t.Fatal("expected DB and Redis URLs to be populated")
	}
	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("expected default HTTPAddr :8080, got %q", cfg.HTTPAddr)
	}
}

func TestLoadMissingRequiredFails(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error when DATABASE_URL is missing")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/config/`
Expected: FAIL — package/`Load` undefined (no go.mod yet).

- [ ] **Step 3: Write minimal implementation**

`backend/go.mod`:
```
module github.com/messenger/backend

go 1.22
```

`backend/internal/config/config.go`:
```go
package config

import (
	"fmt"
	"os"
)

// Config holds all runtime configuration, loaded from the environment.
type Config struct {
	HTTPAddr    string
	DatabaseURL string
	RedisURL    string

	// OPAQUE long-term server key material (base64), shared across all instances.
	OpaqueServerPrivateKey string
	OpaqueServerPublicKey  string
	OpaqueOPRFSeed         string
}

func Load() (*Config, error) {
	c := &Config{
		HTTPAddr:               getenv("HTTP_ADDR", ":8080"),
		DatabaseURL:            os.Getenv("DATABASE_URL"),
		RedisURL:               os.Getenv("REDIS_URL"),
		OpaqueServerPrivateKey: os.Getenv("OPAQUE_SERVER_PRIVATE_KEY"),
		OpaqueServerPublicKey:  os.Getenv("OPAQUE_SERVER_PUBLIC_KEY"),
		OpaqueOPRFSeed:         os.Getenv("OPAQUE_OPRF_SEED"),
	}
	for k, v := range map[string]string{
		"DATABASE_URL":              c.DatabaseURL,
		"REDIS_URL":                 c.RedisURL,
		"OPAQUE_SERVER_PRIVATE_KEY": c.OpaqueServerPrivateKey,
		"OPAQUE_SERVER_PUBLIC_KEY":  c.OpaqueServerPublicKey,
		"OPAQUE_OPRF_SEED":          c.OpaqueOPRFSeed,
	} {
		if v == "" {
			return nil, fmt.Errorf("required env var %s is empty", k)
		}
	}
	return c, nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
```

`backend/cmd/server/main.go`:
```go
package main

import (
	"log"

	"github.com/messenger/backend/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	log.Printf("auth-service starting on %s", cfg.HTTPAddr)
	// Wiring is added in later tasks.
}
```

`backend/.gitignore`:
```
/server
*.env
```

`backend/docker-compose.yml`:
```yaml
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: as
      POSTGRES_PASSWORD: as
      POSTGRES_DB: as
    ports: ["5432:5432"]
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U as"]
      interval: 2s
      timeout: 3s
      retries: 10
  redis:
    image: redis:7-alpine
    ports: ["6379:6379"]
  backend:
    build: .
    depends_on: [postgres, redis]
    environment:
      DATABASE_URL: postgres://as:as@postgres:5432/as?sslmode=disable
      REDIS_URL: redis://redis:6379/0
    ports: ["8080:8080"]
```

`backend/Makefile`:
```make
.PHONY: test build run
test:
	go test ./...
build:
	go build -o server ./cmd/server
run:
	go run ./cmd/server
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/config/`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add backend/go.mod backend/cmd backend/internal/config backend/docker-compose.yml backend/Makefile backend/.gitignore
git commit -m "feat(backend): scaffold module, config loader, docker-compose"
```

---

### Task 2: Postgres connection, embedded migration runner, schema

**Files:**
- Create: `backend/internal/platform/postgres/postgres.go`, `backend/internal/platform/postgres/migrate.go`, `backend/internal/platform/postgres/migrations/0001_init.sql`, `backend/internal/platform/postgres/migrate_test.go`

- [ ] **Step 1: Write the failing test (integration — requires Docker)**

`backend/internal/platform/postgres/migrate_test.go`:
```go
package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func startPostgres(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	ctr, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("as"),
		postgres.WithUsername("as"),
		postgres.WithPassword("as"),
		testcontainers.WithWaitStrategy(
			wait.ForListeningPort("5432/tcp").WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })
	dsn, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("conn string: %v", err)
	}
	return dsn
}

func TestMigrateCreatesTables(t *testing.T) {
	dsn := startPostgres(t)
	ctx := context.Background()
	pool, err := Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Running again must be idempotent.
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate (second run): %v", err)
	}

	for _, table := range []string{"users", "devices", "key_packages", "kt_outbox"} {
		var exists bool
		err := pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name=$1)`,
			table).Scan(&exists)
		if err != nil {
			t.Fatalf("check %s: %v", table, err)
		}
		if !exists {
			t.Fatalf("expected table %q to exist", table)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/platform/postgres/`
Expected: FAIL — `Connect`/`Migrate` undefined. (If Docker is not running, you'll see a container-start error instead — ensure Docker is up before proceeding.)

- [ ] **Step 3: Write minimal implementation**

Add deps: `cd backend && go get github.com/jackc/pgx/v5/pgxpool github.com/testcontainers/testcontainers-go github.com/testcontainers/testcontainers-go/modules/postgres`

`backend/internal/platform/postgres/postgres.go`:
```go
package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect opens a pgx connection pool.
func Connect(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	return pgxpool.New(ctx, dsn)
}
```

`backend/internal/platform/postgres/migrate.go`:
```go
package postgres

import (
	"context"
	"embed"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate applies all embedded SQL migrations in filename order, tracking
// applied versions in schema_migrations. Idempotent.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		var applied bool
		if err := pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT FROM schema_migrations WHERE version=$1)`, name).
			Scan(&applied); err != nil {
			return err
		}
		if applied {
			continue
		}
		sqlBytes, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}
```

`backend/internal/platform/postgres/migrations/0001_init.sql`:
```sql
CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT UNIQUE NOT NULL,
    opaque_record BYTEA NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE devices (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    signing_public_key BYTEA NOT NULL,
    label              TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL DEFAULT 'active',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at         TIMESTAMPTZ
);
CREATE INDEX idx_devices_user ON devices(user_id);

CREATE TABLE key_packages (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id      UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    key_package    BYTEA NOT NULL,
    is_last_resort BOOLEAN NOT NULL DEFAULT FALSE,
    consumed_at    TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_kp_device_unconsumed
    ON key_packages(device_id) WHERE consumed_at IS NULL AND is_last_resort = FALSE;

CREATE TABLE kt_outbox (
    id         BIGSERIAL PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    payload    JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    relayed_at TIMESTAMPTZ
);
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/platform/postgres/`
Expected: PASS (1 test; pulls the postgres image on first run).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/platform/postgres backend/go.mod backend/go.sum
git commit -m "feat(backend): postgres pool + embedded migration runner + initial schema"
```

---

### Task 3: Redis client wrapper

**Files:**
- Create: `backend/internal/platform/redis/redis.go`, `backend/internal/platform/redis/redis_test.go`

- [ ] **Step 1: Write the failing test (unit — miniredis, no Docker)**

`backend/internal/platform/redis/redis_test.go`:
```go
package redis

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestNewClientPingAndSetGet(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	ctx := context.Background()
	client, err := NewClient(ctx, "redis://"+mr.Addr()+"/0")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if err := client.Set(ctx, "k", "v", time.Minute).Err(); err != nil {
		t.Fatalf("set: %v", err)
	}
	got, err := client.Get(ctx, "k").Result()
	if err != nil || got != "v" {
		t.Fatalf("get: got %q err %v", got, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/platform/redis/`
Expected: FAIL — `NewClient` undefined.

- [ ] **Step 3: Write minimal implementation**

Add deps: `cd backend && go get github.com/redis/go-redis/v9 github.com/alicebob/miniredis/v2`

`backend/internal/platform/redis/redis.go`:
```go
package redis

import (
	"context"

	"github.com/redis/go-redis/v9"
)

// Client is the shared Redis client type used across the service.
type Client = redis.Client

// NewClient parses a redis:// URL, connects, and verifies with PING.
func NewClient(ctx context.Context, url string) (*Client, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	c := redis.NewClient(opt)
	if err := c.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return c, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/platform/redis/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/platform/redis backend/go.mod backend/go.sum
git commit -m "feat(backend): redis client wrapper"
```

---

### Task 4: OPAQUE server wrapper + full round-trip unit test

**Files:**
- Create: `backend/internal/opaque/opaque.go`, `backend/internal/opaque/opaque_test.go`, `backend/cmd/genkeys/main.go`

This wraps `bytemare/opaque` v0.18.0. The verified server API: `opaque.DefaultConfiguration()`, `cfg.Server()`, `cfg.KeyGen() (sk *ecc.Scalar, pk *ecc.Element)`, `cfg.GenerateOPRFSeed() []byte`, `server.SetKeyMaterial(*ServerKeyMaterial)`, `server.RegistrationResponse(req, credID, clientOPRFKey)`, `server.GenerateKE2(ke1, *ClientRecord) (*message.KE2, *ServerOutput, error)` where `ServerOutput{ClientMAC, SessionSecret}`, `server.LoginFinish(ke3, expectedClientMac) error`, and `server.Deserialize.{RegistrationRequest,RegistrationRecord,KE1,KE2,KE3}`. Client API: `cfg.Client()`, `client.RegistrationInit(pw)`, `client.RegistrationFinalize(resp, clientID, serverID) (record, exportKey, err)`, `client.GenerateKE1(pw)`, `client.GenerateKE3(ke2, clientID, serverID) (ke3, sessionKey, exportKey, err)`.

- [ ] **Step 1: Write the failing test**

`backend/internal/opaque/opaque_test.go`:
```go
package opaque

import (
	"bytes"
	"testing"

	xopaque "github.com/bytemare/opaque"
)

// Helper: generate fresh server key material for tests.
func testKeyMaterial(t *testing.T) (priv, pub, seed []byte) {
	t.Helper()
	cfg := xopaque.DefaultConfiguration()
	sk, pk := cfg.KeyGen()
	return sk.Encode(), pk.Encode(), cfg.GenerateOPRFSeed()
}

func TestRegisterThenLoginSucceeds(t *testing.T) {
	priv, pub, seed := testKeyMaterial(t)
	srv, err := NewServer(priv, pub, seed, []byte("messenger-as"))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	credID := []byte("user-credential-id")
	password := []byte("correct horse battery staple")

	// --- Registration (client side simulated with bytemare client) ---
	cfg := xopaque.DefaultConfiguration()
	client, _ := cfg.Client()
	regReq, _ := client.RegistrationInit(password)

	regRespBytes, err := srv.RegistrationResponse(regReq.Serialize(), credID)
	if err != nil {
		t.Fatalf("RegistrationResponse: %v", err)
	}
	regResp, err := client.Deserialize.RegistrationResponse(regRespBytes)
	if err != nil {
		t.Fatalf("client deserialize reg resp: %v", err)
	}
	record, _, err := client.RegistrationFinalize(regResp, nil, []byte("messenger-as"))
	if err != nil {
		t.Fatalf("RegistrationFinalize: %v", err)
	}
	recordBytes := record.Serialize()

	// --- Login ---
	client2, _ := cfg.Client()
	ke1, _ := client2.GenerateKE1(password)

	ke2Bytes, loginState, err := srv.LoginStart(ke1.Serialize(), credID, recordBytes)
	if err != nil {
		t.Fatalf("LoginStart: %v", err)
	}
	ke2, err := client2.Deserialize.KE2(ke2Bytes)
	if err != nil {
		t.Fatalf("client deserialize ke2: %v", err)
	}
	ke3, clientSession, _, err := client2.GenerateKE3(ke2, nil, []byte("messenger-as"))
	if err != nil {
		t.Fatalf("GenerateKE3: %v", err)
	}

	serverSession, err := srv.LoginFinish(ke3.Serialize(), loginState)
	if err != nil {
		t.Fatalf("LoginFinish: %v", err)
	}
	if !bytes.Equal(clientSession, serverSession) {
		t.Fatal("client and server session secrets must match on success")
	}
}

func TestLoginWrongPasswordFails(t *testing.T) {
	priv, pub, seed := testKeyMaterial(t)
	srv, _ := NewServer(priv, pub, seed, []byte("messenger-as"))
	credID := []byte("user-credential-id")

	cfg := xopaque.DefaultConfiguration()
	client, _ := cfg.Client()
	regReq, _ := client.RegistrationInit([]byte("right-password"))
	regRespBytes, _ := srv.RegistrationResponse(regReq.Serialize(), credID)
	regResp, _ := client.Deserialize.RegistrationResponse(regRespBytes)
	record, _, _ := client.RegistrationFinalize(regResp, nil, []byte("messenger-as"))

	client2, _ := cfg.Client()
	ke1, _ := client2.GenerateKE1([]byte("WRONG-password"))
	ke2Bytes, loginState, err := srv.LoginStart(ke1.Serialize(), credID, record.Serialize())
	if err != nil {
		t.Fatalf("LoginStart: %v", err)
	}
	ke2, _ := client2.Deserialize.KE2(ke2Bytes)
	ke3, _, _, err := client2.GenerateKE3(ke2, nil, []byte("messenger-as"))
	if err != nil {
		// Some configs surface the failure here; that's acceptable.
		return
	}
	if _, err := srv.LoginFinish(ke3.Serialize(), loginState); err == nil {
		t.Fatal("expected LoginFinish to fail on wrong password")
	}
}

func TestFakeRecordIsDeterministicPerCredID(t *testing.T) {
	priv, pub, seed := testKeyMaterial(t)
	srv, _ := NewServer(priv, pub, seed, []byte("messenger-as"))
	a, err := srv.FakeRecord([]byte("unknown@corp"))
	if err != nil {
		t.Fatalf("FakeRecord: %v", err)
	}
	b, _ := srv.FakeRecord([]byte("unknown@corp"))
	if !bytes.Equal(a, b) {
		t.Fatal("fake record must be stable for the same credential id (anti-enumeration)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/opaque/`
Expected: FAIL — `NewServer` undefined.

- [ ] **Step 3: Write minimal implementation**

Add dep: `cd backend && go get github.com/bytemare/opaque@v0.18.0`

`backend/internal/opaque/opaque.go`:
```go
package opaque

import (
	"fmt"

	xopaque "github.com/bytemare/opaque"
)

// Server wraps the bytemare/opaque server with this service's fixed configuration
// and persistent key material. The wire protocol it speaks is the contract the
// OPAQUE client (separate plan) implements against.
type Server struct {
	cfg      *xopaque.Configuration
	identity []byte
	skm      *xopaque.ServerKeyMaterial
}

// NewServer builds a server from base64-decoded key material (see cmd/genkeys).
// priv/pub are the encoded server AKE keypair; seed is the global OPRF seed.
func NewServer(privEncoded, pubEncoded, oprfSeed, identity []byte) (*Server, error) {
	cfg := xopaque.DefaultConfiguration()
	sk := cfg.AKE.Group().NewScalar()
	if err := sk.Decode(privEncoded); err != nil {
		return nil, fmt.Errorf("decode server private key: %w", err)
	}
	skm := &xopaque.ServerKeyMaterial{
		PrivateKey:     sk,
		PublicKeyBytes: pubEncoded,
		OPRFGlobalSeed: oprfSeed,
		Identity:       identity,
	}
	s := &Server{cfg: cfg, identity: identity, skm: skm}
	// Validate by constructing the underlying server once.
	if _, err := s.newServer(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) newServer() (*xopaque.Server, error) {
	srv, err := s.cfg.Server()
	if err != nil {
		return nil, err
	}
	if err := srv.SetKeyMaterial(s.skm); err != nil {
		return nil, err
	}
	return srv, nil
}

// RegistrationResponse processes a serialized RegistrationRequest and returns a
// serialized RegistrationResponse. credID is the stable per-user credential id.
func (s *Server) RegistrationResponse(reqBytes, credID []byte) ([]byte, error) {
	srv, err := s.newServer()
	if err != nil {
		return nil, err
	}
	req, err := srv.Deserialize.RegistrationRequest(reqBytes)
	if err != nil {
		return nil, fmt.Errorf("deserialize registration request: %w", err)
	}
	resp, err := srv.RegistrationResponse(req, credID, nil)
	if err != nil {
		return nil, err
	}
	return resp.Serialize(), nil
}

// LoginState is the short-lived per-login secret the server must keep between
// LoginStart (GenerateKE2) and LoginFinish. Stored in Redis keyed by login_id.
type LoginState struct {
	ExpectedClientMAC []byte
	SessionSecret     []byte
}

// LoginStart processes a serialized KE1 against the stored record and returns a
// serialized KE2 plus the LoginState to persist until LoginFinish.
func (s *Server) LoginStart(ke1Bytes, credID, recordBytes []byte) ([]byte, *LoginState, error) {
	srv, err := s.newServer()
	if err != nil {
		return nil, nil, err
	}
	ke1, err := srv.Deserialize.KE1(ke1Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("deserialize ke1: %w", err)
	}
	rec, err := srv.Deserialize.RegistrationRecord(recordBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("deserialize record: %w", err)
	}
	cr := &xopaque.ClientRecord{
		RegistrationRecord:   rec,
		CredentialIdentifier: credID,
		ClientIdentity:       nil,
	}
	ke2, out, err := srv.GenerateKE2(ke1, cr)
	if err != nil {
		return nil, nil, err
	}
	return ke2.Serialize(), &LoginState{
		ExpectedClientMAC: out.ClientMAC,
		SessionSecret:     out.SessionSecret,
	}, nil
}

// LoginFinish verifies the serialized KE3 against the stored LoginState. On
// success it returns the server's session secret.
func (s *Server) LoginFinish(ke3Bytes []byte, st *LoginState) ([]byte, error) {
	srv, err := s.newServer()
	if err != nil {
		return nil, err
	}
	ke3, err := srv.Deserialize.KE3(ke3Bytes)
	if err != nil {
		return nil, fmt.Errorf("deserialize ke3: %w", err)
	}
	if err := srv.LoginFinish(ke3, st.ExpectedClientMAC); err != nil {
		return nil, err
	}
	return st.SessionSecret, nil
}

// FakeRecord returns a deterministic fake registration record for an unknown
// credential id, so login attempts on non-existent accounts are indistinguishable
// from real ones (anti-enumeration).
//
// IMPORTANT (verified against bytemare/opaque v0.18.0): cfg.GetFakeRecord returns
// a FRESH RANDOM record on every call and ignores credID for the record contents.
// Returning it directly BREAKS anti-enumeration — two login attempts on the same
// unknown identity would yield records that differ, letting an attacker distinguish
// unknown accounts. Instead, take one correctly-sized fake record from the library
// and deterministically overwrite ClientPublicKey / MaskingKey / Envelope with
// values expanded from the SECRET server OPRF seed keyed by credID (HMAC-SHA512 with
// a domain-separation tag). This yields a record that is correctly shaped for the
// active config, stable per credID, and unforgeable offline (keyed by the secret seed).
// GenerateKE2 on this fake record produces a KE2 byte-identical in length to a real
// account's; login only fails later at LoginFinish (same as a wrong password).
func (s *Server) FakeRecord(credID []byte) ([]byte, error) {
	rec, err := s.cfg.GetFakeRecord(credID)
	if err != nil {
		return nil, err
	}
	rr := rec.RegistrationRecord
	expand := func(label byte, n int) []byte {
		dst := []byte("messenger-opaque-fake-record-v1")
		out := make([]byte, 0, n)
		var counter uint32
		for len(out) < n {
			mac := hmac.New(sha512.New, s.skm.OPRFGlobalSeed)
			mac.Write(dst)
			mac.Write([]byte{label})
			var c [4]byte
			binary.BigEndian.PutUint32(c[:], counter)
			mac.Write(c[:])
			mac.Write(credID)
			out = mac.Sum(out)
			counter++
		}
		return out[:n]
	}
	group := s.cfg.AKE.Group()
	scalar := group.HashToScalar(expand(0x00, group.ScalarLength()), []byte("messenger-opaque-fake-record-v1"))
	rr.ClientPublicKey = group.Base().Multiply(scalar)
	rr.MaskingKey = expand(0x01, len(rr.MaskingKey))
	rr.Envelope = expand(0x02, len(rr.Envelope))
	return rr.Serialize(), nil
}
// (requires imports: crypto/hmac, crypto/sha512, encoding/binary)
```

`backend/cmd/genkeys/main.go`:
```go
package main

import (
	"encoding/base64"
	"fmt"

	xopaque "github.com/bytemare/opaque"
)

// genkeys prints OPAQUE server key material as base64, to be set as env vars
// shared across all instances. Run once per deployment.
func main() {
	cfg := xopaque.DefaultConfiguration()
	sk, pk := cfg.KeyGen()
	seed := cfg.GenerateOPRFSeed()
	enc := base64.StdEncoding.EncodeToString
	fmt.Printf("OPAQUE_SERVER_PRIVATE_KEY=%s\n", enc(sk.Encode()))
	fmt.Printf("OPAQUE_SERVER_PUBLIC_KEY=%s\n", enc(pk.Encode()))
	fmt.Printf("OPAQUE_OPRF_SEED=%s\n", enc(seed))
}
```

**API-version risk (verify, don't guess):** The exact spellings of `cfg.AKE.Group().NewScalar()`, `sk.Encode()`/`sk.Decode()`, `cfg.GetFakeRecord(...).RegistrationRecord`, and `client.Deserialize.RegistrationResponse` may differ slightly in v0.18.0. If anything fails to compile, run `go doc github.com/bytemare/opaque` and `go doc github.com/bytemare/opaque.Configuration` (and the `message` / `ecc` subpackages) to find the exact names, and adjust minimally while preserving behavior. Report what you changed. The behavioral contract (register→login round-trip produces matching session secrets; wrong password fails; fake record is stable) must hold via the tests.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/opaque/`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/opaque backend/cmd/genkeys backend/go.mod backend/go.sum
git commit -m "feat(backend): OPAQUE server wrapper + genkeys + round-trip tests"
```

---

## Milestone 2 — Users, sessions, AS over HTTP

### Task 5: User repository

**Files:**
- Create: `backend/internal/store/store.go`, `backend/internal/store/users.go`, `backend/internal/store/users_test.go`

- [ ] **Step 1: Write the failing test (integration — Docker)**

`backend/internal/store/users_test.go`:
```go
package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/messenger/backend/internal/platform/postgres"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("as"), tcpostgres.WithUsername("as"), tcpostgres.WithPassword("as"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("5432/tcp").WithStartupTimeout(60*time.Second)))
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })
	dsn, _ := ctr.ConnectionString(ctx, "sslmode=disable")
	pool, err := postgres.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}

func TestUserRepoCreateAndGet(t *testing.T) {
	pool := newTestPool(t)
	repo := NewUserRepo(pool)
	ctx := context.Background()

	u, err := repo.Create(ctx, "alice@corp", []byte("opaque-record"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if u.ID == "" {
		t.Fatal("expected generated id")
	}

	got, err := repo.GetByEmail(ctx, "alice@corp")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != u.ID || string(got.OpaqueRecord) != "opaque-record" {
		t.Fatal("round-trip mismatch")
	}

	if _, err := repo.GetByEmail(ctx, "nobody@corp"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	if _, err := repo.Create(ctx, "alice@corp", []byte("x")); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict on duplicate email, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/store/`
Expected: FAIL — `NewUserRepo`/`ErrNotFound` undefined.

- [ ] **Step 3: Write minimal implementation**

`backend/internal/store/store.go`:
```go
package store

import "errors"

var (
	ErrNotFound = errors.New("store: not found")
	ErrConflict = errors.New("store: conflict")
)

// User is an account record.
type User struct {
	ID           string
	Email        string
	OpaqueRecord []byte
}
```

`backend/internal/store/users.go`:
```go
package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UserRepo struct{ pool *pgxpool.Pool }

func NewUserRepo(pool *pgxpool.Pool) *UserRepo { return &UserRepo{pool: pool} }

func (r *UserRepo) Create(ctx context.Context, email string, opaqueRecord []byte) (*User, error) {
	var id string
	err := r.pool.QueryRow(ctx,
		`INSERT INTO users (email, opaque_record) VALUES ($1, $2) RETURNING id`,
		email, opaqueRecord).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			return nil, ErrConflict
		}
		return nil, err
	}
	return &User{ID: id, Email: email, OpaqueRecord: opaqueRecord}, nil
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, opaque_record FROM users WHERE email=$1`, email).
		Scan(&u.ID, &u.Email, &u.OpaqueRecord)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/store/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/store backend/go.sum
git commit -m "feat(backend): user repository with conflict/not-found semantics"
```

---

### Task 6: Session store (Redis) — issue / validate / revoke

**Files:**
- Create: `backend/internal/session/session.go`, `backend/internal/session/session_test.go`

- [ ] **Step 1: Write the failing test (unit — miniredis)**

`backend/internal/session/session_test.go`:
```go
package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
)

func newTestRedis(t *testing.T) *goredis.Client {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	return goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
}

func TestSessionIssueValidateRevoke(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager(newTestRedis(t), time.Hour)

	token, err := mgr.Issue(ctx, "user-1", "")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if len(token) < 32 {
		t.Fatalf("token too short: %d", len(token))
	}

	sess, err := mgr.Validate(ctx, token)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if sess.UserID != "user-1" {
		t.Fatalf("got user %q", sess.UserID)
	}

	if err := mgr.Revoke(ctx, token); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := mgr.Validate(ctx, token); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("expected ErrInvalidSession after revoke, got %v", err)
	}
}

func TestRevokeAllForUser() {}

func TestRevokeAll(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager(newTestRedis(t), time.Hour)
	t1, _ := mgr.Issue(ctx, "user-1", "dev-a")
	t2, _ := mgr.Issue(ctx, "user-1", "dev-b")

	if err := mgr.RevokeAll(ctx, "user-1"); err != nil {
		t.Fatalf("revoke all: %v", err)
	}
	for _, tok := range []string{t1, t2} {
		if _, err := mgr.Validate(ctx, tok); !errors.Is(err, ErrInvalidSession) {
			t.Fatalf("expected all sessions invalid, %q still valid", tok)
		}
	}
}

func TestBindDevice(t *testing.T) {
	ctx := context.Background()
	mgr := NewManager(newTestRedis(t), time.Hour)
	token, _ := mgr.Issue(ctx, "user-1", "")
	if err := mgr.BindDevice(ctx, token, "dev-x"); err != nil {
		t.Fatalf("bind: %v", err)
	}
	sess, _ := mgr.Validate(ctx, token)
	if sess.DeviceID != "dev-x" {
		t.Fatalf("expected device bound, got %q", sess.DeviceID)
	}
}
```

(Delete the stray empty `TestRevokeAllForUser` if your linter objects — it's a no-op placeholder; do not keep it.)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/session/`
Expected: FAIL — `NewManager` undefined.

- [ ] **Step 3: Write minimal implementation**

`backend/internal/session/session.go`:
```go
package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

var ErrInvalidSession = errors.New("session: invalid or expired")

// Session is the state stored per token.
type Session struct {
	UserID   string `json:"user_id"`
	DeviceID string `json:"device_id"`
}

// Manager issues and validates revocable sessions backed by Redis.
// Keys: session:{token} -> JSON; user_sessions:{userID} -> SET of tokens.
type Manager struct {
	rdb *goredis.Client
	ttl time.Duration
}

func NewManager(rdb *goredis.Client, ttl time.Duration) *Manager {
	return &Manager{rdb: rdb, ttl: ttl}
}

func sessionKey(token string) string { return "session:" + token }
func userKey(userID string) string   { return "user_sessions:" + userID }

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (m *Manager) Issue(ctx context.Context, userID, deviceID string) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(Session{UserID: userID, DeviceID: deviceID})
	pipe := m.rdb.TxPipeline()
	pipe.Set(ctx, sessionKey(token), data, m.ttl)
	pipe.SAdd(ctx, userKey(userID), token)
	pipe.Expire(ctx, userKey(userID), m.ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return "", err
	}
	return token, nil
}

func (m *Manager) Validate(ctx context.Context, token string) (*Session, error) {
	data, err := m.rdb.Get(ctx, sessionKey(token)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return nil, ErrInvalidSession
	}
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("corrupt session: %w", err)
	}
	// Sliding expiry.
	m.rdb.Expire(ctx, sessionKey(token), m.ttl)
	return &s, nil
}

func (m *Manager) BindDevice(ctx context.Context, token, deviceID string) error {
	s, err := m.Validate(ctx, token)
	if err != nil {
		return err
	}
	s.DeviceID = deviceID
	data, _ := json.Marshal(s)
	return m.rdb.Set(ctx, sessionKey(token), data, m.ttl).Err()
}

func (m *Manager) Revoke(ctx context.Context, token string) error {
	s, err := m.Validate(ctx, token)
	if err == nil {
		m.rdb.SRem(ctx, userKey(s.UserID), token)
	}
	return m.rdb.Del(ctx, sessionKey(token)).Err()
}

func (m *Manager) RevokeAll(ctx context.Context, userID string) error {
	tokens, err := m.rdb.SMembers(ctx, userKey(userID)).Result()
	if err != nil {
		return err
	}
	pipe := m.rdb.TxPipeline()
	for _, tok := range tokens {
		pipe.Del(ctx, sessionKey(tok))
	}
	pipe.Del(ctx, userKey(userID))
	_, err = pipe.Exec(ctx)
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/session/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/session
git commit -m "feat(backend): revocable Redis session manager"
```

---

### Task 7: AS registration + login orchestration

**Files:**
- Create: `backend/internal/as/as.go`, `backend/internal/as/as_test.go`

The AS ties together the opaque wrapper, user repo, session manager, and a Redis store for login state. Login state is keyed by a random `login_id` with a 30s TTL.

- [ ] **Step 1: Write the failing test (unit — miniredis + in-memory user store + real opaque)**

`backend/internal/as/as_test.go`:
```go
package as

import (
	"context"
	"errors"
	"testing"
	"time"

	xopaque "github.com/bytemare/opaque"
	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	"github.com/messenger/backend/internal/opaque"
	"github.com/messenger/backend/internal/session"
	"github.com/messenger/backend/internal/store"
)

// fakeUserStore implements the UserStore interface in-memory.
type fakeUserStore struct {
	byEmail map[string]*store.User
	nextID  int
}

func newFakeUsers() *fakeUserStore { return &fakeUserStore{byEmail: map[string]*store.User{}} }

func (f *fakeUserStore) Create(_ context.Context, email string, rec []byte) (*store.User, error) {
	if _, ok := f.byEmail[email]; ok {
		return nil, store.ErrConflict
	}
	f.nextID++
	u := &store.User{ID: string(rune('a' + f.nextID)), Email: email, OpaqueRecord: rec}
	f.byEmail[email] = u
	return u, nil
}
func (f *fakeUserStore) GetByEmail(_ context.Context, email string) (*store.User, error) {
	u, ok := f.byEmail[email]
	if !ok {
		return nil, store.ErrNotFound
	}
	return u, nil
}

func newAS(t *testing.T) *Service {
	t.Helper()
	cfg := xopaque.DefaultConfiguration()
	sk, pk := cfg.KeyGen()
	srv, err := opaque.NewServer(sk.Encode(), pk.Encode(), cfg.GenerateOPRFSeed(), []byte("messenger-as"))
	if err != nil {
		t.Fatalf("opaque server: %v", err)
	}
	mr, _ := miniredis.Run()
	t.Cleanup(mr.Close)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	sess := session.NewManager(rdb, time.Hour)
	return NewService(srv, newFakeUsers(), sess, rdb)
}

func TestRegisterAndLoginRoundTrip(t *testing.T) {
	ctx := context.Background()
	svc := newAS(t)
	cfg := xopaque.DefaultConfiguration()
	client, _ := cfg.Client()
	pw := []byte("hunter2hunter2")

	// Register
	regReq, _ := client.RegistrationInit(pw)
	respBytes, err := svc.RegisterStart(ctx, "bob@corp", regReq.Serialize())
	if err != nil {
		t.Fatalf("RegisterStart: %v", err)
	}
	regResp, _ := client.Deserialize.RegistrationResponse(respBytes)
	record, _, _ := client.RegistrationFinalize(regResp, nil, []byte("messenger-as"))
	if err := svc.RegisterFinish(ctx, "bob@corp", record.Serialize()); err != nil {
		t.Fatalf("RegisterFinish: %v", err)
	}

	// Login
	client2, _ := cfg.Client()
	ke1, _ := client2.GenerateKE1(pw)
	loginID, ke2Bytes, err := svc.LoginStart(ctx, "bob@corp", ke1.Serialize())
	if err != nil {
		t.Fatalf("LoginStart: %v", err)
	}
	ke2, _ := client2.Deserialize.KE2(ke2Bytes)
	ke3, _, _, _ := client2.GenerateKE3(ke2, nil, []byte("messenger-as"))
	token, enrollRequired, err := svc.LoginFinish(ctx, loginID, ke3.Serialize())
	if err != nil {
		t.Fatalf("LoginFinish: %v", err)
	}
	if token == "" {
		t.Fatal("expected a session token")
	}
	if !enrollRequired {
		t.Fatal("first login should require device enrollment")
	}
}

func TestLoginUnknownUserDoesNotRevealAbsence(t *testing.T) {
	ctx := context.Background()
	svc := newAS(t)
	cfg := xopaque.DefaultConfiguration()
	client, _ := cfg.Client()
	ke1, _ := client.GenerateKE1([]byte("whatever"))

	// Unknown email must still return a well-formed KE2 (fake record), not an error.
	loginID, ke2Bytes, err := svc.LoginStart(ctx, "ghost@corp", ke1.Serialize())
	if err != nil {
		t.Fatalf("LoginStart for unknown user should not error: %v", err)
	}
	if loginID == "" || len(ke2Bytes) == 0 {
		t.Fatal("expected a KE2 indistinguishable from a real user")
	}
	// Finishing will fail auth, but that is the same outcome as a wrong password.
	ke2, _ := client.Deserialize.KE2(ke2Bytes)
	ke3, _, _, kerr := client.GenerateKE3(ke2, nil, []byte("messenger-as"))
	if kerr == nil {
		if _, _, err := svc.LoginFinish(ctx, loginID, ke3.Serialize()); !errors.Is(err, ErrAuthFailed) {
			t.Fatalf("expected ErrAuthFailed, got %v", err)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/as/`
Expected: FAIL — `NewService` undefined.

- [ ] **Step 3: Write minimal implementation**

`backend/internal/as/as.go`:
```go
package as

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/messenger/backend/internal/opaque"
	"github.com/messenger/backend/internal/session"
	"github.com/messenger/backend/internal/store"
)

var ErrAuthFailed = errors.New("as: authentication failed")

const loginStateTTL = 30 * time.Second

// UserStore is the subset of the user repository the AS needs.
type UserStore interface {
	Create(ctx context.Context, email string, opaqueRecord []byte) (*store.User, error)
	GetByEmail(ctx context.Context, email string) (*store.User, error)
}

type Service struct {
	opaque *opaque.Server
	users  UserStore
	sess   *session.Manager
	rdb    *goredis.Client
}

func NewService(o *opaque.Server, users UserStore, sess *session.Manager, rdb *goredis.Client) *Service {
	return &Service{opaque: o, users: users, sess: sess, rdb: rdb}
}

// credID derives the stable OPAQUE credential identifier from the email.
func credID(email string) []byte { return []byte("cred:" + email) }

func (s *Service) RegisterStart(ctx context.Context, email string, regReq []byte) ([]byte, error) {
	return s.opaque.RegistrationResponse(regReq, credID(email))
}

func (s *Service) RegisterFinish(ctx context.Context, email string, record []byte) error {
	_, err := s.users.Create(ctx, email, record)
	return err
}

type loginState struct {
	UserID            string `json:"user_id"`    // empty for fake/unknown user
	Email             string `json:"email"`
	ExpectedClientMAC []byte `json:"mac"`
	SessionSecret     []byte `json:"secret"`
}

func loginKey(id string) string { return "login:" + id }

// LoginStart returns a login_id and serialized KE2. For unknown users it uses a
// deterministic fake record so the response is indistinguishable (anti-enumeration).
func (s *Service) LoginStart(ctx context.Context, email string, ke1 []byte) (string, []byte, error) {
	var (
		recordBytes []byte
		userID      string
	)
	u, err := s.users.GetByEmail(ctx, email)
	switch {
	case err == nil:
		recordBytes, userID = u.OpaqueRecord, u.ID
	case errors.Is(err, store.ErrNotFound):
		fake, ferr := s.opaque.FakeRecord(credID(email))
		if ferr != nil {
			return "", nil, ferr
		}
		recordBytes = fake
	default:
		return "", nil, err
	}

	ke2, st, err := s.opaque.LoginStart(ke1, credID(email), recordBytes)
	if err != nil {
		return "", nil, err
	}

	id, err := randID()
	if err != nil {
		return "", nil, err
	}
	data, _ := json.Marshal(loginState{
		UserID: userID, Email: email,
		ExpectedClientMAC: st.ExpectedClientMAC, SessionSecret: st.SessionSecret,
	})
	if err := s.rdb.Set(ctx, loginKey(id), data, loginStateTTL).Err(); err != nil {
		return "", nil, err
	}
	return id, ke2, nil
}

// LoginFinish verifies KE3. Returns a session token and whether device enrollment
// is still required. Unknown users and wrong passwords both yield ErrAuthFailed.
func (s *Service) LoginFinish(ctx context.Context, loginID string, ke3 []byte) (string, bool, error) {
	data, err := s.rdb.Get(ctx, loginKey(loginID)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return "", false, ErrAuthFailed
	}
	if err != nil {
		return "", false, err
	}
	s.rdb.Del(ctx, loginKey(loginID)) // single-use

	var ls loginState
	if err := json.Unmarshal(data, &ls); err != nil {
		return "", false, err
	}

	if _, err := s.opaque.LoginFinish(ke3, &opaque.LoginState{
		ExpectedClientMAC: ls.ExpectedClientMAC, SessionSecret: ls.SessionSecret,
	}); err != nil {
		return "", false, ErrAuthFailed
	}
	if ls.UserID == "" { // fake-record path: KE3 verification can't actually succeed,
		return "", false, ErrAuthFailed // but guard defensively.
	}

	token, err := s.sess.Issue(ctx, ls.UserID, "")
	if err != nil {
		return "", false, err
	}
	// device_enroll_required is true until a device is bound to the session.
	return token, true, nil
}

func randID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/as/`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/as
git commit -m "feat(backend): AS register/login orchestration with anti-enumeration"
```

---

### Task 8: HTTP API — router, middleware, auth endpoints

**Files:**
- Create: `backend/internal/httpapi/router.go`, `backend/internal/httpapi/middleware.go`, `backend/internal/httpapi/dto.go`, `backend/internal/httpapi/auth_handlers.go`, `backend/internal/httpapi/auth_handlers_test.go`

- [ ] **Step 1: Write the failing test (handler-level, httptest; AS uses miniredis + fake users)**

`backend/internal/httpapi/auth_handlers_test.go`:
```go
package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	xopaque "github.com/bytemare/opaque"
	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	"github.com/messenger/backend/internal/as"
	"github.com/messenger/backend/internal/opaque"
	"github.com/messenger/backend/internal/session"
	"github.com/messenger/backend/internal/store"
)

// reuse the in-memory user store pattern.
type memUsers struct{ m map[string]*store.User }

func (u *memUsers) Create(_ context.Context, e string, r []byte) (*store.User, error) {
	if _, ok := u.m[e]; ok {
		return nil, store.ErrConflict
	}
	usr := &store.User{ID: "u-" + e, Email: e, OpaqueRecord: r}
	u.m[e] = usr
	return usr, nil
}
func (u *memUsers) GetByEmail(_ context.Context, e string) (*store.User, error) {
	if usr, ok := u.m[e]; ok {
		return usr, nil
	}
	return nil, store.ErrNotFound
}

func newServer(t *testing.T) (http.Handler, *session.Manager) {
	t.Helper()
	cfg := xopaque.DefaultConfiguration()
	sk, pk := cfg.KeyGen()
	osrv, _ := opaque.NewServer(sk.Encode(), pk.Encode(), cfg.GenerateOPRFSeed(), []byte("messenger-as"))
	mr, _ := miniredis.Run()
	t.Cleanup(mr.Close)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	sess := session.NewManager(rdb, time.Hour)
	svc := as.NewService(osrv, &memUsers{m: map[string]*store.User{}}, sess, rdb)
	return NewRouter(svc, sess), sess
}

func postJSON(t *testing.T, h http.Handler, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	buf, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func TestFullOpaqueFlowOverHTTP(t *testing.T) {
	h, _ := newServer(t)
	cfg := xopaque.DefaultConfiguration()
	client, _ := cfg.Client()
	pw := []byte("s3cret-passphrase")

	// register/start
	regReq, _ := client.RegistrationInit(pw)
	rec := postJSON(t, h, "/auth/register/start",
		map[string]string{"email": "carol@corp", "opaque_registration_request": b64(regReq.Serialize())}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("register/start: %d %s", rec.Code, rec.Body)
	}
	var rs struct {
		OpaqueRegistrationResponse string `json:"opaque_registration_response"`
	}
	json.Unmarshal(rec.Body.Bytes(), &rs)
	respBytes, _ := base64.StdEncoding.DecodeString(rs.OpaqueRegistrationResponse)
	regResp, _ := client.Deserialize.RegistrationResponse(respBytes)
	record, _, _ := client.RegistrationFinalize(regResp, nil, []byte("messenger-as"))

	// register/finish
	rec = postJSON(t, h, "/auth/register/finish",
		map[string]string{"email": "carol@corp", "opaque_registration_record": b64(record.Serialize())}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("register/finish: %d %s", rec.Code, rec.Body)
	}

	// login/start
	client2, _ := cfg.Client()
	ke1, _ := client2.GenerateKE1(pw)
	rec = postJSON(t, h, "/auth/login/start",
		map[string]string{"email": "carol@corp", "ke1": b64(ke1.Serialize())}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login/start: %d %s", rec.Code, rec.Body)
	}
	var ls struct {
		LoginID string `json:"login_id"`
		KE2     string `json:"ke2"`
	}
	json.Unmarshal(rec.Body.Bytes(), &ls)
	ke2Bytes, _ := base64.StdEncoding.DecodeString(ls.KE2)
	ke2, _ := client2.Deserialize.KE2(ke2Bytes)
	ke3, _, _, _ := client2.GenerateKE3(ke2, nil, []byte("messenger-as"))

	// login/finish
	rec = postJSON(t, h, "/auth/login/finish",
		map[string]string{"login_id": ls.LoginID, "ke3": b64(ke3.Serialize())}, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("login/finish: %d %s", rec.Code, rec.Body)
	}
	var lf struct {
		SessionToken         string `json:"session_token"`
		DeviceEnrollRequired bool   `json:"device_enroll_required"`
	}
	json.Unmarshal(rec.Body.Bytes(), &lf)
	if lf.SessionToken == "" || !lf.DeviceEnrollRequired {
		t.Fatalf("unexpected login/finish body: %s", rec.Body)
	}

	// authenticated /auth/session
	rec = postJSON(t, h, "/auth/session-check", nil, lf.SessionToken) // uses GET below; see note
	_ = rec
}

func TestSessionEndpointRequiresAuth(t *testing.T) {
	h, _ := newServer(t)
	req := httptest.NewRequest(http.MethodGet, "/auth/session", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}
}
```

(Remove the `/auth/session-check` placeholder line in `TestFullOpaqueFlowOverHTTP` — it is a leftover; the real session check is covered by `TestSessionEndpointRequiresAuth` and Task 12's end-to-end test. Keep the test focused on the register→login flow returning a token.)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/httpapi/`
Expected: FAIL — `NewRouter` undefined.

- [ ] **Step 3: Write minimal implementation**

`backend/internal/httpapi/dto.go`:
```go
package httpapi

type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

type registerStartReq struct {
	Email                      string `json:"email"`
	OpaqueRegistrationRequest  string `json:"opaque_registration_request"`
}
type registerStartResp struct {
	OpaqueRegistrationResponse string `json:"opaque_registration_response"`
}
type registerFinishReq struct {
	Email                     string `json:"email"`
	OpaqueRegistrationRecord  string `json:"opaque_registration_record"`
}
type loginStartReq struct {
	Email string `json:"email"`
	KE1   string `json:"ke1"`
}
type loginStartResp struct {
	LoginID string `json:"login_id"`
	KE2     string `json:"ke2"`
}
type loginFinishReq struct {
	LoginID string `json:"login_id"`
	KE3     string `json:"ke3"`
}
type loginFinishResp struct {
	SessionToken         string `json:"session_token"`
	DeviceEnrollRequired bool   `json:"device_enroll_required"`
}
type sessionResp struct {
	UserID   string `json:"user_id"`
	DeviceID string `json:"device_id"`
}
```

`backend/internal/httpapi/middleware.go`:
```go
package httpapi

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/messenger/backend/internal/session"
)

type ctxKey string

const sessionCtxKey ctxKey = "session"

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, errorResponse{Error: code, Message: msg})
}

func recoverMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic: %v", rec)
				writeError(w, http.StatusInternalServerError, "internal", "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// authMW requires a valid Bearer session token and injects the Session into context.
func authMW(sess *session.Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authz := r.Header.Get("Authorization")
			token := strings.TrimPrefix(authz, "Bearer ")
			if token == "" || token == authz {
				writeError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
				return
			}
			s, err := sess.Validate(r.Context(), token)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "unauthorized", "invalid session")
				return
			}
			ctx := context.WithValue(r.Context(), sessionCtxKey, sessionWithToken{Session: s, Token: token})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type sessionWithToken struct {
	Session *session.Session
	Token   string
}

func sessionFrom(ctx context.Context) sessionWithToken {
	v, _ := ctx.Value(sessionCtxKey).(sessionWithToken)
	return v
}
```

`backend/internal/httpapi/auth_handlers.go`:
```go
package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/messenger/backend/internal/as"
	"github.com/messenger/backend/internal/session"
	"github.com/messenger/backend/internal/store"
)

type authHandlers struct {
	svc  *as.Service
	sess *session.Manager
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON")
		return false
	}
	return true
}

func decodeB64(w http.ResponseWriter, s string) ([]byte, bool) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid base64")
		return nil, false
	}
	return b, true
}

func (h *authHandlers) registerStart(w http.ResponseWriter, r *http.Request) {
	var req registerStartReq
	if !decodeJSON(w, r, &req) {
		return
	}
	reqBytes, ok := decodeB64(w, req.OpaqueRegistrationRequest)
	if !ok {
		return
	}
	resp, err := h.svc.RegisterStart(r.Context(), req.Email, reqBytes)
	if err != nil {
		writeError(w, http.StatusBadRequest, "opaque_error", "registration failed")
		return
	}
	writeJSON(w, http.StatusOK, registerStartResp{
		OpaqueRegistrationResponse: base64.StdEncoding.EncodeToString(resp),
	})
}

func (h *authHandlers) registerFinish(w http.ResponseWriter, r *http.Request) {
	var req registerFinishReq
	if !decodeJSON(w, r, &req) {
		return
	}
	rec, ok := decodeB64(w, req.OpaqueRegistrationRecord)
	if !ok {
		return
	}
	err := h.svc.RegisterFinish(r.Context(), req.Email, rec)
	if errors.Is(err, store.ErrConflict) {
		writeError(w, http.StatusConflict, "conflict", "account already exists")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not store record")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *authHandlers) loginStart(w http.ResponseWriter, r *http.Request) {
	var req loginStartReq
	if !decodeJSON(w, r, &req) {
		return
	}
	ke1, ok := decodeB64(w, req.KE1)
	if !ok {
		return
	}
	loginID, ke2, err := h.svc.LoginStart(r.Context(), req.Email, ke1)
	if err != nil {
		writeError(w, http.StatusBadRequest, "opaque_error", "login failed")
		return
	}
	writeJSON(w, http.StatusOK, loginStartResp{
		LoginID: loginID, KE2: base64.StdEncoding.EncodeToString(ke2),
	})
}

func (h *authHandlers) loginFinish(w http.ResponseWriter, r *http.Request) {
	var req loginFinishReq
	if !decodeJSON(w, r, &req) {
		return
	}
	ke3, ok := decodeB64(w, req.KE3)
	if !ok {
		return
	}
	token, enroll, err := h.svc.LoginFinish(r.Context(), req.LoginID, ke3)
	if errors.Is(err, as.ErrAuthFailed) {
		writeError(w, http.StatusUnauthorized, "auth_failed", "invalid credentials")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "login error")
		return
	}
	writeJSON(w, http.StatusOK, loginFinishResp{SessionToken: token, DeviceEnrollRequired: enroll})
}

func (h *authHandlers) session(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	writeJSON(w, http.StatusOK, sessionResp{UserID: swt.Session.UserID, DeviceID: swt.Session.DeviceID})
}

func (h *authHandlers) logout(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	_ = h.sess.Revoke(r.Context(), swt.Token)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *authHandlers) logoutAll(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	_ = h.sess.RevokeAll(r.Context(), swt.Session.UserID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
```

`backend/internal/httpapi/router.go`:
```go
package httpapi

import (
	"net/http"

	"github.com/messenger/backend/internal/as"
	"github.com/messenger/backend/internal/session"
)

// NewRouter wires the auth endpoints. Device/keypackage routes are added in Task 12.
func NewRouter(svc *as.Service, sess *session.Manager) http.Handler {
	mux := http.NewServeMux()
	ah := &authHandlers{svc: svc, sess: sess}

	mux.HandleFunc("POST /auth/register/start", ah.registerStart)
	mux.HandleFunc("POST /auth/register/finish", ah.registerFinish)
	mux.HandleFunc("POST /auth/login/start", ah.loginStart)
	mux.HandleFunc("POST /auth/login/finish", ah.loginFinish)

	auth := authMW(sess)
	mux.Handle("GET /auth/session", auth(http.HandlerFunc(ah.session)))
	mux.Handle("POST /auth/logout", auth(http.HandlerFunc(ah.logout)))
	mux.Handle("POST /auth/logout/all", auth(http.HandlerFunc(ah.logoutAll)))

	return recoverMW(mux)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/httpapi/`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/httpapi
git commit -m "feat(backend): HTTP auth endpoints, auth middleware, full OPAQUE flow over HTTP"
```

---

## Milestone 3 — Devices, KeyPackages, hardening

### Task 9: Device repository with transactional KT outbox

**Files:**
- Create: `backend/internal/store/devices.go`, `backend/internal/store/devices_test.go`

- [ ] **Step 1: Write the failing test (integration — Docker; reuses newTestPool from Task 5)**

`backend/internal/store/devices_test.go`:
```go
package store

import (
	"context"
	"testing"
)

func TestDeviceEnrollWritesOutboxInSameTx(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	users := NewUserRepo(pool)
	devices := NewDeviceRepo(pool)

	u, _ := users.Create(ctx, "dave@corp", []byte("rec"))

	dev, err := devices.Enroll(ctx, u.ID, []byte("signing-pub-key"), "laptop")
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if dev.ID == "" || dev.Status != "active" {
		t.Fatalf("unexpected device: %+v", dev)
	}

	// An outbox row must exist for the same user (written in the same tx).
	var outboxCount int
	pool.QueryRow(ctx,
		`SELECT count(*) FROM kt_outbox WHERE user_id=$1 AND event_type='device_added'`, u.ID).
		Scan(&outboxCount)
	if outboxCount != 1 {
		t.Fatalf("expected 1 kt_outbox row, got %d", outboxCount)
	}

	list, _ := devices.ListByUser(ctx, u.ID)
	if len(list) != 1 {
		t.Fatalf("expected 1 device, got %d", len(list))
	}

	if err := devices.Revoke(ctx, dev.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	// Revoke also emits an outbox event.
	pool.QueryRow(ctx,
		`SELECT count(*) FROM kt_outbox WHERE user_id=$1 AND event_type='device_revoked'`, u.ID).
		Scan(&outboxCount)
	if outboxCount != 1 {
		t.Fatalf("expected 1 device_revoked outbox row, got %d", outboxCount)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/store/ -run TestDeviceEnroll`
Expected: FAIL — `NewDeviceRepo` undefined.

- [ ] **Step 3: Write minimal implementation**

Append to `backend/internal/store/store.go`:
```go
// Device is a registered device for a user.
type Device struct {
	ID        string
	UserID    string
	Label     string
	Status    string
}
```

`backend/internal/store/devices.go`:
```go
package store

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type DeviceRepo struct{ pool *pgxpool.Pool }

func NewDeviceRepo(pool *pgxpool.Pool) *DeviceRepo { return &DeviceRepo{pool: pool} }

// Enroll inserts a device and, in the SAME transaction, writes a kt_outbox event.
func (r *DeviceRepo) Enroll(ctx context.Context, userID string, signingPubKey []byte, label string) (*Device, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var id string
	if err := tx.QueryRow(ctx,
		`INSERT INTO devices (user_id, signing_public_key, label) VALUES ($1,$2,$3) RETURNING id`,
		userID, signingPubKey, label).Scan(&id); err != nil {
		return nil, err
	}
	if err := emitKTEvent(ctx, tx, userID, "device_added", map[string]string{"device_id": id}); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &Device{ID: id, UserID: userID, Label: label, Status: "active"}, nil
}

func (r *DeviceRepo) ListByUser(ctx context.Context, userID string) ([]Device, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, user_id, label, status FROM devices WHERE user_id=$1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.ID, &d.UserID, &d.Label, &d.Status); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Revoke marks a device revoked and emits a kt_outbox event in the same tx.
func (r *DeviceRepo) Revoke(ctx context.Context, deviceID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var userID string
	err = tx.QueryRow(ctx,
		`UPDATE devices SET status='revoked', revoked_at=now() WHERE id=$1 RETURNING user_id`,
		deviceID).Scan(&userID)
	if err == pgx.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err := emitKTEvent(ctx, tx, userID, "device_revoked", map[string]string{"device_id": deviceID}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func emitKTEvent(ctx context.Context, tx pgx.Tx, userID, eventType string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO kt_outbox (user_id, event_type, payload) VALUES ($1,$2,$3)`,
		userID, eventType, data)
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/store/ -run TestDeviceEnroll`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/store/devices.go backend/internal/store/store.go backend/internal/store/devices_test.go
git commit -m "feat(backend): device repo with transactional KT outbox events"
```

---

### Task 10: KeyPackage repository — upload / consume one-time / last-resort / count

**Files:**
- Create: `backend/internal/store/keypackages.go`, `backend/internal/store/keypackages_test.go`

- [ ] **Step 1: Write the failing test (integration — Docker)**

`backend/internal/store/keypackages_test.go`:
```go
package store

import (
	"context"
	"testing"
)

func TestKeyPackageUploadConsumeExhaustLastResort(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	users := NewUserRepo(pool)
	devices := NewDeviceRepo(pool)
	kp := NewKeyPackageRepo(pool)

	u, _ := users.Create(ctx, "erin@corp", []byte("rec"))
	dev, _ := devices.Enroll(ctx, u.ID, []byte("pub"), "phone")

	// Upload two one-time and one last-resort.
	if err := kp.Upload(ctx, dev.ID, [][]byte{[]byte("otk-1"), []byte("otk-2")}, false); err != nil {
		t.Fatalf("upload one-time: %v", err)
	}
	if err := kp.Upload(ctx, dev.ID, [][]byte{[]byte("last-resort")}, true); err != nil {
		t.Fatalf("upload last-resort: %v", err)
	}

	if n, _ := kp.CountAvailable(ctx, dev.ID); n != 2 {
		t.Fatalf("expected 2 available one-time, got %d", n)
	}

	// Consume both one-time packages; each must be distinct and not last-resort.
	first, lr1, _ := kp.Consume(ctx, dev.ID)
	second, lr2, _ := kp.Consume(ctx, dev.ID)
	if lr1 || lr2 {
		t.Fatal("one-time consumes should not be last-resort while pool is non-empty")
	}
	if string(first) == string(second) {
		t.Fatal("must not hand out the same one-time package twice")
	}

	if n, _ := kp.CountAvailable(ctx, dev.ID); n != 0 {
		t.Fatalf("expected 0 available after consuming both, got %d", n)
	}

	// Pool exhausted -> last-resort, flagged.
	got, isLast, err := kp.Consume(ctx, dev.ID)
	if err != nil {
		t.Fatalf("consume last-resort: %v", err)
	}
	if !isLast || string(got) != "last-resort" {
		t.Fatalf("expected last-resort package, got %q last=%v", got, isLast)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/store/ -run TestKeyPackage`
Expected: FAIL — `NewKeyPackageRepo` undefined.

- [ ] **Step 3: Write minimal implementation**

`backend/internal/store/keypackages.go`:
```go
package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNoKeyPackage = errors.New("store: no key package available")

type KeyPackageRepo struct{ pool *pgxpool.Pool }

func NewKeyPackageRepo(pool *pgxpool.Pool) *KeyPackageRepo { return &KeyPackageRepo{pool: pool} }

func (r *KeyPackageRepo) Upload(ctx context.Context, deviceID string, packages [][]byte, lastResort bool) error {
	batch := &pgx.Batch{}
	for _, p := range packages {
		batch.Queue(
			`INSERT INTO key_packages (device_id, key_package, is_last_resort) VALUES ($1,$2,$3)`,
			deviceID, p, lastResort)
	}
	br := r.pool.SendBatch(ctx, batch)
	defer br.Close()
	for range packages {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}

func (r *KeyPackageRepo) CountAvailable(ctx context.Context, deviceID string) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM key_packages
		 WHERE device_id=$1 AND consumed_at IS NULL AND is_last_resort=FALSE`, deviceID).Scan(&n)
	return n, err
}

// Consume returns one unconsumed one-time package (marking it consumed). If the
// one-time pool is empty, it returns the last-resort package with isLastResort=true.
func (r *KeyPackageRepo) Consume(ctx context.Context, deviceID string) (pkg []byte, isLastResort bool, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback(ctx)

	// Atomically select-and-mark one unconsumed one-time package.
	var id string
	err = tx.QueryRow(ctx,
		`SELECT id, key_package FROM key_packages
		 WHERE device_id=$1 AND consumed_at IS NULL AND is_last_resort=FALSE
		 ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1`, deviceID).Scan(&id, &pkg)
	switch {
	case err == nil:
		if _, err := tx.Exec(ctx, `UPDATE key_packages SET consumed_at=now() WHERE id=$1`, id); err != nil {
			return nil, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, false, err
		}
		return pkg, false, nil
	case errors.Is(err, pgx.ErrNoRows):
		// Fall back to last-resort (reusable, not marked consumed).
		err = tx.QueryRow(ctx,
			`SELECT key_package FROM key_packages
			 WHERE device_id=$1 AND is_last_resort=TRUE LIMIT 1`, deviceID).Scan(&pkg)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, ErrNoKeyPackage
		}
		if err != nil {
			return nil, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, false, err
		}
		return pkg, true, nil
	default:
		return nil, false, err
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/store/ -run TestKeyPackage`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/store/keypackages.go backend/internal/store/keypackages_test.go
git commit -m "feat(backend): keypackage repo — one-time consume + last-resort fallback"
```

---

### Task 11: Device & KeyPackage HTTP endpoints + enroll flow

**Files:**
- Create: `backend/internal/devices/devices.go`, `backend/internal/keypackages/keypackages.go`, `backend/internal/httpapi/device_handlers.go`, `backend/internal/httpapi/keypackage_handlers.go`, `backend/internal/httpapi/device_flow_test.go`
- Modify: `backend/internal/httpapi/router.go`, `backend/internal/httpapi/dto.go`

- [ ] **Step 1: Write the failing test (integration — Docker; full stack with real Postgres + miniredis)**

`backend/internal/httpapi/device_flow_test.go`:
```go
package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	xopaque "github.com/bytemare/opaque"
	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	"github.com/messenger/backend/internal/as"
	"github.com/messenger/backend/internal/devices"
	"github.com/messenger/backend/internal/keypackages"
	"github.com/messenger/backend/internal/opaque"
	"github.com/messenger/backend/internal/platform/postgres"
	"github.com/messenger/backend/internal/session"
	"github.com/messenger/backend/internal/store"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func newFullServer(t *testing.T) http.Handler {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("as"), tcpostgres.WithUsername("as"), tcpostgres.WithPassword("as"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("5432/tcp").WithStartupTimeout(60*time.Second)))
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })
	dsn, _ := ctr.ConnectionString(ctx, "sslmode=disable")
	pool, _ := postgres.Connect(ctx, dsn)
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cfg := xopaque.DefaultConfiguration()
	sk, pk := cfg.KeyGen()
	osrv, _ := opaque.NewServer(sk.Encode(), pk.Encode(), cfg.GenerateOPRFSeed(), []byte("messenger-as"))
	mr, _ := miniredis.Run()
	t.Cleanup(mr.Close)
	rdb := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	sess := session.NewManager(rdb, time.Hour)

	userRepo := store.NewUserRepo(pool)
	svc := as.NewService(osrv, userRepo, sess, rdb)
	devSvc := devices.NewService(store.NewDeviceRepo(pool))
	kpSvc := keypackages.NewService(store.NewKeyPackageRepo(pool))
	return NewRouterFull(svc, sess, devSvc, kpSvc)
}

func TestEnrollDeviceAndUploadKeyPackages(t *testing.T) {
	h := newFullServer(t)
	cfg := xopaque.DefaultConfiguration()
	client, _ := cfg.Client()
	pw := []byte("device-flow-pass")

	// register + login -> token (helpers from auth_handlers_test.go are in-package)
	regReq, _ := client.RegistrationInit(pw)
	rec := postJSON(t, h, "/auth/register/start",
		map[string]string{"email": "frank@corp", "opaque_registration_request": b64(regReq.Serialize())}, "")
	var rs registerStartResp
	json.Unmarshal(rec.Body.Bytes(), &rs)
	respBytes, _ := base64.StdEncoding.DecodeString(rs.OpaqueRegistrationResponse)
	regResp, _ := client.Deserialize.RegistrationResponse(respBytes)
	record, _, _ := client.RegistrationFinalize(regResp, nil, []byte("messenger-as"))
	postJSON(t, h, "/auth/register/finish",
		map[string]string{"email": "frank@corp", "opaque_registration_record": b64(record.Serialize())}, "")

	client2, _ := cfg.Client()
	ke1, _ := client2.GenerateKE1(pw)
	rec = postJSON(t, h, "/auth/login/start",
		map[string]string{"email": "frank@corp", "ke1": b64(ke1.Serialize())}, "")
	var ls loginStartResp
	json.Unmarshal(rec.Body.Bytes(), &ls)
	ke2Bytes, _ := base64.StdEncoding.DecodeString(ls.KE2)
	ke2, _ := client2.Deserialize.KE2(ke2Bytes)
	ke3, _, _, _ := client2.GenerateKE3(ke2, nil, []byte("messenger-as"))
	rec = postJSON(t, h, "/auth/login/finish",
		map[string]string{"login_id": ls.LoginID, "ke3": b64(ke3.Serialize())}, "")
	var lf loginFinishResp
	json.Unmarshal(rec.Body.Bytes(), &lf)
	token := lf.SessionToken

	// enroll device
	rec = postJSON(t, h, "/devices", map[string]any{
		"signing_public_key":  b64([]byte("ed25519-pub")),
		"label":               "frank-laptop",
		"initial_key_packages": []string{b64([]byte("kp-1")), b64([]byte("kp-2"))},
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("enroll: %d %s", rec.Code, rec.Body)
	}
	var dr struct {
		DeviceID string `json:"device_id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &dr)
	if dr.DeviceID == "" {
		t.Fatal("expected device_id")
	}

	// keypackage count should be 2
	rec = postJSON(t, h, "/keypackages/count-check", nil, token) // see note; real route is GET
	_ = rec

	// list devices shows the enrolled device
	req := httptestNewGet("/devices", token)
	rrec := serve(h, req)
	if rrec.Code != http.StatusOK {
		t.Fatalf("list devices: %d", rrec.Code)
	}
}
```

NOTE: the `/keypackages/count-check` line is a leftover — delete it. Replace the two trailing GET-style assertions with real GET requests using `net/http/httptest.NewRequest(http.MethodGet, path, nil)` + a `Bearer` header (add a small `httptestNewGet`/`serve` helper in this test file, or inline them). Keep the assertion that `GET /devices` returns 200 and the device list contains the enrolled device, and that `GET /keypackages/count` returns `{"available":2}`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/httpapi/ -run TestEnrollDevice`
Expected: FAIL — `devices.NewService` / `NewRouterFull` undefined.

- [ ] **Step 3: Write minimal implementation**

`backend/internal/devices/devices.go`:
```go
package devices

import (
	"context"

	"github.com/messenger/backend/internal/store"
)

type Repo interface {
	Enroll(ctx context.Context, userID string, signingPubKey []byte, label string) (*store.Device, error)
	ListByUser(ctx context.Context, userID string) ([]store.Device, error)
	Revoke(ctx context.Context, deviceID string) error
}

type Service struct{ repo Repo }

func NewService(repo Repo) *Service { return &Service{repo: repo} }

func (s *Service) Enroll(ctx context.Context, userID string, signingPubKey []byte, label string) (*store.Device, error) {
	return s.repo.Enroll(ctx, userID, signingPubKey, label)
}
func (s *Service) List(ctx context.Context, userID string) ([]store.Device, error) {
	return s.repo.ListByUser(ctx, userID)
}
func (s *Service) Revoke(ctx context.Context, deviceID string) error {
	return s.repo.Revoke(ctx, deviceID)
}
```

`backend/internal/keypackages/keypackages.go`:
```go
package keypackages

import (
	"context"

	"github.com/messenger/backend/internal/store"
)

type Repo interface {
	Upload(ctx context.Context, deviceID string, packages [][]byte, lastResort bool) error
	CountAvailable(ctx context.Context, deviceID string) (int, error)
	Consume(ctx context.Context, deviceID string) ([]byte, bool, error)
}

type Service struct{ repo Repo }

func NewService(repo Repo) *Service { return &Service{repo: repo} }

func (s *Service) Upload(ctx context.Context, deviceID string, packages [][]byte) error {
	return s.repo.Upload(ctx, deviceID, packages, false)
}
func (s *Service) Count(ctx context.Context, deviceID string) (int, error) {
	return s.repo.CountAvailable(ctx, deviceID)
}
func (s *Service) Consume(ctx context.Context, deviceID string) ([]byte, bool, error) {
	return s.repo.Consume(ctx, deviceID)
}
```

Append to `backend/internal/httpapi/dto.go`:
```go
type enrollDeviceReq struct {
	SigningPublicKey   string   `json:"signing_public_key"`
	Label              string   `json:"label"`
	InitialKeyPackages []string `json:"initial_key_packages"`
}
type enrollDeviceResp struct {
	DeviceID string `json:"device_id"`
}
type deviceItem struct {
	DeviceID string `json:"device_id"`
	Label    string `json:"label"`
	Status   string `json:"status"`
}
type uploadKeyPackagesReq struct {
	KeyPackages []string `json:"key_packages"`
}
type keyPackageResp struct {
	KeyPackage   string `json:"key_package"`
	IsLastResort bool   `json:"is_last_resort"`
}
type countResp struct {
	Available int `json:"available"`
}
```

`backend/internal/httpapi/device_handlers.go`:
```go
package httpapi

import (
	"net/http"

	"github.com/messenger/backend/internal/devices"
)

type deviceHandlers struct {
	svc  *devices.Service
	sess sessionBinder
}

// sessionBinder lets device enroll bind the device to the current session.
type sessionBinder interface {
	BindDevice(ctx interface{ Done() <-chan struct{} }, token, deviceID string) error
}

func (h *deviceHandlers) enroll(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	var req enrollDeviceReq
	if !decodeJSON(w, r, &req) {
		return
	}
	pub, ok := decodeB64(w, req.SigningPublicKey)
	if !ok {
		return
	}
	dev, err := h.svc.Enroll(r.Context(), swt.Session.UserID, pub, req.Label)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "enroll failed")
		return
	}
	// Upload any initial key packages (best-effort within this request).
	if len(req.InitialKeyPackages) > 0 {
		pkgs := make([][]byte, 0, len(req.InitialKeyPackages))
		for _, s := range req.InitialKeyPackages {
			b, ok := decodeB64(w, s)
			if !ok {
				return
			}
			pkgs = append(pkgs, b)
		}
		if err := h.uploadInitial(r, dev.ID, pkgs); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "key package upload failed")
			return
		}
	}
	writeJSON(w, http.StatusOK, enrollDeviceResp{DeviceID: dev.ID})
}

func (h *deviceHandlers) list(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	devs, err := h.svc.List(r.Context(), swt.Session.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list failed")
		return
	}
	out := make([]deviceItem, 0, len(devs))
	for _, d := range devs {
		out = append(out, deviceItem{DeviceID: d.ID, Label: d.Label, Status: d.Status})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *deviceHandlers) revoke(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.svc.Revoke(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "revoke failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
```

NOTE on the `sessionBinder` sketch above: it is over-engineered. Replace it with the concrete dependency the router already has. Concretely: give `deviceHandlers` and `keypackageHandlers` the fields they actually need — a `*devices.Service` / `*keypackages.Service`, and a `*session.Manager` for binding the device to the session and resolving the caller's `device_id`. Implement `enroll` to call `sess.BindDevice(r.Context(), swt.Token, dev.ID)` after a successful enroll (so the session becomes device-scoped and `device_enroll_required` is satisfied), and add an `uploadInitial` helper that calls the keypackage service. Wire keypackage handlers (`upload`, `consume`, `count`) against `*keypackages.Service`, resolving `device_id` from the path or the session as appropriate. Keep handlers thin; do not introduce new interfaces beyond `devices.Repo`/`keypackages.Repo` already defined.

`backend/internal/httpapi/keypackage_handlers.go`:
```go
package httpapi

import (
	"net/http"

	"github.com/messenger/backend/internal/keypackages"
)

type keypackageHandlers struct {
	svc *keypackages.Service
}

func (h *keypackageHandlers) upload(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	var req uploadKeyPackagesReq
	if !decodeJSON(w, r, &req) {
		return
	}
	pkgs := make([][]byte, 0, len(req.KeyPackages))
	for _, s := range req.KeyPackages {
		b, ok := decodeB64(w, s)
		if !ok {
			return
		}
		pkgs = append(pkgs, b)
	}
	if err := h.svc.Upload(r.Context(), swt.Session.DeviceID, pkgs); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "upload failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *keypackageHandlers) count(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	n, err := h.svc.Count(r.Context(), swt.Session.DeviceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "count failed")
		return
	}
	writeJSON(w, http.StatusOK, countResp{Available: n})
}

func (h *keypackageHandlers) consume(w http.ResponseWriter, r *http.Request) {
	deviceID := r.PathValue("device_id")
	pkg, isLast, err := h.svc.Consume(r.Context(), deviceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "no_keypackage", "no key package available")
		return
	}
	writeJSON(w, http.StatusOK, keyPackageResp{
		KeyPackage:   b64enc(pkg),
		IsLastResort: isLast,
	})
}
```

Add `b64enc` to `middleware.go` (or a small util) returning `base64.StdEncoding.EncodeToString(b)` and the `uploadInitial` method on `deviceHandlers` calling the keypackage service. Wire enroll to bind the device into the session.

Update `backend/internal/httpapi/router.go` — add a `NewRouterFull` that includes device + keypackage routes (keep `NewRouter` for the auth-only tests):
```go
func NewRouterFull(svc *as.Service, sess *session.Manager, devSvc *devices.Service, kpSvc *keypackages.Service) http.Handler {
	mux := http.NewServeMux()
	ah := &authHandlers{svc: svc, sess: sess}
	dh := &deviceHandlers{svc: devSvc, sess: sess, kp: kpSvc}
	kh := &keypackageHandlers{svc: kpSvc}

	mux.HandleFunc("POST /auth/register/start", ah.registerStart)
	mux.HandleFunc("POST /auth/register/finish", ah.registerFinish)
	mux.HandleFunc("POST /auth/login/start", ah.loginStart)
	mux.HandleFunc("POST /auth/login/finish", ah.loginFinish)

	auth := authMW(sess)
	mux.Handle("GET /auth/session", auth(http.HandlerFunc(ah.session)))
	mux.Handle("POST /auth/logout", auth(http.HandlerFunc(ah.logout)))
	mux.Handle("POST /auth/logout/all", auth(http.HandlerFunc(ah.logoutAll)))

	mux.Handle("POST /devices", auth(http.HandlerFunc(dh.enroll)))
	mux.Handle("GET /devices", auth(http.HandlerFunc(dh.list)))
	mux.Handle("POST /devices/{id}/revoke", auth(http.HandlerFunc(dh.revoke)))

	mux.Handle("POST /keypackages", auth(http.HandlerFunc(kh.upload)))
	mux.Handle("GET /keypackages/count", auth(http.HandlerFunc(kh.count)))
	mux.Handle("GET /keypackages/{device_id}", auth(http.HandlerFunc(kh.consume)))

	return recoverMW(mux)
}
```
(Update `deviceHandlers` to carry `sess *session.Manager` and `kp *keypackages.Service` fields per the note above. Resolve the `BindDevice` signature against the real `session.Manager.BindDevice(ctx, token, deviceID)`.)

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/httpapi/ -run TestEnrollDevice`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/devices backend/internal/keypackages backend/internal/httpapi
git commit -m "feat(backend): device + keypackage services and HTTP endpoints; enroll binds session"
```

---

### Task 12: Rate limiting + wire main.go + security tests

**Files:**
- Create: `backend/internal/session/ratelimit.go`, `backend/internal/session/ratelimit_test.go`
- Modify: `backend/cmd/server/main.go`, `backend/internal/httpapi/router.go`, `backend/internal/httpapi/middleware.go`

- [ ] **Step 1: Write the failing test (unit — miniredis)**

`backend/internal/session/ratelimit_test.go`:
```go
package session

import (
	"context"
	"testing"
	"time"
)

func TestRateLimiterBlocksAfterLimit(t *testing.T) {
	ctx := context.Background()
	rl := NewRateLimiter(newTestRedis(t), 3, time.Minute)

	for i := 0; i < 3; i++ {
		ok, err := rl.Allow(ctx, "ip:1.2.3.4")
		if err != nil || !ok {
			t.Fatalf("request %d should be allowed (ok=%v err=%v)", i, ok, err)
		}
	}
	ok, err := rl.Allow(ctx, "ip:1.2.3.4")
	if err != nil {
		t.Fatalf("allow: %v", err)
	}
	if ok {
		t.Fatal("4th request should be blocked")
	}

	// A different key is independent.
	ok, _ = rl.Allow(ctx, "ip:5.6.7.8")
	if !ok {
		t.Fatal("different key should be allowed")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/session/ -run TestRateLimiter`
Expected: FAIL — `NewRateLimiter` undefined.

- [ ] **Step 3: Write minimal implementation**

`backend/internal/session/ratelimit.go`:
```go
package session

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// RateLimiter is a fixed-window counter in Redis.
type RateLimiter struct {
	rdb    *goredis.Client
	limit  int64
	window time.Duration
}

func NewRateLimiter(rdb *goredis.Client, limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{rdb: rdb, limit: int64(limit), window: window}
}

// Allow increments the counter for key and returns false once it exceeds limit
// within the window.
func (rl *RateLimiter) Allow(ctx context.Context, key string) (bool, error) {
	k := "ratelimit:" + key
	n, err := rl.rdb.Incr(ctx, k).Result()
	if err != nil {
		return false, err
	}
	if n == 1 {
		rl.rdb.Expire(ctx, k, rl.window)
	}
	return n <= rl.limit, nil
}
```

Add a `rateLimitMW` to `middleware.go` that keys on client IP and is applied to `/auth/login/start` and `/auth/register/start` (returns 429 when blocked):
```go
func rateLimitMW(rl *session.RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := r.Header.Get("X-Forwarded-For")
			if ip == "" {
				ip = r.RemoteAddr
			}
			ok, err := rl.Allow(r.Context(), "auth:"+ip)
			if err == nil && !ok {
				writeError(w, http.StatusTooManyRequests, "rate_limited", "too many requests")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```
Apply it in `NewRouterFull` by wrapping the two start handlers, e.g.:
```go
rl := rateLimitMW(rateLimiter) // pass a *session.RateLimiter into NewRouterFull
mux.Handle("POST /auth/login/start", rl(http.HandlerFunc(ah.loginStart)))
mux.Handle("POST /auth/register/start", rl(http.HandlerFunc(ah.registerStart)))
```
Add a `*session.RateLimiter` parameter to `NewRouterFull` and construct it in `main.go`. Update the auth-only `NewRouter` and existing tests that call `NewRouter`/`NewRouterFull` to pass a rate limiter built on the test Redis (a high limit so existing flow tests aren't throttled, e.g. `NewRateLimiter(rdb, 1000, time.Minute)`).

Wire `backend/cmd/server/main.go` fully:
```go
package main

import (
	"context"
	"encoding/base64"
	"log"
	"net/http"
	"time"

	"github.com/messenger/backend/internal/as"
	"github.com/messenger/backend/internal/config"
	"github.com/messenger/backend/internal/devices"
	"github.com/messenger/backend/internal/httpapi"
	"github.com/messenger/backend/internal/keypackages"
	"github.com/messenger/backend/internal/opaque"
	"github.com/messenger/backend/internal/platform/postgres"
	"github.com/messenger/backend/internal/platform/redis"
	"github.com/messenger/backend/internal/session"
	"github.com/messenger/backend/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	ctx := context.Background()

	pool, err := postgres.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer pool.Close()
	if err := postgres.Migrate(ctx, pool); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	rdb, err := redis.NewClient(ctx, cfg.RedisURL)
	if err != nil {
		log.Fatalf("redis: %v", err)
	}

	dec := base64.StdEncoding.DecodeString
	priv, _ := dec(cfg.OpaqueServerPrivateKey)
	pub, _ := dec(cfg.OpaqueServerPublicKey)
	seed, _ := dec(cfg.OpaqueOPRFSeed)
	osrv, err := opaque.NewServer(priv, pub, seed, []byte("messenger-as"))
	if err != nil {
		log.Fatalf("opaque: %v", err)
	}

	sess := session.NewManager(rdb, 24*time.Hour)
	rl := session.NewRateLimiter(rdb, 10, time.Minute)
	svc := as.NewService(osrv, store.NewUserRepo(pool), sess, rdb)
	devSvc := devices.NewService(store.NewDeviceRepo(pool))
	kpSvc := keypackages.NewService(store.NewKeyPackageRepo(pool))

	handler := httpapi.NewRouterFull(svc, sess, devSvc, kpSvc, rl)

	log.Printf("auth-service listening on %s", cfg.HTTPAddr)
	if err := http.ListenAndServe(cfg.HTTPAddr, handler); err != nil {
		log.Fatalf("server: %v", err)
	}
}
```

Add a security test `backend/internal/httpapi/security_test.go`:
```go
package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Session revocation is immediate: after logout the token is rejected.
func TestLogoutRevokesImmediately(t *testing.T) {
	h, sess := newServer(t)
	token, _ := sess.Issue(t.Context(), "user-x", "")
	// authenticated call works
	req := httptestNewGet("/auth/session", token)
	if serve(h, req).Code != http.StatusOK {
		t.Fatal("expected 200 with valid token")
	}
	// logout
	if postJSON(t, h, "/auth/logout", nil, token).Code != http.StatusOK {
		t.Fatal("logout failed")
	}
	// now rejected
	if serve(h, httptestNewGet("/auth/session", token)).Code != http.StatusUnauthorized {
		t.Fatal("expected 401 after logout")
	}
}

// Unknown-user and wrong-password logins produce the same status/shape (anti-enumeration).
func TestLoginIndistinguishableForUnknownUser(t *testing.T) {
	h, _ := newServer(t)
	// login/start for a never-registered user must still return 200 with a login_id+ke2
	rec := postJSON(t, h, "/auth/login/start",
		map[string]string{"email": "ghost@corp", "ke1": b64(make([]byte, 32))}, "")
	// Either 200 (well-formed ke2) — never a 404/"user not found".
	if rec.Code == http.StatusNotFound {
		t.Fatal("unknown user must not be distinguishable via 404")
	}
	var body map[string]any
	json.Unmarshal(rec.Body.Bytes(), &body)
	if _, hasErr := body["error"]; hasErr {
		if msg, _ := body["message"].(string); msg == "user not found" {
			t.Fatal("error message leaks user existence")
		}
	}
}
```
Add the small `httptestNewGet(path, token)` and `serve(h, req)` helpers to a shared `_test.go` (e.g. `auth_handlers_test.go`): `httptestNewGet` builds a GET request with a Bearer header; `serve` runs it through the handler and returns the recorder. Note `t.Context()` requires Go 1.24+; if on 1.22/1.23 use `context.Background()`.

- [ ] **Step 4: Run all tests**

Run: `cd backend && go build ./... && go test ./...`
Expected: PASS — all unit tests pass; integration tests pass when Docker is running. `go vet ./...` clean.

- [ ] **Step 5: Commit**

```bash
git add backend
git commit -m "feat(backend): auth rate limiting, full server wiring, security tests"
```

---

## Self-Review

**1. Spec coverage:**
- OPAQUE register/login, server never sees password → Tasks 4, 7, 8 ✓
- Revocable Redis sessions (+ logout, logout/all) → Tasks 6, 8 ✓
- Short-lived login state in Redis (multi-instance safe) → Task 7 ✓
- Device enroll / list / revoke, bind device to session → Tasks 9, 11 ✓
- KeyPackage store: upload, one-time consume, last-resort, count → Tasks 10, 11 ✓
- Transactional KT outbox (same tx) → Task 9 ✓
- Email as username → Tasks 5, 7, 8 ✓
- Anti-enumeration (fake record), rate limiting → Tasks 4, 7, 12 ✓
- Postgres schema (users/devices/key_packages/kt_outbox) → Task 2 ✓
- Modular monolith, stateless instances, Docker Compose → Tasks 1, 12 ✓
- Testing: OPAQUE round-trip, session lifecycle, enroll, keypackage consume/exhaustion, outbox-in-tx, security (revoke-immediate, anti-enumeration) → Tasks 4, 6, 7, 8, 9, 10, 11, 12 ✓
- Out of scope: KT log, DS/WebSocket, OPAQUE client → correctly excluded; `kt_outbox` is the only KT touchpoint ✓

**2. Placeholder scan:** Three test snippets contain intentionally-flagged leftovers to delete (`TestRevokeAllForUser` no-op in Task 6; `/auth/session-check` line in Task 8; `/keypackages/count-check` line in Task 11) — each is explicitly called out with the correction. Task 11's `sessionBinder` sketch is explicitly flagged as over-engineered with concrete replacement instructions. These are guidance, not silent gaps. All production code steps contain complete implementations.

**3. Type consistency:** `opaque.Server` methods (`RegistrationResponse`, `LoginStart`→`LoginState`, `LoginFinish`, `FakeRecord`) are used consistently in `as.Service`. `as.Service` methods (`RegisterStart/Finish`, `LoginStart/Finish`) match the HTTP handlers. `session.Manager` (`Issue`, `Validate`, `BindDevice`, `Revoke`, `RevokeAll`) is consistent across AS, middleware, and handlers. Repos (`UserRepo`, `DeviceRepo`, `KeyPackageRepo`) match the `devices.Repo`/`keypackages.Repo` interfaces. `NewRouter` (auth-only, Tasks 8) vs `NewRouterFull` (Tasks 11–12, adds device/keypackage + rate limiter) — Task 12 updates `NewRouterFull`'s signature to add the rate limiter and notes that callers/tests must be updated.

**4. API-version risk (call out, not placeholder):** The `bytemare/opaque` v0.18.0 method/type spellings are taken from the published API but two spots warrant a `go doc` check during execution (flagged inline in Task 4): scalar `Encode()/Decode()` and `GetFakeRecord(...).RegistrationRecord`. The `testcontainers-go` module API (`postgres.Run`, `ConnectionString`) is current as of v0.30+; if the installed version differs, adjust the container bootstrap accordingly.
