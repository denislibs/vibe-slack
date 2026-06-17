# Conversations Backend (6.1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development. Steps use checkbox (`- [ ]`) syntax.

**Goal:** Workspace-scoped conversations (DM + public/private channels) with a user-membership + authz layer over the MLS-agnostic DS journal, member search, device-roster authz (closes DS M2), and the bearer-over-WebSocket fix.

**Architecture:** New `internal/conversations` module (repo + service + http) holding `conversation_meta` + `conversation_user_members`; the DS device-roster is gated by conversation membership. The DS journal (`conversations`, `messages`) is untouched in shape; a journal row is created alongside metadata. No MLS on the backend.

**Tech Stack:** Go (pgx/v5, net/http ServeMux, testcontainers-go), Postgres.

**Conventions (verified — follow exactly):**
- Repos in `internal/store` (`pool *pgxpool.Pool`; `errors.As` on `*pgconn.PgError` code `"23505"`→conflict; `pgx.ErrNoRows`→`store.ErrNotFound`).
- Services per package, take a `Repo` interface (see `internal/workspace`).
- HTTP in `internal/httpapi`: helpers `decodeJSON`, `writeJSON`, `writeError`, `sessionFrom(ctx)` → `sessionWithToken{Session *session.Session, Token string}` (`Session.UserID`). Routes in `router.go` via `NewRouterFull(...)`, wrapped `auth := authMW(sess)`. Go 1.22 patterns, `r.PathValue`.
- Integration tests use `newTestPool(t)` in `internal/store/users_test.go` (package `store`).
- `store.WorkspaceRepo.RoleOf(ctx, wsID, userID) (string, error)` → `ErrNotFound` if not a member; `store.RoleOwner/RoleAdmin/RoleMember`.
- `store.UserRepo.FindByEmailOrUsername(ctx, q)`.
- DS journal: `conversations(group_id TEXT PK, next_seq BIGINT DEFAULT 0, created_at)`; device roster `conversation_members(group_id, device_id, join_seq)` via `store.RosterRepo`.
- **Docker required** for integration tests.

---

### Task 1: Migration — conversation_meta + conversation_user_members

**Files:** Create `backend/internal/platform/postgres/migrations/0005_conversations.sql`; Test `backend/internal/platform/postgres/migrate_conversations_test.go`

- [ ] **Step 1: Failing test** — `migrate_conversations_test.go` (mirror `migrate_workspaces_test.go`: spin testcontainers, `Connect`+`Migrate`, then assert). Assertions:
  - tables `conversation_meta`, `conversation_user_members` exist;
  - insert a user + workspace, then a `conversation_meta` row with `type='channel', visibility='public'` succeeds;
  - a row with `type='bogus'` fails (CHECK);
  - the partial unique index blocks a second `dm_key` duplicate within the same workspace: insert two `conversation_meta` rows with the same `(workspace_id, dm_key='u1|u2')` → second errors.

```go
package postgres

import (
	"context"
	"testing"

	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestMigrateConversations(t *testing.T) {
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("as"), tcpostgres.WithUsername("as"), tcpostgres.WithPassword("as"),
		tcpostgres.BasicWaitStrategies())
	if err != nil {
		t.Fatalf("start: %v", err)
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
	for _, tbl := range []string{"conversation_meta", "conversation_user_members"} {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_name=$1`, tbl).Scan(&n); err != nil || n != 1 {
			t.Fatalf("table %s missing (n=%d err=%v)", tbl, n, err)
		}
	}
	var uid, wid string
	pool.QueryRow(ctx, `INSERT INTO users (email, username, opaque_record) VALUES ('a@c','alice','x') RETURNING id`).Scan(&uid)
	pool.QueryRow(ctx, `INSERT INTO workspaces (name, slug, owner_user_id) VALUES ('W','w',$1) RETURNING id`, uid).Scan(&wid)
	if _, err := pool.Exec(ctx, `INSERT INTO conversations (group_id) VALUES ('g1')`); err != nil {
		t.Fatalf("journal row: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO conversation_meta (group_id, workspace_id, type, visibility, created_by) VALUES ('g1',$1,'channel','public',$2)`, wid, uid); err != nil {
		t.Fatalf("insert meta: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO conversation_meta (group_id, workspace_id, type, visibility, created_by) VALUES ('g2',$1,'bogus','public',$2)`, wid, uid); err == nil {
		t.Fatal("expected CHECK violation for bogus type")
	}
	// dm_key uniqueness within workspace
	pool.Exec(ctx, `INSERT INTO conversations (group_id) VALUES ('d1')`)
	pool.Exec(ctx, `INSERT INTO conversations (group_id) VALUES ('d2')`)
	if _, err := pool.Exec(ctx, `INSERT INTO conversation_meta (group_id, workspace_id, type, visibility, created_by, dm_key) VALUES ('d1',$1,'dm','private',$2,'u1|u2')`, wid, uid); err != nil {
		t.Fatalf("dm1: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO conversation_meta (group_id, workspace_id, type, visibility, created_by, dm_key) VALUES ('d2',$1,'dm','private',$2,'u1|u2')`, wid, uid); err == nil {
		t.Fatal("expected dm_key uniqueness violation")
	}
}
```

- [ ] **Step 2: Run → FAIL.** `cd backend && go test ./internal/platform/postgres/ -run TestMigrateConversations -v`
- [ ] **Step 3: Migration** `0005_conversations.sql`:
```sql
CREATE TABLE conversation_meta (
    group_id     TEXT PRIMARY KEY REFERENCES conversations(group_id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    type         TEXT NOT NULL CHECK (type IN ('dm','channel')),
    visibility   TEXT NOT NULL CHECK (visibility IN ('public','private')),
    name         TEXT NOT NULL DEFAULT '',
    created_by   UUID NOT NULL REFERENCES users(id),
    dm_key       TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX conversation_meta_ws_idx ON conversation_meta(workspace_id);
CREATE UNIQUE INDEX conversation_meta_dm_uniq ON conversation_meta(workspace_id, dm_key) WHERE dm_key IS NOT NULL;

CREATE TABLE conversation_user_members (
    group_id  TEXT NOT NULL REFERENCES conversation_meta(group_id) ON DELETE CASCADE,
    user_id   UUID NOT NULL REFERENCES users(id),
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, user_id)
);
CREATE INDEX conversation_user_members_user_idx ON conversation_user_members(user_id);
```
- [ ] **Step 4: Run → PASS.**
- [ ] **Step 5: Commit** `feat(backend): conversations migration (meta + user members)`

---

### Task 2: ConversationRepo (store)

**Files:** Create `backend/internal/store/conversations.go`; Test `backend/internal/store/conversations_test.go`

Types + repo:
```go
type Conversation struct {
	GroupID, WorkspaceID, Type, Visibility, Name, CreatedBy string
}
type ConvRepo struct{ pool *pgxpool.Pool }
func NewConvRepo(pool *pgxpool.Pool) *ConvRepo { return &ConvRepo{pool: pool} }
```
Methods (all create the DS journal row `conversations(group_id)` in the same tx as meta):
- `CreateChannel(ctx, groupID, wsID, visibility, name, creatorUserID string) (*Conversation, error)` — tx: insert `conversations`, `conversation_meta(type='channel')`, `conversation_user_members(creator)`.
- `GetOrCreateDM(ctx, groupID, wsID, creatorUserID, targetUserID string) (*Conversation, bool, error)` — compute `dm_key` = sorted `a|b`; try select existing by `(wsID, dm_key)`; if found return it + `false`; else tx insert journal + meta(`type='dm',visibility='private',dm_key`) + both memberships, return `true`. On `23505` (lost race) re-select and return existing.
- `Get(ctx, groupID) (*Conversation, error)` → `ErrNotFound`.
- `IsMember(ctx, groupID, userID) (bool, error)`.
- `AddUser(ctx, groupID, userID) error` (`23505`→`ErrConflict`).
- `RemoveUser(ctx, groupID, userID) error` (0 rows→`ErrNotFound`).
- `MemberUserIDs(ctx, groupID) ([]string, error)`.
- `ListForUser(ctx, wsID, userID) ([]Conversation, error)` — union of: conversations where user is a member, plus public channels in the workspace. Use:
  ```sql
  SELECT m.group_id, m.workspace_id, m.type, m.visibility, m.name, m.created_by
    FROM conversation_meta m
   WHERE m.workspace_id=$1
     AND ( m.visibility='public'
        OR EXISTS (SELECT 1 FROM conversation_user_members cm WHERE cm.group_id=m.group_id AND cm.user_id=$2) )
   ORDER BY m.created_at
  ```

The `groupID` is generated by the caller (service) as a UUID string. Provide helper `func NewGroupID() string` in the conversations service using `crypto/rand` (or reuse an existing id generator — check `internal/as` `randID`; if unexported, generate locally).

- [ ] **Step 1-2:** integration test `conversations_test.go` (use `newTestPool`): create user+workspace via existing repos; `CreateChannel` then `Get`/`IsMember(creator)=true`/`MemberUserIDs`; `GetOrCreateDM` twice with same pair → second returns `created=false` and same group_id; `AddUser`/`RemoveUser` (+ ErrConflict on dup, ErrNotFound on absent); `ListForUser` returns the public channel for a non-member and the DM for a member, and excludes a private channel the user isn't in. Run → FAIL.
- [ ] **Step 3:** implement `conversations.go`.
- [ ] **Step 4:** run → PASS; `go vet ./internal/store/`.
- [ ] **Step 5: Commit** `feat(backend): conversation repository (meta, dm idempotency, membership)`

---

### Task 3: conversations.Service (authz)

**Files:** Create `backend/internal/conversations/conversations.go` + `conversations_test.go`

```go
package conversations

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sort"
	"strings"

	"github.com/messenger/backend/internal/store"
)

var (
	ErrForbidden = errors.New("conversations: forbidden")
	ErrNotMember = errors.New("conversations: not a member")
	ErrInvalid   = errors.New("conversations: invalid input")
)

type Repo interface {
	CreateChannel(ctx context.Context, groupID, wsID, visibility, name, creator string) (*store.Conversation, error)
	GetOrCreateDM(ctx context.Context, groupID, wsID, creator, target string) (*store.Conversation, bool, error)
	Get(ctx context.Context, groupID string) (*store.Conversation, error)
	IsMember(ctx context.Context, groupID, userID string) (bool, error)
	AddUser(ctx context.Context, groupID, userID string) error
	RemoveUser(ctx context.Context, groupID, userID string) error
	MemberUserIDs(ctx context.Context, groupID string) ([]string, error)
	ListForUser(ctx context.Context, wsID, userID string) ([]store.Conversation, error)
}
type WorkspaceRoles interface {
	RoleOf(ctx context.Context, workspaceID, userID string) (string, error) // store.ErrNotFound if not a member
}
type Users interface {
	FindByEmailOrUsername(ctx context.Context, q string) (*store.User, error)
}

type Service struct {
	repo  Repo
	wsr   WorkspaceRoles
	users Users
}
func NewService(repo Repo, wsr WorkspaceRoles, users Users) *Service { return &Service{repo, wsr, users} }

func newGroupID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *Service) requireWSMember(ctx context.Context, wsID, userID string) error {
	_, err := s.wsr.RoleOf(ctx, wsID, userID)
	if errors.Is(err, store.ErrNotFound) {
		return ErrNotMember
	}
	return err
}
```
Methods + rules:
- `CreateChannel(ctx, callerID, wsID, visibility, name)` — `requireWSMember`; validate `visibility in {public,private}` and `name != ""` else `ErrInvalid`; `repo.CreateChannel(newGroupID(), ...)`.
- `CreateDM(ctx, callerID, wsID, emailOrUsername)` — `requireWSMember(caller)`; resolve target via `users.FindByEmailOrUsername` (→ `store.ErrNotFound`); target must be WS member (`requireWSMember(target)` else `ErrInvalid`); target != caller (else `ErrInvalid`); `repo.GetOrCreateDM(newGroupID(), wsID, caller, target)`.
- `List(ctx, callerID, wsID)` — `requireWSMember`; `repo.ListForUser`.
- `Get(ctx, callerID, groupID)` — load conv; caller must be member OR (channel public AND WS member); else `ErrNotMember`.
- `Join(ctx, callerID, groupID)` — load conv; must be channel+public; caller must be WS member of conv's workspace; `repo.AddUser` (idempotent: treat `ErrConflict` as success).
- `AddUser(ctx, callerID, groupID, emailOrUsername)` — load conv; for private: caller must be a member; for public: caller must be WS member; resolve target (must be WS member of conv ws); `repo.AddUser` (→ ErrConflict 409).
- `RemoveUser(ctx, callerID, groupID, targetUserID)` — load conv; DM → `ErrForbidden` (can't leave a DM in v1); caller removing self always ok if member; removing another → only `created_by`; else `ErrForbidden`.
- `IsMember(ctx, groupID, userID)` — passthrough (used by roster authz, Task 6).

- [ ] **Steps 1-4:** unit test with fakes (mirror `workspace_test.go` fake pattern): fake `Repo`, fake `WorkspaceRoles` (map ws→user→role), fake `Users`. Cover: non-WS-member create → ErrNotMember; DM idempotency + self-DM → ErrInvalid + target-not-WS-member → ErrInvalid; Get on public channel by non-member WS user → ok, by non-member non-WS → ErrNotMember; Join public ok / private → ErrNotMember; AddUser private by non-member → ErrNotMember, by member ok; RemoveUser DM → ErrForbidden, self ok, other-by-non-creator → ErrForbidden. RED→GREEN, `go vet`.
- [ ] **Step 5: Commit** `feat(backend): conversations service (dm/channel authz, visibility)`

---

### Task 4: conversations HTTP handlers + routes

**Files:** Create `backend/internal/httpapi/conversation_handlers.go` + `conversation_handlers_test.go`; Modify `dto.go`, `router.go`.

DTOs:
```go
type createConvReq struct {
	Type           string `json:"type"`            // "dm" | "channel"
	Visibility     string `json:"visibility"`      // channel only
	Name           string `json:"name"`            // channel only
	EmailOrUsername string `json:"email_or_username"` // dm target
}
type convResp struct {
	GroupID    string `json:"group_id"`
	Type       string `json:"type"`
	Visibility string `json:"visibility"`
	Name       string `json:"name"`
}
type addConvUserReq struct{ EmailOrUsername string `json:"email_or_username"` }
```
Handler holds an interface `convService` (matching the Service methods used) so tests use a fake + injected session context (same technique as `workspace_handlers_test.go` with `sessionCtxKey`). Error mapping helper `writeConvErr`: `conversations.ErrNotMember`→404, `ErrForbidden`→403, `ErrInvalid`→400, `store.ErrConflict`→409, `store.ErrNotFound`→404, else 500.
Handlers: `create` (reads `wsId` path, dispatches dm vs channel), `list` (`wsId`), `get` (`group`), `join` (`group`), `addUser` (`group`), `removeUser` (`group`,`userId`).

Routes in `NewRouterFull` (add `convSvc *conversations.Service` param, build `ch := &conversationHandlers{svc: convSvc}`):
```go
mux.Handle("POST /workspaces/{wsId}/conversations", auth(http.HandlerFunc(ch.create)))
mux.Handle("GET /workspaces/{wsId}/conversations", auth(http.HandlerFunc(ch.list)))
mux.Handle("GET /conversations/{group}", auth(http.HandlerFunc(ch.get)))
mux.Handle("POST /conversations/{group}/join", auth(http.HandlerFunc(ch.join)))
mux.Handle("POST /conversations/{group}/users", auth(http.HandlerFunc(ch.addUser)))
mux.Handle("DELETE /conversations/{group}/users/{userId}", auth(http.HandlerFunc(ch.removeUser)))
```

- [ ] **Steps 1-4:** handler test (package `httpapi`, fake `convService`, `withSession` helper already exists from workspaces task — reuse it): assert create(channel)→200 maps fields; create routes dm vs channel by `type`; addUser error mapping table (nil→200, ErrNotMember→404, ErrForbidden→403, store.ErrConflict→409, store.ErrNotFound→404). RED→GREEN. (Note: `cmd/server`/`NewRouterFull` callers break until Task 8 — expected; ensure `go test ./internal/httpapi/` compiles in isolation and update in-package test callers of `NewRouterFull` with a `nil` conv arg.)
- [ ] **Step 5: Commit** `feat(backend): conversation HTTP handlers + routes`

---

### Task 5: Workspace member search

**Files:** Modify `backend/internal/store/workspaces.go` (add `SearchMembers`), `backend/internal/workspace/workspace.go` (add `SearchMembers` with WS-member authz), `backend/internal/httpapi/workspace_handlers.go` (handler) + `router.go` (route); tests in the respective `_test.go`.

- Repo: `func (r *WorkspaceRepo) SearchMembers(ctx, wsID, q string) ([]WorkspaceMember, error)` —
  ```sql
  SELECT u.id, u.username, u.email, m.role FROM workspace_members m JOIN users u ON u.id=m.user_id
  WHERE m.workspace_id=$1 AND (u.username ILIKE $2 OR u.email ILIKE $2) ORDER BY u.username LIMIT 20
  ```
  pass `q+"%"` as `$2` (prefix match). Empty `q` → return `[]` (no error).
- Service: `func (s *Service) SearchMembers(ctx, callerID, wsID, q string) ([]store.WorkspaceMember, error)` — `requireRole(caller)` first (non-member→ErrNotMember), then repo.
- Handler `GET /workspaces/{id}/members/search` reads `q` query param; reuse `wsMemberResp`. Map errors via existing `writeWorkspaceErr`.
- [ ] **Steps:** add to `workspaces_test.go` an integration case (owner + 2 members; search by username prefix and email prefix scoped to the WS; cross-WS user excluded). Add to `workspace_handlers_test.go` a fake-svc mapping check (already has `fakeWS`; add `SearchMembers` to it + the `wsService` interface). RED→GREEN. Commit `feat(backend): workspace member search`.

---

### Task 6: Device-roster authz (closes DS M2)

**Files:** Modify `backend/internal/httpapi/roster_handlers.go` + `router.go` (inject a membership checker); Test add to `backend/internal/httpapi/roster_handlers_test.go`.

- Define in httpapi: `type convMembership interface { IsMember(ctx context.Context, groupID, userID string) (bool, error) }` (satisfied by `*conversations.Service`).
- `rosterHandlers` gains `members convMembership`. In `addMember`/`removeMember`: read `swt := sessionFrom(r.Context())`; `ok, err := h.members.IsMember(ctx, groupID, swt.Session.UserID)`; if `!ok` → `writeError(404, "not_found", "conversation not found")`. Then proceed as before.
- `router.go`: build `rh := &rosterHandlers{roster: rosterRepo, members: convSvc}` (convSvc from Task 4 param).
- [ ] **Steps:** test (package httpapi) with a fake `convMembership` (returns false/true) + `withSession`: non-member caller → 404, member caller → reaches roster add (use a fake/real roster — to avoid DB, make the existing `roster *store.RosterRepo` an interface too, OR test only the 404 path for non-member and a 200 path with a stub). Simplest: introduce `type rosterStore interface { AddMember(ctx,...); RemoveMember(ctx,...) }` so the handler is unit-testable with fakes for both. RED→GREEN. Commit `fix(backend): gate device-roster mutations by conversation membership (DS M2)`.

---

### Task 7: Bearer-over-WebSocket fix

**Files:** Modify `backend/internal/ws/gateway.go`; Test `backend/internal/ws/gateway_token_test.go` (or extend existing ws test).

- In the gateway `Handle`, where it currently extracts the bearer token from `Authorization`, fall back to `r.URL.Query().Get("access_token")` when the header is absent/empty. Extract a small helper `func tokenFromRequest(r *http.Request) string` and unit-test it: header present → that token; header absent + `?access_token=X` → `X`; neither → "".
- [ ] **Steps:** Read `gateway.go` first to match its current extraction. RED→GREEN unit test on `tokenFromRequest`. Commit `fix(backend): accept session token via ?access_token= for browser WebSocket`.

---

### Task 8: Wire into server + full verification

**Files:** Modify `backend/cmd/server/main.go`.

- Build `convSvc := conversations.NewService(store.NewConvRepo(pool), store.NewWorkspaceRepo(pool), store.NewUserRepo(pool))`.
- Pass `convSvc` to `NewRouterFull(...)` (new last param, matching Tasks 4 & 6 signature).
- [ ] **Steps:** add import; gofmt; `cd backend && go build ./... && go vet ./... && go test ./...` → all green (Docker required). Commit `feat(backend): mount conversations service in server router`.

---

## Self-Review

**Spec coverage:** conversation_meta + user_members migration → T1; repo (dm idempotency, membership, ListForUser visibility) → T2; service authz (WS membership, dm/channel/public/private rules, RemoveUser/Join) → T3; HTTP create/list/get/join/add/remove → T4; member search → T5; device-roster authz closing DS M2 → T6; bearer-over-WS fix → T7; wiring + full verify → T8. ✓ Out of scope (MLS, KT-client, UI) untouched.

**Placeholders:** T2/T3 give method contracts + rule lists + the non-trivial SQL (ListForUser, dm_key) rather than every line — deliberate, because they mirror the already-merged `workspace` repo/service patterns the implementer has in-repo; each task's tests are the precise contract. T1/T7 fully specified. No TBDs.

**Type consistency:** `store.Conversation` shape consistent T2↔T3↔T4; `conversations.Service` methods ↔ `convService`/`convMembership` httpapi interfaces (T4/T6) ↔ `cmd/server` wiring (T8); `WorkspaceRoles`/`Users` interfaces satisfied by existing `*store.WorkspaceRepo`/`*store.UserRepo`. `NewRouterFull` gains exactly one new param (`convSvc`), updated in T4/T6 and wired in T8 — note the `cmd/server` build is red T4→T8 (flagged in T4/T8).

**Sequencing note:** T4 and T6 both modify `router.go`/`NewRouterFull`; T8 closes the build. Per-task green builds should treat T4→T8 as a group (in-package `httpapi` tests stay green throughout; only `cmd/server` is deferred).
