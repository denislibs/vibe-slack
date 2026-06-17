# Workspaces Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a multi-tenant workspace layer (workspaces + membership + owner/admin/member roles) and a global-unique `username`, on top of the existing AS/DS/KT monolith, with a thin frontend slice to create/select a workspace before chat.

**Architecture:** New Go module `internal/workspace` (repo + service + HTTP) reusing the AS device-bound session for identity. One additive migration (`workspaces`, `workspace_members`, `users.username`). The only AS change is accepting `username` at registration. No scoping of `conversations`/KT yet (deferred to subproject 6/8). Frontend gains a typed `workspace` API client, a current-workspace context, and a create/select gate.

**Tech Stack:** Go (pgx/v5, net/http ServeMux, testcontainers-go), SolidJS + Feature-Sliced Design (vitest/jsdom), Postgres.

**Conventions (verified in the codebase — follow exactly):**
- Repos live in `internal/store` (`pool *pgxpool.Pool`, `errors.As` on `*pgconn.PgError` code `"23505"` → `store.ErrConflict`; `pgx.ErrNoRows` → `store.ErrNotFound`).
- Services live in their own package, take a `Repo` interface, hold no HTTP knowledge (see `internal/devices`).
- HTTP handlers in `internal/httpapi`: helpers `decodeJSON`, `writeJSON`, `writeError`, `sessionFrom(ctx)` (returns `sessionWithToken{Session:*session.Session, Token string}`), routed in `router.go` via `NewRouterFull(...)`, guarded by `authMW(sess)`. Go 1.22 method-prefixed patterns (`"POST /workspaces"`, `{id}` path wildcards via `r.PathValue("id")`).
- Integration tests use `newTestPool(t)` (testcontainers `postgres:16-alpine`, `tcpostgres.BasicWaitStrategies()`, then `postgres.Migrate`). It lives in `internal/store/users_test.go` (package `store`).
- Migrations: drop a new file in `internal/platform/postgres/migrations/`; `Migrate` applies in filename order, tracked in `schema_migrations`. **Integration tests require a running Docker daemon.**

---

### Task 1: Migration — workspaces, workspace_members, users.username

**Files:**
- Create: `backend/internal/platform/postgres/migrations/0004_workspaces.sql`
- Test: `backend/internal/platform/postgres/migrate_workspaces_test.go`

- [ ] **Step 1: Write the failing test**

`backend/internal/platform/postgres/migrate_workspaces_test.go`:
```go
package postgres

import (
	"context"
	"testing"

	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestMigrateWorkspaces(t *testing.T) {
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("as"), tcpostgres.WithUsername("as"), tcpostgres.WithPassword("as"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })
	dsn, _ := ctr.ConnectionString(ctx, "sslmode=disable")
	pool, err := Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// users.username exists, is unique and not null
	var col string
	if err := pool.QueryRow(ctx,
		`SELECT column_name FROM information_schema.columns WHERE table_name='users' AND column_name='username'`).
		Scan(&col); err != nil {
		t.Fatalf("users.username missing: %v", err)
	}

	// workspaces + workspace_members tables exist
	for _, tbl := range []string{"workspaces", "workspace_members"} {
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM information_schema.tables WHERE table_name=$1`, tbl).Scan(&n); err != nil || n != 1 {
			t.Fatalf("table %s missing (n=%d err=%v)", tbl, n, err)
		}
	}

	// role CHECK rejects bogus roles (needs a user + workspace first)
	var uid string
	pool.QueryRow(ctx, `INSERT INTO users (email, username, opaque_record) VALUES ('a@c','alice','x') RETURNING id`).Scan(&uid)
	var wid string
	pool.QueryRow(ctx, `INSERT INTO workspaces (name, slug, owner_user_id) VALUES ('W','w',$1) RETURNING id`, uid).Scan(&wid)
	if _, err := pool.Exec(ctx,
		`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1,$2,'wizard')`, wid, uid); err == nil {
		t.Fatal("expected CHECK violation for bogus role")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/platform/postgres/ -run TestMigrateWorkspaces -v`
Expected: FAIL — `users.username missing` / tables absent.

- [ ] **Step 3: Write the migration**

`backend/internal/platform/postgres/migrations/0004_workspaces.sql`:
```sql
ALTER TABLE users ADD COLUMN username TEXT;
-- No real users exist yet; this guard keeps the migration safe if any rows are present.
UPDATE users SET username = id::text WHERE username IS NULL;
ALTER TABLE users ALTER COLUMN username SET NOT NULL;
ALTER TABLE users ADD CONSTRAINT users_username_key UNIQUE (username);

CREATE TABLE workspaces (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT NOT NULL,
    slug          TEXT NOT NULL UNIQUE,
    owner_user_id UUID NOT NULL REFERENCES users(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE workspace_members (
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id      UUID NOT NULL REFERENCES users(id),
    role         TEXT NOT NULL CHECK (role IN ('owner','admin','member')),
    joined_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, user_id)
);
CREATE INDEX workspace_members_user_idx ON workspace_members(user_id);
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/platform/postgres/ -run TestMigrateWorkspaces -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/internal/platform/postgres/migrations/0004_workspaces.sql backend/internal/platform/postgres/migrate_workspaces_test.go
git -C /Users/denisurevic/Documents/slack commit -m "feat(backend): workspaces migration (workspaces, members, users.username)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 2: users store — username, Create signature, lookup

**Files:**
- Modify: `backend/internal/store/store.go` (add `Username` to `User`, add `ErrUsernameTaken`)
- Modify: `backend/internal/store/users.go` (`Create` takes username; `FindByEmailOrUsername`)
- Modify: all callers of `UserRepo.Create` (compiler-guided; tests)
- Test: `backend/internal/store/users_workspace_test.go`

- [ ] **Step 1: Write the failing test**

`backend/internal/store/users_workspace_test.go`:
```go
package store

import (
	"context"
	"errors"
	"testing"
)

func TestUserCreateWithUsernameAndLookup(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	users := NewUserRepo(pool)

	u, err := users.Create(ctx, "alice@corp", "alice", []byte("rec"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if u.Username != "alice" {
		t.Fatalf("username not set: %+v", u)
	}

	// duplicate username → ErrUsernameTaken (distinct from email conflict)
	if _, err := users.Create(ctx, "other@corp", "alice", []byte("rec")); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("expected ErrUsernameTaken, got %v", err)
	}
	// duplicate email → ErrConflict
	if _, err := users.Create(ctx, "alice@corp", "alice2", []byte("rec")); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}

	// lookup by email and by username both resolve
	byEmail, err := users.FindByEmailOrUsername(ctx, "alice@corp")
	if err != nil || byEmail.ID != u.ID {
		t.Fatalf("find by email: %v %+v", err, byEmail)
	}
	byName, err := users.FindByEmailOrUsername(ctx, "alice")
	if err != nil || byName.ID != u.ID {
		t.Fatalf("find by username: %v %+v", err, byName)
	}
	if _, err := users.FindByEmailOrUsername(ctx, "ghost"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/store/ -run TestUserCreateWithUsername -v`
Expected: FAIL — compile error (Create arity) / `ErrUsernameTaken` undefined.

- [ ] **Step 3: Implement**

In `backend/internal/store/store.go`, add to the error block and `User`:
```go
var (
	ErrNotFound      = errors.New("store: not found")
	ErrConflict      = errors.New("store: conflict")
	ErrUsernameTaken = errors.New("store: username taken")
)

// User is an account record.
type User struct {
	ID           string
	Email        string
	Username     string
	OpaqueRecord []byte
}
```

Replace `backend/internal/store/users.go` body:
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

func (r *UserRepo) Create(ctx context.Context, email, username string, opaqueRecord []byte) (*User, error) {
	var id string
	err := r.pool.QueryRow(ctx,
		`INSERT INTO users (email, username, opaque_record) VALUES ($1, $2, $3) RETURNING id`,
		email, username, opaqueRecord).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			if pgErr.ConstraintName == "users_username_key" {
				return nil, ErrUsernameTaken
			}
			return nil, ErrConflict
		}
		return nil, err
	}
	return &User{ID: id, Email: email, Username: username, OpaqueRecord: opaqueRecord}, nil
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, username, opaque_record FROM users WHERE email=$1`, email).
		Scan(&u.ID, &u.Email, &u.Username, &u.OpaqueRecord)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// FindByEmailOrUsername resolves an existing account for workspace invites/adds.
func (r *UserRepo) FindByEmailOrUsername(ctx context.Context, q string) (*User, error) {
	var u User
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, username, opaque_record FROM users WHERE email=$1 OR username=$1`, q).
		Scan(&u.ID, &u.Email, &u.Username, &u.OpaqueRecord)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}
```

- [ ] **Step 4: Fix all callers (compiler-guided), then run tests**

Run `cd backend && go build ./... 2>&1 | head` and update every `users.Create(ctx, email, rec)` to `users.Create(ctx, email, <username>, rec)` (use a sensible username, e.g. derived from the email local-part). Known test call sites to update: `internal/store/users_test.go`, `internal/store/devices_test.go`, and any others the compiler flags. (The `as` package's `UserStore.Create` and its fake are updated in Task 3 — `go build ./...` will still flag them now; if so, apply the Task-3 interface change here so the build is green, and Task 3 then only adds validation + the handler.)

Run: `cd backend && go test ./internal/store/ -run TestUserCreateWithUsername -v` → PASS, and `go test ./internal/store/` → all green.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/internal/store
git -C /Users/denisurevic/Documents/slack commit -m "feat(backend): users gain username + email-or-username lookup

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 3: AS registration accepts username

**Files:**
- Modify: `backend/internal/as/as.go` (`UserStore.Create` signature, `RegisterFinish` arg + validation, `ErrInvalidUsername`, `ValidateUsername`)
- Modify: `backend/internal/as/as_test.go` (fake `Create` signature + a username case)
- Modify: `backend/internal/httpapi/dto.go` (`registerFinishReq` += `Username`)
- Modify: `backend/internal/httpapi/auth_handlers.go` (`registerFinish` passes username, maps errors)

- [ ] **Step 1: Write the failing test**

ADD to `backend/internal/as/as_test.go`:
```go
func TestValidateUsername(t *testing.T) {
	ok := []string{"alice", "bob_99", "a_b_c", "abc"}
	bad := []string{"ab", "Alice", "has space", "no-dash", "waaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaay_too_long_xxxxx", ""}
	for _, s := range ok {
		if err := ValidateUsername(s); err != nil {
			t.Errorf("expected %q valid, got %v", s, err)
		}
	}
	for _, s := range bad {
		if err := ValidateUsername(s); err == nil {
			t.Errorf("expected %q invalid", s)
		}
	}
}
```
Also: the fake user store in `as_test.go` currently has `Create(_ context.Context, email string, rec []byte)`. Update its signature to `Create(_ context.Context, email, username string, rec []byte)` and store/return `Username: username`. Update the `newFakeUsers` map usage if it keys by email (keep keying by email; also enforce no-op for username here — uniqueness is the DB's job, covered in Task 2).

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/as/ -run TestValidateUsername -v`
Expected: FAIL — `ValidateUsername` undefined.

- [ ] **Step 3: Implement**

In `backend/internal/as/as.go`:
- Add import `"regexp"`.
- Add near `ErrAuthFailed`:
```go
var ErrInvalidUsername = errors.New("as: invalid username")

var usernameRe = regexp.MustCompile(`^[a-z0-9_]{3,32}$`)

// ValidateUsername enforces the public username format (lowercase, digits, underscore).
func ValidateUsername(s string) error {
	if !usernameRe.MatchString(s) {
		return ErrInvalidUsername
	}
	return nil
}
```
- Change the `UserStore` interface `Create` and `RegisterFinish`:
```go
type UserStore interface {
	Create(ctx context.Context, email, username string, opaqueRecord []byte) (*store.User, error)
	GetByEmail(ctx context.Context, email string) (*store.User, error)
}

func (s *Service) RegisterFinish(ctx context.Context, email, username string, record []byte) error {
	if err := ValidateUsername(username); err != nil {
		return err
	}
	_, err := s.users.Create(ctx, email, username, record)
	return err
}
```

In `backend/internal/httpapi/dto.go`, extend:
```go
type registerFinishReq struct {
	Email                    string `json:"email"`
	Username                 string `json:"username"`
	OpaqueRegistrationRecord string `json:"opaque_registration_record"`
}
```

In `backend/internal/httpapi/auth_handlers.go`, update `registerFinish`:
```go
func (h *authHandlers) registerFinish(w http.ResponseWriter, r *http.Request) {
	var req registerFinishReq
	if !decodeJSON(w, r, &req) {
		return
	}
	rec, ok := decodeB64(w, req.OpaqueRegistrationRecord)
	if !ok {
		return
	}
	err := h.svc.RegisterFinish(r.Context(), req.Email, req.Username, rec)
	switch {
	case errors.Is(err, as.ErrInvalidUsername):
		writeError(w, http.StatusBadRequest, "bad_request", "invalid username")
	case errors.Is(err, store.ErrUsernameTaken):
		writeError(w, http.StatusConflict, "conflict", "username taken")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "account already exists")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal", "could not store record")
	default:
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}
```

- [ ] **Step 4: Run tests**

Run: `cd backend && go test ./internal/as/ ./internal/httpapi/ -v` and `go build ./...`
Expected: PASS, build green.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/internal/as backend/internal/httpapi/dto.go backend/internal/httpapi/auth_handlers.go
git -C /Users/denisurevic/Documents/slack commit -m "feat(backend): registration captures validated username

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 4: WorkspaceRepo (store)

**Files:**
- Create: `backend/internal/store/workspaces.go`
- Test: `backend/internal/store/workspaces_test.go`

- [ ] **Step 1: Write the failing test**

`backend/internal/store/workspaces_test.go`:
```go
package store

import (
	"context"
	"errors"
	"testing"
)

func TestWorkspaceRepoLifecycle(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	users := NewUserRepo(pool)
	repo := NewWorkspaceRepo(pool)

	owner, _ := users.Create(ctx, "owner@corp", "owner", []byte("r"))
	bob, _ := users.Create(ctx, "bob@corp", "bob", []byte("r"))

	ws, err := repo.Create(ctx, "Acme", "acme", owner.ID)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// owner membership created in the same tx
	role, err := repo.RoleOf(ctx, ws.ID, owner.ID)
	if err != nil || role != RoleOwner {
		t.Fatalf("owner role: %v %q", err, role)
	}
	// non-member → ErrNotFound
	if _, err := repo.RoleOf(ctx, ws.ID, bob.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	// slug uniqueness probe
	exists, _ := repo.SlugExists(ctx, "acme")
	if !exists {
		t.Fatal("slug should exist")
	}

	// add bob as member; dup → ErrConflict
	if err := repo.AddMember(ctx, ws.ID, bob.ID, RoleMember); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := repo.AddMember(ctx, ws.ID, bob.ID, RoleMember); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict on dup add, got %v", err)
	}

	// members lists both with username/email
	ms, _ := repo.Members(ctx, ws.ID)
	if len(ms) != 2 {
		t.Fatalf("expected 2 members, got %d", len(ms))
	}
	// ListForUser returns the ws with bob's role
	list, _ := repo.ListForUser(ctx, bob.ID)
	if len(list) != 1 || list[0].Role != RoleMember || list[0].Slug != "acme" {
		t.Fatalf("ListForUser: %+v", list)
	}

	// set role and remove
	if err := repo.SetRole(ctx, ws.ID, bob.ID, RoleAdmin); err != nil {
		t.Fatalf("setrole: %v", err)
	}
	if r2, _ := repo.RoleOf(ctx, ws.ID, bob.ID); r2 != RoleAdmin {
		t.Fatalf("role not updated: %q", r2)
	}
	if err := repo.RemoveMember(ctx, ws.ID, bob.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := repo.RemoveMember(ctx, ws.ID, bob.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound removing absent member, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/store/ -run TestWorkspaceRepoLifecycle -v`
Expected: FAIL — `NewWorkspaceRepo` undefined.

- [ ] **Step 3: Implement**

`backend/internal/store/workspaces.go`:
```go
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
)

type Workspace struct {
	ID, Name, Slug, OwnerUserID string
	CreatedAt                   time.Time
}

type WorkspaceWithRole struct {
	ID, Name, Slug, Role string
}

type WorkspaceMember struct {
	UserID, Username, Email, Role string
}

type WorkspaceRepo struct{ pool *pgxpool.Pool }

func NewWorkspaceRepo(pool *pgxpool.Pool) *WorkspaceRepo { return &WorkspaceRepo{pool: pool} }

// Create inserts a workspace and its owner membership in one transaction.
func (r *WorkspaceRepo) Create(ctx context.Context, name, slug, ownerUserID string) (*Workspace, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var id string
	var createdAt time.Time
	err = tx.QueryRow(ctx,
		`INSERT INTO workspaces (name, slug, owner_user_id) VALUES ($1,$2,$3) RETURNING id, created_at`,
		name, slug, ownerUserID).Scan(&id, &createdAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrConflict
		}
		return nil, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1,$2,$3)`,
		id, ownerUserID, RoleOwner); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &Workspace{ID: id, Name: name, Slug: slug, OwnerUserID: ownerUserID, CreatedAt: createdAt}, nil
}

func (r *WorkspaceRepo) SlugExists(ctx context.Context, slug string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT FROM workspaces WHERE slug=$1)`, slug).Scan(&exists)
	return exists, err
}

func (r *WorkspaceRepo) ListForUser(ctx context.Context, userID string) ([]WorkspaceWithRole, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT w.id, w.name, w.slug, m.role
		   FROM workspace_members m JOIN workspaces w ON w.id = m.workspace_id
		  WHERE m.user_id=$1 ORDER BY w.created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkspaceWithRole
	for rows.Next() {
		var w WorkspaceWithRole
		if err := rows.Scan(&w.ID, &w.Name, &w.Slug, &w.Role); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (r *WorkspaceRepo) Members(ctx context.Context, workspaceID string) ([]WorkspaceMember, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT u.id, u.username, u.email, m.role
		   FROM workspace_members m JOIN users u ON u.id = m.user_id
		  WHERE m.workspace_id=$1 ORDER BY m.joined_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkspaceMember
	for rows.Next() {
		var m WorkspaceMember
		if err := rows.Scan(&m.UserID, &m.Username, &m.Email, &m.Role); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *WorkspaceRepo) RoleOf(ctx context.Context, workspaceID, userID string) (string, error) {
	var role string
	err := r.pool.QueryRow(ctx,
		`SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`,
		workspaceID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return role, err
}

func (r *WorkspaceRepo) AddMember(ctx context.Context, workspaceID, userID, role string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1,$2,$3)`,
		workspaceID, userID, role)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrConflict
		}
	}
	return err
}

func (r *WorkspaceRepo) RemoveMember(ctx context.Context, workspaceID, userID string) error {
	ct, err := r.pool.Exec(ctx,
		`DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, workspaceID, userID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *WorkspaceRepo) SetRole(ctx context.Context, workspaceID, userID, role string) error {
	ct, err := r.pool.Exec(ctx,
		`UPDATE workspace_members SET role=$3 WHERE workspace_id=$1 AND user_id=$2`,
		workspaceID, userID, role)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/store/ -run TestWorkspaceRepoLifecycle -v` → PASS.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/internal/store/workspaces.go backend/internal/store/workspaces_test.go
git -C /Users/denisurevic/Documents/slack commit -m "feat(backend): workspace repository (create, membership, roles)

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 5: workspace.Service (permission matrix)

**Files:**
- Create: `backend/internal/workspace/workspace.go`
- Test: `backend/internal/workspace/workspace_test.go`

- [ ] **Step 1: Write the failing test**

`backend/internal/workspace/workspace_test.go`:
```go
package workspace

import (
	"context"
	"errors"
	"testing"

	"github.com/messenger/backend/internal/store"
)

// fakeRepo is an in-memory Repo for authz/unit testing.
type fakeRepo struct {
	roles map[string]map[string]string // wsID -> userID -> role
	slugs map[string]bool
	seq   int
}

func newFakeRepo() *fakeRepo { return &fakeRepo{roles: map[string]map[string]string{}, slugs: map[string]bool{}} }

func (f *fakeRepo) Create(_ context.Context, name, slug, owner string) (*store.Workspace, error) {
	f.seq++
	id := "ws" + string(rune('0'+f.seq))
	f.roles[id] = map[string]string{owner: store.RoleOwner}
	f.slugs[slug] = true
	return &store.Workspace{ID: id, Name: name, Slug: slug, OwnerUserID: owner}, nil
}
func (f *fakeRepo) SlugExists(_ context.Context, slug string) (bool, error) { return f.slugs[slug], nil }
func (f *fakeRepo) ListForUser(context.Context, string) ([]store.WorkspaceWithRole, error) {
	return nil, nil
}
func (f *fakeRepo) Members(context.Context, string) ([]store.WorkspaceMember, error) { return nil, nil }
func (f *fakeRepo) RoleOf(_ context.Context, ws, u string) (string, error) {
	if r, ok := f.roles[ws][u]; ok {
		return r, nil
	}
	return "", store.ErrNotFound
}
func (f *fakeRepo) AddMember(_ context.Context, ws, u, role string) error {
	if _, ok := f.roles[ws][u]; ok {
		return store.ErrConflict
	}
	f.roles[ws][u] = role
	return nil
}
func (f *fakeRepo) RemoveMember(_ context.Context, ws, u string) error {
	if _, ok := f.roles[ws][u]; !ok {
		return store.ErrNotFound
	}
	delete(f.roles[ws], u)
	return nil
}
func (f *fakeRepo) SetRole(_ context.Context, ws, u, role string) error {
	if _, ok := f.roles[ws][u]; !ok {
		return store.ErrNotFound
	}
	f.roles[ws][u] = role
	return nil
}

type fakeUsers struct{ byKey map[string]*store.User }

func (f *fakeUsers) FindByEmailOrUsername(_ context.Context, q string) (*store.User, error) {
	if u, ok := f.byKey[q]; ok {
		return u, nil
	}
	return nil, store.ErrNotFound
}

func setup() (*Service, *fakeRepo, string) {
	repo := newFakeRepo()
	users := &fakeUsers{byKey: map[string]*store.User{
		"bob@corp": {ID: "bob", Email: "bob@corp", Username: "bob"},
		"bob":      {ID: "bob", Email: "bob@corp", Username: "bob"},
		"carol":    {ID: "carol", Email: "carol@corp", Username: "carol"},
	}}
	svc := NewService(repo, users)
	ws, _ := svc.Create(context.Background(), "owner", "Acme")
	return svc, repo, ws.ID
}

func TestCreateMakesOwnerAndSlug(t *testing.T) {
	svc, _, _ := setup()
	ws, err := svc.Create(context.Background(), "u2", "Acme")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if ws.Slug != "acme-2" { // deduped against the one created in setup
		t.Fatalf("expected deduped slug acme-2, got %q", ws.Slug)
	}
}

func TestAddMemberAuthz(t *testing.T) {
	ctx := context.Background()
	svc, repo, ws := setup()

	// member cannot add
	repo.roles[ws]["m"] = store.RoleMember
	if _, err := svc.AddMember(ctx, "m", ws, "bob@corp"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("member add should be forbidden, got %v", err)
	}
	// non-member caller → ErrNotMember
	if _, err := svc.AddMember(ctx, "stranger", ws, "bob@corp"); !errors.Is(err, ErrNotMember) {
		t.Fatalf("stranger → ErrNotMember, got %v", err)
	}
	// owner adds bob (by email) as member
	m, err := svc.AddMember(ctx, "owner", ws, "bob@corp")
	if err != nil || m.Role != store.RoleMember || m.UserID != "bob" {
		t.Fatalf("owner add: %v %+v", err, m)
	}
	// dup → ErrConflict
	if _, err := svc.AddMember(ctx, "owner", ws, "bob"); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("dup add → ErrConflict, got %v", err)
	}
	// unknown user → ErrNotFound
	if _, err := svc.AddMember(ctx, "owner", ws, "ghost"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown user → ErrNotFound, got %v", err)
	}
}

func TestSetRoleAndRemoveAuthz(t *testing.T) {
	ctx := context.Background()
	svc, repo, ws := setup()
	repo.roles[ws]["admin1"] = store.RoleAdmin
	repo.roles[ws]["mem1"] = store.RoleMember

	// only owner sets roles
	if err := svc.SetRole(ctx, "admin1", ws, "mem1", store.RoleAdmin); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin set role forbidden, got %v", err)
	}
	if err := svc.SetRole(ctx, "owner", ws, "mem1", store.RoleAdmin); err != nil {
		t.Fatalf("owner set role: %v", err)
	}
	// nobody can change the owner's role
	if err := svc.SetRole(ctx, "owner", ws, "owner", store.RoleMember); !errors.Is(err, ErrForbidden) {
		t.Fatalf("demote owner forbidden, got %v", err)
	}
	// admin removes only members, not admins
	repo.roles[ws]["mem2"] = store.RoleMember
	if err := svc.RemoveMember(ctx, "admin1", ws, "mem1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("admin removing admin forbidden, got %v", err) // mem1 was promoted to admin above
	}
	if err := svc.RemoveMember(ctx, "admin1", ws, "mem2"); err != nil {
		t.Fatalf("admin remove member: %v", err)
	}
	// owner cannot be removed
	if err := svc.RemoveMember(ctx, "owner", ws, "owner"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("remove owner forbidden, got %v", err)
	}
	// owner cannot leave; admin/member can
	if err := svc.Leave(ctx, "owner", ws); !errors.Is(err, ErrForbidden) {
		t.Fatalf("owner leave forbidden, got %v", err)
	}
	if err := svc.Leave(ctx, "admin1", ws); err != nil {
		t.Fatalf("admin leave: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/workspace/ -v`
Expected: FAIL — package/`NewService` undefined.

- [ ] **Step 3: Implement**

`backend/internal/workspace/workspace.go`:
```go
package workspace

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/messenger/backend/internal/store"
)

var (
	ErrForbidden = errors.New("workspace: forbidden")
	ErrNotMember = errors.New("workspace: not a member")
	ErrInvalid   = errors.New("workspace: invalid input")
)

type Repo interface {
	Create(ctx context.Context, name, slug, ownerUserID string) (*store.Workspace, error)
	SlugExists(ctx context.Context, slug string) (bool, error)
	ListForUser(ctx context.Context, userID string) ([]store.WorkspaceWithRole, error)
	Members(ctx context.Context, workspaceID string) ([]store.WorkspaceMember, error)
	RoleOf(ctx context.Context, workspaceID, userID string) (string, error)
	AddMember(ctx context.Context, workspaceID, userID, role string) error
	RemoveMember(ctx context.Context, workspaceID, userID string) error
	SetRole(ctx context.Context, workspaceID, userID, role string) error
}

type UserLookup interface {
	FindByEmailOrUsername(ctx context.Context, q string) (*store.User, error)
}

type Service struct {
	repo  Repo
	users UserLookup
}

func NewService(repo Repo, users UserLookup) *Service { return &Service{repo: repo, users: users} }

func (s *Service) Create(ctx context.Context, ownerUserID, name string) (*store.Workspace, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalid
	}
	slug, err := s.uniqueSlug(ctx, slugify(name))
	if err != nil {
		return nil, err
	}
	return s.repo.Create(ctx, name, slug, ownerUserID)
}

func (s *Service) ListForUser(ctx context.Context, userID string) ([]store.WorkspaceWithRole, error) {
	return s.repo.ListForUser(ctx, userID)
}

func (s *Service) requireRole(ctx context.Context, workspaceID, userID string) (string, error) {
	role, err := s.repo.RoleOf(ctx, workspaceID, userID)
	if errors.Is(err, store.ErrNotFound) {
		return "", ErrNotMember
	}
	return role, err
}

func (s *Service) Members(ctx context.Context, callerID, workspaceID string) ([]store.WorkspaceMember, error) {
	if _, err := s.requireRole(ctx, workspaceID, callerID); err != nil {
		return nil, err
	}
	return s.repo.Members(ctx, workspaceID)
}

func (s *Service) AddMember(ctx context.Context, callerID, workspaceID, emailOrUsername string) (*store.WorkspaceMember, error) {
	role, err := s.requireRole(ctx, workspaceID, callerID)
	if err != nil {
		return nil, err
	}
	if role != store.RoleOwner && role != store.RoleAdmin {
		return nil, ErrForbidden
	}
	u, err := s.users.FindByEmailOrUsername(ctx, strings.TrimSpace(emailOrUsername))
	if err != nil {
		return nil, err // store.ErrNotFound → 404
	}
	if err := s.repo.AddMember(ctx, workspaceID, u.ID, store.RoleMember); err != nil {
		return nil, err // store.ErrConflict → 409
	}
	return &store.WorkspaceMember{UserID: u.ID, Username: u.Username, Email: u.Email, Role: store.RoleMember}, nil
}

func (s *Service) SetRole(ctx context.Context, callerID, workspaceID, targetUserID, role string) error {
	if role != store.RoleAdmin && role != store.RoleMember {
		return ErrInvalid
	}
	callerRole, err := s.requireRole(ctx, workspaceID, callerID)
	if err != nil {
		return err
	}
	if callerRole != store.RoleOwner {
		return ErrForbidden
	}
	targetRole, err := s.repo.RoleOf(ctx, workspaceID, targetUserID)
	if errors.Is(err, store.ErrNotFound) {
		return store.ErrNotFound
	}
	if err != nil {
		return err
	}
	if targetRole == store.RoleOwner {
		return ErrForbidden // owner role is immutable in v1
	}
	return s.repo.SetRole(ctx, workspaceID, targetUserID, role)
}

func (s *Service) RemoveMember(ctx context.Context, callerID, workspaceID, targetUserID string) error {
	callerRole, err := s.requireRole(ctx, workspaceID, callerID)
	if err != nil {
		return err
	}
	targetRole, err := s.repo.RoleOf(ctx, workspaceID, targetUserID)
	if errors.Is(err, store.ErrNotFound) {
		return store.ErrNotFound
	}
	if err != nil {
		return err
	}
	if targetRole == store.RoleOwner {
		return ErrForbidden // owner cannot be removed
	}
	switch callerRole {
	case store.RoleOwner:
		// may remove admins and members
	case store.RoleAdmin:
		if targetRole != store.RoleMember {
			return ErrForbidden
		}
	default:
		return ErrForbidden
	}
	return s.repo.RemoveMember(ctx, workspaceID, targetUserID)
}

func (s *Service) Leave(ctx context.Context, callerID, workspaceID string) error {
	role, err := s.requireRole(ctx, workspaceID, callerID)
	if err != nil {
		return err
	}
	if role == store.RoleOwner {
		return ErrForbidden // owner must transfer ownership first (future plan)
	}
	return s.repo.RemoveMember(ctx, workspaceID, callerID)
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(name string) string {
	s := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if s == "" {
		s = "ws"
	}
	return s
}

func (s *Service) uniqueSlug(ctx context.Context, base string) (string, error) {
	candidate := base
	for i := 2; i <= 1000; i++ {
		exists, err := s.repo.SlugExists(ctx, candidate)
		if err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
	return "", ErrInvalid
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/workspace/ -v` → PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/internal/workspace
git -C /Users/denisurevic/Documents/slack commit -m "feat(backend): workspace service with owner/admin/member authz

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 6: Workspace HTTP handlers + routes

**Files:**
- Create: `backend/internal/httpapi/workspace_handlers.go`
- Modify: `backend/internal/httpapi/dto.go` (workspace DTOs)
- Modify: `backend/internal/httpapi/router.go` (`NewRouterFull` gains `wsSvc`, routes)
- Test: `backend/internal/httpapi/workspace_handlers_test.go`

- [ ] **Step 1: Write the failing test**

`backend/internal/httpapi/workspace_handlers_test.go`:
```go
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/messenger/backend/internal/session"
	"github.com/messenger/backend/internal/store"
	"github.com/messenger/backend/internal/workspace"
)

// fakeWS implements wsService for HTTP-mapping tests.
type fakeWS struct {
	createErr, addErr, roleErr, rmErr, leaveErr error
	addResult                                   *store.WorkspaceMember
}

func (f *fakeWS) Create(_ context.Context, owner, name string) (*store.Workspace, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &store.Workspace{ID: "w1", Name: name, Slug: "w1", OwnerUserID: owner}, nil
}
func (f *fakeWS) ListForUser(context.Context, string) ([]store.WorkspaceWithRole, error) {
	return []store.WorkspaceWithRole{{ID: "w1", Name: "W", Slug: "w1", Role: "owner"}}, nil
}
func (f *fakeWS) Members(context.Context, string, string) ([]store.WorkspaceMember, error) {
	return []store.WorkspaceMember{{UserID: "u1", Username: "a", Email: "a@c", Role: "owner"}}, nil
}
func (f *fakeWS) AddMember(context.Context, string, string, string) (*store.WorkspaceMember, error) {
	if f.addErr != nil {
		return nil, f.addErr
	}
	return f.addResult, nil
}
func (f *fakeWS) SetRole(context.Context, string, string, string, string) error  { return f.roleErr }
func (f *fakeWS) RemoveMember(context.Context, string, string, string) error     { return f.rmErr }
func (f *fakeWS) Leave(context.Context, string, string) error                    { return f.leaveErr }

func withSession(r *http.Request) *http.Request {
	ctx := context.WithValue(r.Context(), sessionCtxKey, sessionWithToken{Session: &session.Session{UserID: "caller"}})
	return r.WithContext(ctx)
}

func TestWorkspaceCreate(t *testing.T) {
	h := &workspaceHandlers{svc: &fakeWS{}}
	req := withSession(httptest.NewRequest("POST", "/workspaces", strings.NewReader(`{"name":"Acme"}`)))
	rec := httptest.NewRecorder()
	h.create(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var resp workspaceResp
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Role != "owner" || resp.ID != "w1" {
		t.Fatalf("resp %+v", resp)
	}
}

func TestWorkspaceAddMemberMapping(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{nil, 200},
		{workspace.ErrNotMember, 404},
		{workspace.ErrForbidden, 403},
		{store.ErrNotFound, 404},
		{store.ErrConflict, 409},
	}
	for _, c := range cases {
		h := &workspaceHandlers{svc: &fakeWS{addErr: c.err, addResult: &store.WorkspaceMember{UserID: "bob", Role: "member"}}}
		req := withSession(httptest.NewRequest("POST", "/workspaces/w1/members", strings.NewReader(`{"email_or_username":"bob"}`)))
		req.SetPathValue("id", "w1")
		rec := httptest.NewRecorder()
		h.addMember(rec, req)
		if rec.Code != c.want {
			t.Fatalf("err %v → status %d, want %d", c.err, rec.Code, c.want)
		}
	}
}

func TestWorkspaceCreateInvalid(t *testing.T) {
	h := &workspaceHandlers{svc: &fakeWS{createErr: workspace.ErrInvalid}}
	req := withSession(httptest.NewRequest("POST", "/workspaces", strings.NewReader(`{"name":""}`)))
	rec := httptest.NewRecorder()
	h.create(rec, req)
	if rec.Code != 400 {
		t.Fatalf("status %d, want 400", rec.Code)
	}
	_ = errors.Is // keep errors imported
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/httpapi/ -run TestWorkspace -v`
Expected: FAIL — `workspaceHandlers`/`workspaceResp`/`wsService` undefined.

- [ ] **Step 3: Implement**

Add to `backend/internal/httpapi/dto.go`:
```go
type createWorkspaceReq struct {
	Name string `json:"name"`
}
type workspaceResp struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	Role string `json:"role"`
}
type addWSMemberReq struct {
	EmailOrUsername string `json:"email_or_username"`
}
type setWSRoleReq struct {
	Role string `json:"role"`
}
type wsMemberResp struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Role     string `json:"role"`
}
```

`backend/internal/httpapi/workspace_handlers.go`:
```go
package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/messenger/backend/internal/store"
	"github.com/messenger/backend/internal/workspace"
)

// wsService is the workspace behaviour the HTTP layer needs (*workspace.Service satisfies it).
type wsService interface {
	Create(ctx context.Context, ownerUserID, name string) (*store.Workspace, error)
	ListForUser(ctx context.Context, userID string) ([]store.WorkspaceWithRole, error)
	Members(ctx context.Context, callerID, workspaceID string) ([]store.WorkspaceMember, error)
	AddMember(ctx context.Context, callerID, workspaceID, emailOrUsername string) (*store.WorkspaceMember, error)
	SetRole(ctx context.Context, callerID, workspaceID, targetUserID, role string) error
	RemoveMember(ctx context.Context, callerID, workspaceID, targetUserID string) error
	Leave(ctx context.Context, callerID, workspaceID string) error
}

type workspaceHandlers struct{ svc wsService }

func writeWorkspaceErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workspace.ErrNotMember):
		writeError(w, http.StatusNotFound, "not_found", "workspace not found")
	case errors.Is(err, workspace.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden", "insufficient role")
	case errors.Is(err, workspace.ErrInvalid):
		writeError(w, http.StatusBadRequest, "bad_request", "invalid input")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", "already a member")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "not found")
	default:
		writeError(w, http.StatusInternalServerError, "internal", "workspace error")
	}
}

func (h *workspaceHandlers) create(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	var req createWorkspaceReq
	if !decodeJSON(w, r, &req) {
		return
	}
	ws, err := h.svc.Create(r.Context(), swt.Session.UserID, req.Name)
	if err != nil {
		writeWorkspaceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, workspaceResp{ID: ws.ID, Name: ws.Name, Slug: ws.Slug, Role: store.RoleOwner})
}

func (h *workspaceHandlers) list(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	ls, err := h.svc.ListForUser(r.Context(), swt.Session.UserID)
	if err != nil {
		writeWorkspaceErr(w, err)
		return
	}
	out := make([]workspaceResp, 0, len(ls))
	for _, x := range ls {
		out = append(out, workspaceResp{ID: x.ID, Name: x.Name, Slug: x.Slug, Role: x.Role})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *workspaceHandlers) members(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	ms, err := h.svc.Members(r.Context(), swt.Session.UserID, r.PathValue("id"))
	if err != nil {
		writeWorkspaceErr(w, err)
		return
	}
	out := make([]wsMemberResp, 0, len(ms))
	for _, m := range ms {
		out = append(out, wsMemberResp{UserID: m.UserID, Username: m.Username, Email: m.Email, Role: m.Role})
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *workspaceHandlers) addMember(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	var req addWSMemberReq
	if !decodeJSON(w, r, &req) {
		return
	}
	m, err := h.svc.AddMember(r.Context(), swt.Session.UserID, r.PathValue("id"), req.EmailOrUsername)
	if err != nil {
		writeWorkspaceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, wsMemberResp{UserID: m.UserID, Username: m.Username, Email: m.Email, Role: m.Role})
}

func (h *workspaceHandlers) setRole(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	var req setWSRoleReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.svc.SetRole(r.Context(), swt.Session.UserID, r.PathValue("id"), r.PathValue("user"), req.Role); err != nil {
		writeWorkspaceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *workspaceHandlers) removeMember(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	if err := h.svc.RemoveMember(r.Context(), swt.Session.UserID, r.PathValue("id"), r.PathValue("user")); err != nil {
		writeWorkspaceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *workspaceHandlers) leave(w http.ResponseWriter, r *http.Request) {
	swt := sessionFrom(r.Context())
	if err := h.svc.Leave(r.Context(), swt.Session.UserID, r.PathValue("id")); err != nil {
		writeWorkspaceErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
```

In `backend/internal/httpapi/router.go`: add import `"github.com/messenger/backend/internal/workspace"`, extend `NewRouterFull` signature with `wsSvc *workspace.Service` (append as the last parameter), build `wh := &workspaceHandlers{svc: wsSvc}`, and register routes (place near the other authed routes):
```go
	mux.Handle("POST /workspaces", auth(http.HandlerFunc(wh.create)))
	mux.Handle("GET /workspaces", auth(http.HandlerFunc(wh.list)))
	mux.Handle("GET /workspaces/{id}/members", auth(http.HandlerFunc(wh.members)))
	mux.Handle("POST /workspaces/{id}/members", auth(http.HandlerFunc(wh.addMember)))
	mux.Handle("PATCH /workspaces/{id}/members/{user}", auth(http.HandlerFunc(wh.setRole)))
	mux.Handle("DELETE /workspaces/{id}/members/me", auth(http.HandlerFunc(wh.leave)))
	mux.Handle("DELETE /workspaces/{id}/members/{user}", auth(http.HandlerFunc(wh.removeMember)))
```
(Go 1.22 routing: the literal `…/members/me` is more specific than `…/members/{user}`, so both can coexist without conflict.)

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/httpapi/ -run TestWorkspace -v` → PASS.
(Build will fail at `cmd/server` until Task 7 passes the new arg — that's expected; `go test ./internal/httpapi/` still compiles in isolation. If you prefer green build now, do Task 7 before committing.)

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/internal/httpapi/workspace_handlers.go backend/internal/httpapi/workspace_handlers_test.go backend/internal/httpapi/dto.go backend/internal/httpapi/router.go
git -C /Users/denisurevic/Documents/slack commit -m "feat(backend): workspace HTTP handlers + routes

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 7: Wire workspace service into the server

**Files:**
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Implement**

In `backend/cmd/server/main.go`: add import `"github.com/messenger/backend/internal/workspace"`. After the existing service constructions (near `svc := as.NewService(...)`), add:
```go
	wsSvc := workspace.NewService(store.NewWorkspaceRepo(pool), store.NewUserRepo(pool))
```
Update the router call to pass it (append as the last argument, matching the Task-6 signature):
```go
	apiHandler := httpapi.NewRouterFull(svc, sess, devSvc, kpSvc, rl, rosterRepo, ktSvc, ktPub, wsSvc)
```

- [ ] **Step 2: Build + full backend test**

Run:
```bash
cd backend && go build ./... && go vet ./... && go test ./...
```
Expected: build green, all packages pass (integration tests require Docker running).

- [ ] **Step 3: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/cmd/server/main.go
git -C /Users/denisurevic/Documents/slack commit -m "feat(backend): mount workspace service in server router

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 8: Frontend — thread username through registration

**Files:**
- Modify: `client/src/shared/api/as.ts` (`registerFinish` takes username)
- Modify: `client/src/features/authenticate/authenticate.ts` (`register` takes username)
- Modify: `client/src/app/authFlow.ts` (`register` signature), `client/src/main.tsx` (onRegister)
- Modify: `client/src/widgets/login-form/LoginForm.tsx`, `client/src/pages/auth/AuthPage.tsx` (username field in register mode)
- Modify/extend tests: `client/src/shared/api/as.test.ts`, `client/src/features/authenticate/authenticate.test.ts`, `client/src/pages/auth/AuthPage.test.tsx`

**Note:** Read each file before editing — JSX details below are described, not quoted. Keep SolidJS props non-destructured.

- [ ] **Step 1: Update tests (red)**

In `client/src/shared/api/as.test.ts`: change the register-finish expectation so `registerFinish("a@corp", "alice", "RECORD")` POSTs `/auth/register/finish` with body containing `username: "alice"`.

In `client/src/features/authenticate/authenticate.test.ts`: update the register test so `register("a@corp", "alice", "pw")` calls `as.registerFinish` with the username threaded through.

In `client/src/pages/auth/AuthPage.test.tsx`: add a test that, in register mode, filling Email + Username + Password and submitting calls `onRegister(email, username, password)`. (Use `aria-label="Username"` for the new field.)

- [ ] **Step 2: Run to verify failures**

Run: `cd client && npx vitest run src/shared/api/as.test.ts src/features/authenticate/authenticate.test.ts src/pages/auth/AuthPage.test.tsx`
Expected: FAIL.

- [ ] **Step 3: Implement**

`as.ts` — change `registerFinish`:
```ts
async registerFinish(email: string, username: string, record: string): Promise<void> {
  await this.post("/auth/register/finish", { email, username, opaque_registration_record: record });
}
```
(Match the existing `post`/method shape in the file; only the signature and body gain `username`.)

`authenticate.ts` — change `register`:
```ts
async register(email: string, username: string, password: string): Promise<void> {
  const init = this.opaque.regInit(pwB64(password));
  const response = await this.as.registerStart(email, init.request);
  const fin = this.opaque.regFinalize(init.flowId, response, SERVER_ID_B64);
  await this.as.registerFinish(email, username, fin.record);
}
```
(Keep the existing helper names `pwB64`, `SERVER_ID_B64`; only add the `username` param and pass it to `registerFinish`. Update the `AsLike` port type's `registerFinish` to `(email, username, record) => Promise<void>`.)

`authFlow.ts` — change the `register` member and the `AuthFlowDeps.authenticator.register` type to `(email, username, password) => Promise<void>`:
```ts
register: (email: string, username: string, password: string) => deps.authenticator.register(email, username, password),
```

`main.tsx` — `onRegister={(email, username, pw) => void run(() => authFlow.register(email, username, pw))()}`.

`LoginForm.tsx` / `AuthPage.tsx` — add a Username input (`aria-label="Username"`) shown in register mode; thread it so the register submit calls `onRegister(email, username, password)`. `onLogin(email, password)` stays unchanged. Update the `AuthPage`/`LoginForm` prop types: `onRegister: (email: string, username: string, password: string) => void`.

- [ ] **Step 4: Run tests**

Run: `cd client && npx vitest run && npx tsc --noEmit`
Expected: all green, tsc clean.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add client/src
git -C /Users/denisurevic/Documents/slack commit -m "feat(client): capture username during registration

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 9: Frontend — typed workspace API client

**Files:**
- Create: `client/src/shared/api/workspace.ts`, `client/src/shared/api/workspace.test.ts`

- [ ] **Step 1: Write the failing test**

`client/src/shared/api/workspace.test.ts`:
```ts
import { describe, it, expect, vi } from "vitest";
import { WorkspaceClient } from "./workspace";

function mockFetch(status: number, body: unknown) {
  return vi.fn(async () => ({ ok: status < 400, status, json: async () => body }) as unknown as Response);
}

describe("WorkspaceClient", () => {
  it("create posts name with bearer token and returns the workspace", async () => {
    const f = mockFetch(200, { id: "w1", name: "Acme", slug: "acme", role: "owner" });
    const c = new WorkspaceClient("http://api", f);
    const ws = await c.create("TOK", "Acme");
    expect(ws.id).toBe("w1");
    const [url, init] = f.mock.calls[0];
    expect(url).toBe("http://api/workspaces");
    expect((init as RequestInit).method).toBe("POST");
    expect((init as any).headers.Authorization).toBe("Bearer TOK");
    expect(JSON.parse((init as any).body)).toEqual({ name: "Acme" });
  });

  it("list returns my workspaces", async () => {
    const f = mockFetch(200, [{ id: "w1", name: "Acme", slug: "acme", role: "member" }]);
    const c = new WorkspaceClient("http://api", f);
    const list = await c.list("TOK");
    expect(list).toHaveLength(1);
    expect(list[0].role).toBe("member");
  });

  it("addMember posts email_or_username", async () => {
    const f = mockFetch(200, { user_id: "u2", username: "bob", email: "bob@c", role: "member" });
    const c = new WorkspaceClient("http://api", f);
    const m = await c.addMember("TOK", "w1", "bob");
    expect(m.user_id).toBe("u2");
    expect(JSON.parse((f.mock.calls[0][1] as any).body)).toEqual({ email_or_username: "bob" });
  });

  it("throws on non-2xx", async () => {
    const f = mockFetch(403, { error: "forbidden", message: "nope" });
    const c = new WorkspaceClient("http://api", f);
    await expect(c.create("TOK", "X")).rejects.toThrow();
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd client && npx vitest run src/shared/api/workspace.test.ts`
Expected: FAIL — cannot find `./workspace`.

- [ ] **Step 3: Implement**

`client/src/shared/api/workspace.ts`:
```ts
type FetchFn = typeof fetch;

export interface Workspace {
  id: string;
  name: string;
  slug: string;
  role: string;
}
export interface WorkspaceMember {
  user_id: string;
  username: string;
  email: string;
  role: string;
}

// Typed client for the workspace endpoints. All calls carry the device-bound
// session token as a Bearer header (same token the AS issues).
export class WorkspaceClient {
  constructor(private baseURL: string, private fetchFn: FetchFn = fetch) {}

  private async call<T>(token: string, method: string, path: string, body?: unknown): Promise<T> {
    const init: RequestInit = {
      method,
      headers: { "Content-Type": "application/json", Authorization: `Bearer ${token}` },
    };
    if (body !== undefined) init.body = JSON.stringify(body);
    const res = await this.fetchFn(this.baseURL + path, init);
    if (!res.ok) throw new Error(`workspace ${method} ${path} failed: ${res.status}`);
    return (await res.json()) as T;
  }

  create(token: string, name: string): Promise<Workspace> {
    return this.call<Workspace>(token, "POST", "/workspaces", { name });
  }
  list(token: string): Promise<Workspace[]> {
    return this.call<Workspace[]>(token, "GET", "/workspaces");
  }
  members(token: string, workspaceId: string): Promise<WorkspaceMember[]> {
    return this.call<WorkspaceMember[]>(token, "GET", `/workspaces/${workspaceId}/members`);
  }
  addMember(token: string, workspaceId: string, emailOrUsername: string): Promise<WorkspaceMember> {
    return this.call<WorkspaceMember>(token, "POST", `/workspaces/${workspaceId}/members`, {
      email_or_username: emailOrUsername,
    });
  }
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd client && npx vitest run src/shared/api/workspace.test.ts` → PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add client/src/shared/api/workspace.ts client/src/shared/api/workspace.test.ts
git -C /Users/denisurevic/Documents/slack commit -m "feat(client): typed workspace API client

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

### Task 10: Frontend — workspace context, gate, bootstrap wiring + full verify

**Files:**
- Create: `client/src/entities/workspace/store.ts`, `store.test.ts`
- Create: `client/src/features/workspaces/workspaces.ts`, `workspaces.test.ts`
- Create: `client/src/pages/workspace/WorkspacePage.tsx`, `WorkspacePage.test.tsx`
- Modify: `client/src/app/App.tsx`, `App.test.tsx` (3-way gate), `client/src/app/bootstrap.ts`, `client/src/main.tsx`

- [ ] **Step 1: Write failing tests**

`client/src/entities/workspace/store.test.ts`:
```ts
import { describe, it, expect } from "vitest";
import { createWorkspaceStore } from "./store";

describe("workspace store", () => {
  it("tracks my workspaces and the current selection", () => {
    const s = createWorkspaceStore();
    expect(s.current()).toBeNull();
    s.setList([{ id: "w1", name: "Acme", slug: "acme", role: "owner" }]);
    expect(s.list()).toHaveLength(1);
    s.select("w1");
    expect(s.current()).toBe("w1");
    s.clear();
    expect(s.current()).toBeNull();
    expect(s.list()).toHaveLength(0);
  });
});
```

`client/src/features/workspaces/workspaces.test.ts`:
```ts
import { describe, it, expect, vi } from "vitest";
import { createWorkspaces } from "./workspaces";
import { createWorkspaceStore } from "../../entities/workspace/store";

function deps(listResult: any[] = []) {
  return {
    client: {
      create: vi.fn(async () => ({ id: "w1", name: "Acme", slug: "acme", role: "owner" })),
      list: vi.fn(async () => listResult),
    },
    store: createWorkspaceStore(),
    token: () => "TOK",
  };
}

describe("workspaces feature", () => {
  it("load fills the store and auto-selects when exactly one exists", async () => {
    const d = deps([{ id: "w1", name: "Acme", slug: "acme", role: "member" }]);
    await createWorkspaces(d as any).load();
    expect(d.client.list).toHaveBeenCalledWith("TOK");
    expect(d.store.list()).toHaveLength(1);
    expect(d.store.current()).toBe("w1");
  });

  it("load does not auto-select when there are zero or many", async () => {
    const d = deps([]);
    await createWorkspaces(d as any).load();
    expect(d.store.current()).toBeNull();
  });

  it("create adds the workspace and selects it", async () => {
    const d = deps([]);
    await createWorkspaces(d as any).create("Acme");
    expect(d.client.create).toHaveBeenCalledWith("TOK", "Acme");
    expect(d.store.current()).toBe("w1");
  });
});
```

ADD to `client/src/app/App.test.tsx` a 3-way gate test: when onboarded **and** a workspace is selected → ChatPage (existing chat assertions); when onboarded but no workspace selected → the WorkspacePage (assert its heading text, e.g. "Workspaces"). The `App` props gain `workspace` (the store) and workspace callbacks (`onCreateWorkspace`, `onSelectWorkspace`). Update the existing onboarded tests to also select a workspace so they still reach ChatPage.

`client/src/pages/workspace/WorkspacePage.test.tsx`: render `WorkspacePage` with a list and assert it shows entries and that clicking one calls `onSelect(id)`; entering a name and submitting calls `onCreate(name)`.

- [ ] **Step 2: Run to verify failures**

Run: `cd client && npx vitest run src/entities/workspace src/features/workspaces src/pages/workspace src/app/App.test.tsx`
Expected: FAIL.

- [ ] **Step 3: Implement**

`client/src/entities/workspace/store.ts`:
```ts
import { createSignal } from "solid-js";
import type { Workspace } from "../../shared/api/workspace";

// Current-workspace context: list of my workspaces + the active selection.
// Kept in memory like the session token (no persistence in this milestone).
export function createWorkspaceStore() {
  const [list, setList] = createSignal<Workspace[]>([]);
  const [current, setCurrent] = createSignal<string | null>(null);
  return {
    list,
    current,
    setList: (ws: Workspace[]) => setList(ws),
    select: (id: string) => setCurrent(id),
    clear: () => {
      setList([]);
      setCurrent(null);
    },
  };
}

export type WorkspaceStore = ReturnType<typeof createWorkspaceStore>;
```

`client/src/features/workspaces/workspaces.ts`:
```ts
import type { WorkspaceStore } from "../../entities/workspace/store";
import type { Workspace } from "../../shared/api/workspace";

export interface WorkspacesDeps {
  client: {
    create(token: string, name: string): Promise<Workspace>;
    list(token: string): Promise<Workspace[]>;
  };
  store: WorkspaceStore;
  token: () => string;
}

// Use-cases for the workspace gate. Auto-selects when the user has exactly one
// workspace so the common case skips the picker.
export function createWorkspaces(deps: WorkspacesDeps) {
  return {
    async load(): Promise<void> {
      const ws = await deps.client.list(deps.token());
      deps.store.setList(ws);
      if (ws.length === 1) deps.store.select(ws[0].id);
    },
    async create(name: string): Promise<void> {
      const ws = await deps.client.create(deps.token(), name);
      deps.store.setList([...deps.store.list(), ws]);
      deps.store.select(ws.id);
    },
  };
}
```

`client/src/pages/workspace/WorkspacePage.tsx` (presentational, prop-driven, non-destructured props):
```tsx
import { For, createSignal, type Component } from "solid-js";
import type { Workspace } from "../../shared/api/workspace";
import { Button } from "../../shared/ui/Button";

export const WorkspacePage: Component<{
  workspaces: Workspace[];
  onSelect: (id: string) => void;
  onCreate: (name: string) => void;
  busy: boolean;
}> = (props) => {
  const [name, setName] = createSignal("");
  return (
    <div>
      <h1>Workspaces</h1>
      <ul>
        <For each={props.workspaces}>
          {(w) => (
            <li>
              <button onClick={() => props.onSelect(w.id)}>{w.name}</button>
            </li>
          )}
        </For>
      </ul>
      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (name().trim()) props.onCreate(name().trim());
        }}
      >
        <input aria-label="Workspace name" value={name()} onInput={(e) => setName(e.currentTarget.value)} />
        <Button type="submit" disabled={props.busy}>Create</Button>
      </form>
    </div>
  );
};
```
(If `Button` doesn't accept `type`/`disabled`, use a plain `<button>` — read `shared/ui/Button` first.)

`client/src/app/App.tsx` — 3-way gate (anonymous → AuthPage; onboarded but no current workspace → WorkspacePage; else → ChatPage). Read the current file and extend it; the new props are `workspace: WorkspaceStore`, `onCreateWorkspace: (name: string) => void`, `onSelectWorkspace: (id: string) => void`. Sketch:
```tsx
import { Show, type Component } from "solid-js";
import { ChatPage } from "../pages/chat/ChatPage";
import { AuthPage } from "../pages/auth/AuthPage";
import { WorkspacePage } from "../pages/workspace/WorkspacePage";
import type { ConversationStore } from "../entities/conversation/store";
import type { ConnectionStore } from "../entities/connection/store";
import type { SessionStore } from "../entities/session/store";
import type { WorkspaceStore } from "../entities/workspace/store";

export const App: Component<{
  groupId: string;
  conversation: ConversationStore;
  connection: ConnectionStore;
  session: SessionStore;
  workspace: WorkspaceStore;
  onLogin: (email: string, password: string) => void;
  onRegister: (email: string, username: string, password: string) => void;
  onCreateWorkspace: (name: string) => void;
  onSelectWorkspace: (id: string) => void;
  onSend: (text: string) => void;
  authError: string;
  busy: boolean;
}> = (props) => (
  <Show
    when={props.session.status() !== "anonymous"}
    fallback={<AuthPage onLogin={props.onLogin} onRegister={props.onRegister} error={props.authError} busy={props.busy} />}
  >
    <Show
      when={props.workspace.current() !== null}
      fallback={
        <WorkspacePage
          workspaces={props.workspace.list()}
          onSelect={props.onSelectWorkspace}
          onCreate={props.onCreateWorkspace}
          busy={props.busy}
        />
      }
    >
      <ChatPage
        messages={props.conversation.messages(props.groupId)}
        status={props.connection.status()}
        onSend={props.onSend}
      />
    </Show>
  </Show>
);
```

`client/src/app/bootstrap.ts` — construct the workspace store, client, and feature; return them. Add:
```ts
import { WorkspaceClient } from "../shared/api/workspace";
import { createWorkspaceStore } from "../entities/workspace/store";
import { createWorkspaces } from "../features/workspaces/workspaces";
```
Inside `bootstrap()`, after `session`/`as` are built:
```ts
  const workspace = createWorkspaceStore();
  const wsClient = new WorkspaceClient(DS_HTTP_URL);
  const workspaces = createWorkspaces({
    client: wsClient,
    store: workspace,
    token: () => session.token() ?? "",
  });
```
Extend the return object with `workspace` and `workspaces`. Also: after a successful `authFlow` connect, the app should load workspaces — wire `workspaces.load()` to run once the session has a token (call it from `main.tsx` after login succeeds, see below).

`client/src/main.tsx` — pass the new props and load workspaces after login:
```tsx
const { orchestrator, conversation, connection, session, workspace, workspaces, authFlow } = bootstrap("device");
// ...existing run() helper...
const onLogin = (email: string, pw: string) =>
  void run(async () => {
    await authFlow.login(email, pw);
    await workspaces.load();
  })();
// in JSX:
//   workspace={workspace}
//   onLogin={(email, pw) => onLogin(email, pw)}
//   onRegister={(email, username, pw) => void run(() => authFlow.register(email, username, pw))()}
//   onCreateWorkspace={(name) => void run(() => workspaces.create(name))()}
//   onSelectWorkspace={(id) => workspace.select(id)}
```

- [ ] **Step 4: FULL VERIFICATION**

Run:
```bash
cd client && npx vitest run && npx tsc --noEmit && npx vite build
cd /Users/denisurevic/Documents/slack/backend && go build ./... && go test ./...
```
Expected: all client tests green (workspace store/feature/page, App 3-way gate, register-username, existing), tsc clean, vite build OK; backend builds and all packages pass (Docker required for integration tests). Report a per-area summary.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add client/src
git -C /Users/denisurevic/Documents/slack commit -m "feat(client): workspace gate — create/select before chat

Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>"
```

---

## Self-Review

**1. Spec coverage:**
- `workspaces`, `workspace_members`, `users.username` migration → Task 1 ✓
- username unique + registration captures it → Tasks 1, 2, 3, 8 ✓
- `internal/workspace` module (repo+service+http) over AS session → Tasks 4, 5, 6, 7 ✓
- Create WS → owner; list mine; members → Tasks 4, 5, 6 ✓
- Direct add of existing user (owner/admin) by email/username → Tasks 2 (lookup), 5, 6 ✓
- Role matrix (owner>admin>member; owner immutable; admin limits; leave) → Task 5 ✓ (table-driven authz tests)
- Frontend: workspace API client, current-workspace context, create/select gate → Tasks 9, 10 ✓
- Errors: non-member→404, forbidden→403, dup→409, unknown user→404, bad input→400, username taken→409 → Tasks 3, 6 ✓
- Out of scope (conversations/DM/search, email invites, cross-WS, conv/KT scoping, ownership transfer) → untouched ✓

**2. Placeholder scan:** No TBD/TODO. JSX-level frontend edits (AuthPage/LoginForm/App) provide the test as the contract plus exact prop signatures and a code sketch, with an explicit "read the file first" instruction — necessary because those files' current internals aren't quoted here. All backend code is complete.

**3. Type consistency:** `store.User` gains `Username` (Tasks 2,4,5); `UserRepo.Create(email, username, rec)` consistent across Tasks 2,3 and all call sites; `as.UserStore.Create` matches; `workspace.Repo`/`UserLookup` interfaces match `store.WorkspaceRepo`/`UserRepo` method sets (Tasks 4,5); `wsService` (httpapi) matches `*workspace.Service` methods (Tasks 5,6); `NewRouterFull` gains `wsSvc` and the `cmd/server` call updates to match (Tasks 6,7); frontend `register(email, username, password)` consistent across as.ts/authenticate/authFlow/main/AuthPage (Task 8); `WorkspaceClient`/`Workspace`/`WorkspaceMember` shapes consistent across Tasks 9,10; `App` props consistent across Tasks 10.

**4. Known sequencing note:** After Task 6 the `cmd/server` build is red until Task 7 (the router signature changed) — both tasks call this out, and Task 7's verification runs the full `go build ./... && go test ./...`. Implementers doing strict per-task green builds should pair Tasks 6 and 7.
