# Delivery Service (DS) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the WebSocket delivery fabric — authenticated connections, an append-only encrypted message log with per-group sequencing, per-device sync cursors, and cross-node live fan-out via Redis pub/sub — so messages reach every group member's devices, online or offline, across horizontally-scaled nodes.

**Architecture:** Extends the existing Go modular monolith `backend/`. Postgres holds the durable source of truth (message log + roster + cursors); Redis pub/sub does best-effort live delivery (a channel per device); the log + cursor sync fills any gap. Nodes are stateless (shared state in Postgres/Redis), so N replicas behind a load balancer scale horizontally. A node holds WebSocket connections in an in-process hub, subscribes Redis channels for its locally-connected devices, and delivers published frames to those connections.

**Tech Stack:** Go 1.26, `github.com/coder/websocket` (+ `wsjson`), `github.com/jackc/pgx/v5`, `github.com/redis/go-redis/v9`, `github.com/testcontainers/testcontainers-go` (Postgres + Redis modules), `github.com/alicebob/miniredis/v2`. Reuses `internal/session` (auth), `internal/platform/{postgres,redis}`, `internal/store`, `internal/httpapi` from plan 1.

**Spec:** `docs/superpowers/specs/2026-06-17-delivery-service-design.md`. Rich presence / typing / read-receipts, mobile push / RabbitMQ, KT log, and the OPAQUE client are out of scope (separate plans).

**Environment prerequisite:** Integration tests (Tasks 1–6, 9–12) use testcontainers and REQUIRE a running Docker daemon. Pub/sub tests use a REAL Redis testcontainer (`testcontainers-go/modules/redis`) — miniredis pub/sub semantics are not relied on for fan-out. Unit tests (Tasks 7, 8) need no Docker. If Docker is unavailable, report it — do not fake/skip integration tests.

**Toolchain note (learned in plan 1):** testcontainers-go v0.42 needs `postgres.BasicWaitStrategies()` (plain `ForListeningPort` races the PG initdb restart). Run `go` from inside `backend/`; commit from the repo root.

---

## File Structure

```
backend/
  internal/platform/postgres/migrations/0002_delivery.sql   # conversations, messages, conversation_members, device_cursors
  internal/store/messages.go         # MessageRepo: Append (per-group seq, idempotent), ListSince
  internal/store/roster.go           # RosterRepo: AddMember, RemoveMember, Members, IsMember
  internal/store/cursors.go          # CursorRepo: Advance, Get
  internal/delivery/delivery.go      # Service: Send, Sync, Ack (orchestrates store + roster + Publisher)
  internal/fanout/fanout.go          # Redis pub/sub: Publish/Subscribe/Unsubscribe + receive loop
  internal/hub/hub.go                # in-process connection registry (deviceID -> bounded send channel)
  internal/ws/frames.go              # frame envelope types + decode
  internal/ws/gateway.go             # /ws handler: auth, register, read/write pumps
  internal/httpapi/roster_handlers.go# POST/DELETE conversation members
  cmd/server/main.go                 # mount /ws + roster routes, wire fanout/hub/delivery, graceful shutdown
```

Each file owns one responsibility; `delivery` depends on a small `Publisher` interface so it tests without Redis; repos sit behind interfaces already established in plan 1.

---

## Milestone 1 — Data layer

### Task 1: Migration for the delivery schema

**Files:**
- Create: `backend/internal/platform/postgres/migrations/0002_delivery.sql`
- Create: `backend/internal/platform/postgres/migrate_delivery_test.go`

- [ ] **Step 1: Write the failing test (integration — Docker)**

`backend/internal/platform/postgres/migrate_delivery_test.go`:
```go
package postgres

import (
	"context"
	"testing"

	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestMigrateCreatesDeliveryTables(t *testing.T) {
	ctx := context.Background()
	ctr, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("as"), postgres.WithUsername("as"), postgres.WithPassword("as"),
		postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })
	dsn, _ := ctr.ConnectionString(ctx, "sslmode=disable")
	pool, err := Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, table := range []string{"conversations", "messages", "conversation_members", "device_cursors"} {
		var exists bool
		pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name=$1)`, table).Scan(&exists)
		if !exists {
			t.Fatalf("expected table %q to exist", table)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/platform/postgres/ -run TestMigrateCreatesDeliveryTables`
Expected: FAIL — tables don't exist (migration 0002 absent).

- [ ] **Step 3: Write minimal implementation**

`backend/internal/platform/postgres/migrations/0002_delivery.sql`:
```sql
CREATE TABLE conversations (
    group_id   TEXT PRIMARY KEY,
    next_seq   BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE messages (
    group_id      TEXT NOT NULL,
    seq           BIGINT NOT NULL,
    sender_device UUID NOT NULL,
    client_msg_id TEXT NOT NULL,
    content_type  TEXT NOT NULL,
    ciphertext    BYTEA NOT NULL,
    server_ts     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, seq)
);
-- Idempotency: a given sender's client_msg_id maps to exactly one row per group.
CREATE UNIQUE INDEX idx_messages_idem ON messages (group_id, sender_device, client_msg_id);

CREATE TABLE conversation_members (
    group_id   TEXT NOT NULL,
    device_id  UUID NOT NULL,
    join_seq   BIGINT NOT NULL,
    added_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (group_id, device_id)
);

CREATE TABLE device_cursors (
    device_id  UUID NOT NULL,
    group_id   TEXT NOT NULL,
    acked_seq  BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (device_id, group_id)
);
```
The embedded migration runner from plan 1 (`migrate.go`, `//go:embed migrations/*.sql`) picks up `0002_delivery.sql` automatically by filename order.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/platform/postgres/ -run TestMigrateCreatesDeliveryTables`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/internal/platform/postgres
git -C /Users/denisurevic/Documents/slack commit -m "feat(backend): delivery schema migration (conversations, messages, members, cursors)"
```

---

### Task 2: Message repository — append with per-group seq, idempotent; ListSince

**Files:**
- Create: `backend/internal/store/messages.go`, `backend/internal/store/messages_test.go`

- [ ] **Step 1: Write the failing test (integration — Docker; reuses `newTestPool` from plan 1's `users_test.go`, same package)**

`backend/internal/store/messages_test.go`:
```go
package store

import (
	"context"
	"testing"
)

func TestMessageAppendAssignsMonotonicSeqAndIsIdempotent(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	repo := NewMessageRepo(pool)
	dev := "11111111-1111-1111-1111-111111111111"

	s1, dup1, err := repo.Append(ctx, "g1", dev, "cm-1", "application", []byte("ct-1"))
	if err != nil || dup1 {
		t.Fatalf("append 1: seq=%d dup=%v err=%v", s1, dup1, err)
	}
	s2, _, _ := repo.Append(ctx, "g1", dev, "cm-2", "application", []byte("ct-2"))
	if s1 != 1 || s2 != 2 {
		t.Fatalf("expected monotonic seq 1,2 got %d,%d", s1, s2)
	}

	// Idempotent replay of cm-1 returns the same seq, no new row.
	s1again, dup, err := repo.Append(ctx, "g1", dev, "cm-1", "application", []byte("ct-1"))
	if err != nil || !dup || s1again != s1 {
		t.Fatalf("idempotent replay: seq=%d dup=%v err=%v", s1again, dup, err)
	}

	// A different group has its own seq space.
	sg2, _, _ := repo.Append(ctx, "g2", dev, "cm-1", "application", []byte("x"))
	if sg2 != 1 {
		t.Fatalf("group g2 should start at seq 1, got %d", sg2)
	}
}

func TestListSinceRespectsJoinSeq(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	repo := NewMessageRepo(pool)
	dev := "11111111-1111-1111-1111-111111111111"
	for i := 0; i < 5; i++ {
		repo.Append(ctx, "g1", dev, "cm-"+string(rune('a'+i)), "application", []byte("ct"))
	}
	// since=2, joinSeq=0 → seqs 3,4,5
	msgs, err := repo.ListSince(ctx, "g1", 2, 0, 100)
	if err != nil {
		t.Fatalf("listsince: %v", err)
	}
	if len(msgs) != 3 || msgs[0].Seq != 3 || msgs[2].Seq != 5 {
		t.Fatalf("expected seqs 3..5, got %+v", msgs)
	}
	// joinSeq=4 clamps the floor: since=0, joinSeq=4 → seqs 4,5
	msgs2, _ := repo.ListSince(ctx, "g1", 0, 4, 100)
	if len(msgs2) != 2 || msgs2[0].Seq != 4 {
		t.Fatalf("join_seq floor not applied, got %+v", msgs2)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/store/ -run 'TestMessageAppend|TestListSince'`
Expected: FAIL — `NewMessageRepo` undefined.

- [ ] **Step 3: Write minimal implementation**

`backend/internal/store/messages.go`:
```go
package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Message is one stored (encrypted) message in a conversation log.
type Message struct {
	GroupID      string
	Seq          int64
	SenderDevice string
	ContentType  string
	Ciphertext   []byte
	ServerTS     time.Time
}

type MessageRepo struct{ pool *pgxpool.Pool }

func NewMessageRepo(pool *pgxpool.Pool) *MessageRepo { return &MessageRepo{pool: pool} }

// Append stores a message, assigning the next per-group seq in one transaction.
// Idempotent on (groupID, senderDevice, clientMsgID): a replay returns the existing
// seq with dup=true and writes nothing.
func (r *MessageRepo) Append(ctx context.Context, groupID, senderDevice, clientMsgID, contentType string, ciphertext []byte) (seq int64, dup bool, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback(ctx)

	// Idempotency check first.
	err = tx.QueryRow(ctx,
		`SELECT seq FROM messages WHERE group_id=$1 AND sender_device=$2 AND client_msg_id=$3`,
		groupID, senderDevice, clientMsgID).Scan(&seq)
	if err == nil {
		return seq, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, false, err
	}

	// Bump the per-group counter (creating the conversation row if needed).
	if err := tx.QueryRow(ctx,
		`INSERT INTO conversations (group_id, next_seq) VALUES ($1, 1)
		 ON CONFLICT (group_id) DO UPDATE SET next_seq = conversations.next_seq + 1
		 RETURNING next_seq`, groupID).Scan(&seq); err != nil {
		return 0, false, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO messages (group_id, seq, sender_device, client_msg_id, content_type, ciphertext)
		 VALUES ($1,$2,$3,$4,$5,$6)`,
		groupID, seq, senderDevice, clientMsgID, contentType, ciphertext); err != nil {
		return 0, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, false, err
	}
	return seq, false, nil
}

// ListSince returns messages with seq > max(sinceSeq, joinSeq-1), ordered by seq,
// up to limit. joinSeq enforces that a device never sees messages before it joined.
func (r *MessageRepo) ListSince(ctx context.Context, groupID string, sinceSeq, joinSeq int64, limit int) ([]Message, error) {
	floor := sinceSeq
	if joinSeq-1 > floor {
		floor = joinSeq - 1
	}
	rows, err := r.pool.Query(ctx,
		`SELECT group_id, seq, sender_device, content_type, ciphertext, server_ts
		 FROM messages WHERE group_id=$1 AND seq > $2 ORDER BY seq LIMIT $3`,
		groupID, floor, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.GroupID, &m.Seq, &m.SenderDevice, &m.ContentType, &m.Ciphertext, &m.ServerTS); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/store/ -run 'TestMessageAppend|TestListSince'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/internal/store/messages.go backend/internal/store/messages_test.go
git -C /Users/denisurevic/Documents/slack commit -m "feat(backend): message repo — per-group monotonic seq, idempotent append, ListSince"
```

---

### Task 3: Roster repository

**Files:**
- Create: `backend/internal/store/roster.go`, `backend/internal/store/roster_test.go`

- [ ] **Step 1: Write the failing test (integration — Docker)**

`backend/internal/store/roster_test.go`:
```go
package store

import (
	"context"
	"testing"
)

func TestRosterAddListRemove(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	repo := NewRosterRepo(pool)
	devA := "aaaaaaaa-1111-1111-1111-111111111111"
	devB := "bbbbbbbb-1111-1111-1111-111111111111"

	if err := repo.AddMember(ctx, "g1", devA, 0); err != nil {
		t.Fatalf("add A: %v", err)
	}
	if err := repo.AddMember(ctx, "g1", devB, 5); err != nil {
		t.Fatalf("add B: %v", err)
	}
	// Re-adding is idempotent (updates join_seq).
	if err := repo.AddMember(ctx, "g1", devA, 0); err != nil {
		t.Fatalf("re-add A: %v", err)
	}

	members, err := repo.Members(ctx, "g1")
	if err != nil || len(members) != 2 {
		t.Fatalf("members: %d %v", len(members), err)
	}
	if ok, _ := repo.IsMember(ctx, "g1", devA); !ok {
		t.Fatal("A should be a member")
	}

	if err := repo.RemoveMember(ctx, "g1", devB); err != nil {
		t.Fatalf("remove B: %v", err)
	}
	if ok, _ := repo.IsMember(ctx, "g1", devB); ok {
		t.Fatal("B should no longer be a member")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/store/ -run TestRoster`
Expected: FAIL — `NewRosterRepo` undefined.

- [ ] **Step 3: Write minimal implementation**

`backend/internal/store/roster.go`:
```go
package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Member is a device in a conversation, with the seq at which it joined.
type Member struct {
	DeviceID string
	JoinSeq  int64
}

type RosterRepo struct{ pool *pgxpool.Pool }

func NewRosterRepo(pool *pgxpool.Pool) *RosterRepo { return &RosterRepo{pool: pool} }

// AddMember adds (or re-adds, updating join_seq) a device to a conversation.
func (r *RosterRepo) AddMember(ctx context.Context, groupID, deviceID string, joinSeq int64) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO conversation_members (group_id, device_id, join_seq) VALUES ($1,$2,$3)
		 ON CONFLICT (group_id, device_id) DO UPDATE SET join_seq = EXCLUDED.join_seq`,
		groupID, deviceID, joinSeq)
	return err
}

func (r *RosterRepo) RemoveMember(ctx context.Context, groupID, deviceID string) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM conversation_members WHERE group_id=$1 AND device_id=$2`, groupID, deviceID)
	return err
}

func (r *RosterRepo) Members(ctx context.Context, groupID string) ([]Member, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT device_id, join_seq FROM conversation_members WHERE group_id=$1`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.DeviceID, &m.JoinSeq); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *RosterRepo) IsMember(ctx context.Context, groupID, deviceID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT FROM conversation_members WHERE group_id=$1 AND device_id=$2)`,
		groupID, deviceID).Scan(&exists)
	return exists, err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/store/ -run TestRoster`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/internal/store/roster.go backend/internal/store/roster_test.go
git -C /Users/denisurevic/Documents/slack commit -m "feat(backend): roster repo — conversation membership with join_seq"
```

---

### Task 4: Cursor repository

**Files:**
- Create: `backend/internal/store/cursors.go`, `backend/internal/store/cursors_test.go`

- [ ] **Step 1: Write the failing test (integration — Docker)**

`backend/internal/store/cursors_test.go`:
```go
package store

import (
	"context"
	"testing"
)

func TestCursorAdvanceIsMonotonic(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	repo := NewCursorRepo(pool)
	dev := "cccccccc-1111-1111-1111-111111111111"

	if got, _ := repo.Get(ctx, dev, "g1"); got != 0 {
		t.Fatalf("fresh cursor should be 0, got %d", got)
	}
	if err := repo.Advance(ctx, dev, "g1", 5); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if got, _ := repo.Get(ctx, dev, "g1"); got != 5 {
		t.Fatalf("expected 5, got %d", got)
	}
	// Advancing to a lower value must not move the cursor backward.
	if err := repo.Advance(ctx, dev, "g1", 3); err != nil {
		t.Fatalf("advance lower: %v", err)
	}
	if got, _ := repo.Get(ctx, dev, "g1"); got != 5 {
		t.Fatalf("cursor must not regress; expected 5, got %d", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/store/ -run TestCursor`
Expected: FAIL — `NewCursorRepo` undefined.

- [ ] **Step 3: Write minimal implementation**

`backend/internal/store/cursors.go`:
```go
package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

type CursorRepo struct{ pool *pgxpool.Pool }

func NewCursorRepo(pool *pgxpool.Pool) *CursorRepo { return &CursorRepo{pool: pool} }

// Advance moves the device's cursor for a group to uptoSeq, never backward.
func (r *CursorRepo) Advance(ctx context.Context, deviceID, groupID string, uptoSeq int64) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO device_cursors (device_id, group_id, acked_seq) VALUES ($1,$2,$3)
		 ON CONFLICT (device_id, group_id)
		 DO UPDATE SET acked_seq = GREATEST(device_cursors.acked_seq, EXCLUDED.acked_seq)`,
		deviceID, groupID, uptoSeq)
	return err
}

// Get returns the device's acked_seq for a group (0 if none).
func (r *CursorRepo) Get(ctx context.Context, deviceID, groupID string) (int64, error) {
	var seq int64
	err := r.pool.QueryRow(ctx,
		`SELECT acked_seq FROM device_cursors WHERE device_id=$1 AND group_id=$2`,
		deviceID, groupID).Scan(&seq)
	if err != nil {
		// pgx.ErrNoRows → no cursor yet → 0.
		return 0, nil
	}
	return seq, nil
}
```
Note: the `Get` swallows all errors as 0. Acceptable for "no cursor = 0", but to avoid masking real DB errors, prefer checking `errors.Is(err, pgx.ErrNoRows)` explicitly and returning other errors. Implement it that way (import `errors` and `pgx`):
```go
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return seq, nil
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/store/ -run TestCursor`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/internal/store/cursors.go backend/internal/store/cursors_test.go
git -C /Users/denisurevic/Documents/slack commit -m "feat(backend): cursor repo — monotonic per-device delivery cursor"
```

---

## Milestone 2 — Delivery, fan-out, hub, frames

### Task 5: Delivery service (Send / Sync / Ack)

**Files:**
- Create: `backend/internal/delivery/delivery.go`, `backend/internal/delivery/delivery_test.go`

- [ ] **Step 1: Write the failing test (integration — Docker for repos; a fake publisher captures fan-out)**

`backend/internal/delivery/delivery_test.go`:
```go
package delivery

import (
	"context"
	"sync"
	"testing"

	"github.com/messenger/backend/internal/store"
)

type fakePublisher struct {
	mu   sync.Mutex
	sent map[string]int // deviceID -> count
}

func newFakePublisher() *fakePublisher { return &fakePublisher{sent: map[string]int{}} }
func (f *fakePublisher) Publish(_ context.Context, deviceID string, _ []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent[deviceID]++
	return nil
}

func newService(t *testing.T) (*Service, *fakePublisher) {
	pool := store.NewTestPool(t) // exported test helper; see note
	pub := newFakePublisher()
	svc := NewService(store.NewMessageRepo(pool), store.NewRosterRepo(pool), store.NewCursorRepo(pool), pub)
	return svc, pub
}

func TestSendFansOutToOtherMembersOnly(t *testing.T) {
	ctx := context.Background()
	svc, pub := newService(t)
	roster := store.NewRosterRepo(storePoolOf(svc))
	alice := "aaaaaaaa-1111-1111-1111-111111111111"
	bob := "bbbbbbbb-1111-1111-1111-111111111111"
	roster.AddMember(ctx, "g1", alice, 0)
	roster.AddMember(ctx, "g1", bob, 0)

	seq, err := svc.Send(ctx, alice, "g1", "cm-1", "application", []byte("ct"))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if seq != 1 {
		t.Fatalf("expected seq 1, got %d", seq)
	}
	// Bob gets the fan-out; Alice (sender device) does not.
	if pub.sent[bob] != 1 || pub.sent[alice] != 0 {
		t.Fatalf("fan-out wrong: %+v", pub.sent)
	}
}

func TestSendByNonMemberIsForbidden(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t)
	_, err := svc.Send(ctx, "dddddddd-1111-1111-1111-111111111111", "g1", "cm-1", "application", []byte("ct"))
	if err != ErrNotMember {
		t.Fatalf("expected ErrNotMember, got %v", err)
	}
}

func TestSyncReturnsSinceCursorRespectingJoinSeq(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t)
	roster := store.NewRosterRepo(storePoolOf(svc))
	alice := "aaaaaaaa-1111-1111-1111-111111111111"
	late := "eeeeeeee-1111-1111-1111-111111111111"
	roster.AddMember(ctx, "g1", alice, 0)
	for i := 0; i < 3; i++ {
		svc.Send(ctx, alice, "g1", "cm-"+string(rune('a'+i)), "application", []byte("ct"))
	}
	// Late joins at seq 3 (after the 3 messages).
	roster.AddMember(ctx, "g1", late, 3)
	msgs, err := svc.Sync(ctx, late, "g1", 0)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("late joiner must not see pre-join messages, got %d", len(msgs))
	}
}

func TestAckAdvancesCursor(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t)
	dev := "aaaaaaaa-1111-1111-1111-111111111111"
	if err := svc.Ack(ctx, dev, "g1", 7); err != nil {
		t.Fatalf("ack: %v", err)
	}
	cur := store.NewCursorRepo(storePoolOf(svc))
	if got, _ := cur.Get(ctx, dev, "g1"); got != 7 {
		t.Fatalf("expected cursor 7, got %d", got)
	}
}
```

Two test helpers are referenced: `store.NewTestPool(t)` and `storePoolOf(svc)`. Implement them simply:
- In `backend/internal/store/`, add `testpool.go` (NOT `_test.go`, so it's importable by other packages' tests) is the wrong call — instead, refactor the existing `newTestPool` in `users_test.go` into an EXPORTED helper in a small non-test file guarded for test use is overkill. Simplest: in this delivery test, construct the pool directly with the same testcontainers bootstrap (copy the ~12-line `BasicWaitStrategies` bootstrap into a local `newTestPool(t)` in `delivery_test.go`). Replace `store.NewTestPool(t)` with a local `newTestPool(t)` and drop `storePoolOf`; instead keep the `*pgxpool.Pool` in a local variable and build the repos in the test from it. Rewrite the test to hold `pool` locally:
  ```go
  func newService(t *testing.T) (*Service, *fakePublisher, *pgxpool.Pool) {
      pool := newTestPool(t)
      pub := newFakePublisher()
      svc := NewService(store.NewMessageRepo(pool), store.NewRosterRepo(pool), store.NewCursorRepo(pool), pub)
      return svc, pub, pool
  }
  ```
  and use `store.NewRosterRepo(pool)` / `store.NewCursorRepo(pool)` directly. Do this refactor when writing the test; the assertions above are correct as written otherwise.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/delivery/`
Expected: FAIL — `NewService` undefined.

- [ ] **Step 3: Write minimal implementation**

`backend/internal/delivery/delivery.go`:
```go
package delivery

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/messenger/backend/internal/store"
)

var ErrNotMember = errors.New("delivery: sender is not a member of the group")

// Publisher delivers a serialized frame to a device's live channel (Redis pub/sub
// in production; a fake in tests).
type Publisher interface {
	Publish(ctx context.Context, deviceID string, payload []byte) error
}

// OutgoingMessage is the wire shape pushed to recipients (matches the ws "message" frame).
type OutgoingMessage struct {
	Type         string `json:"type"`
	GroupID      string `json:"group_id"`
	Seq          int64  `json:"seq"`
	SenderDevice string `json:"sender_device"`
	ContentType  string `json:"content_type"`
	Ciphertext   []byte `json:"ciphertext"`
	ServerTS     int64  `json:"server_ts"`
}

type Service struct {
	messages *store.MessageRepo
	roster   *store.RosterRepo
	cursors  *store.CursorRepo
	pub      Publisher
}

func NewService(m *store.MessageRepo, r *store.RosterRepo, c *store.CursorRepo, pub Publisher) *Service {
	return &Service{messages: m, roster: r, cursors: c, pub: pub}
}

// Send appends a message and fans out a live notification to every member device
// except the sender's own device. Returns the assigned seq.
func (s *Service) Send(ctx context.Context, senderDevice, groupID, clientMsgID, contentType string, ciphertext []byte) (int64, error) {
	ok, err := s.roster.IsMember(ctx, groupID, senderDevice)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, ErrNotMember
	}

	seq, dup, err := s.messages.Append(ctx, groupID, senderDevice, clientMsgID, contentType, ciphertext)
	if err != nil {
		return 0, err
	}
	if dup {
		return seq, nil // idempotent: already appended & fanned out
	}

	members, err := s.roster.Members(ctx, groupID)
	if err != nil {
		return seq, err
	}
	frame := OutgoingMessage{
		Type: "message", GroupID: groupID, Seq: seq, SenderDevice: senderDevice,
		ContentType: contentType, Ciphertext: ciphertext,
	}
	payload, _ := json.Marshal(frame)
	for _, m := range members {
		if m.DeviceID == senderDevice {
			continue
		}
		if m.JoinSeq > seq {
			continue // joined after this message; will not receive it
		}
		_ = s.pub.Publish(ctx, m.DeviceID, payload) // best-effort; durable via log+sync
	}
	return seq, nil
}

// Sync returns messages a device missed, from max(sinceSeq, joinSeq) up to limit.
func (s *Service) Sync(ctx context.Context, deviceID, groupID string, sinceSeq int64) ([]store.Message, error) {
	members, err := s.roster.Members(ctx, groupID)
	if err != nil {
		return nil, err
	}
	var joinSeq int64 = -1
	for _, m := range members {
		if m.DeviceID == deviceID {
			joinSeq = m.JoinSeq
			break
		}
	}
	if joinSeq < 0 {
		return nil, ErrNotMember
	}
	return s.messages.ListSince(ctx, groupID, sinceSeq, joinSeq, 500)
}

// Ack advances the device's delivery cursor.
func (s *Service) Ack(ctx context.Context, deviceID, groupID string, uptoSeq int64) error {
	return s.cursors.Advance(ctx, deviceID, groupID, uptoSeq)
}
```
Note: `Ack` does not require membership (a device acking its own cursor is harmless); `Sync` requires membership and applies join_seq.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/delivery/`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/internal/delivery
git -C /Users/denisurevic/Documents/slack commit -m "feat(backend): delivery service — Send fan-out (join_seq aware), Sync, Ack"
```

---

### Task 6: Fan-out over Redis pub/sub

**Files:**
- Create: `backend/internal/fanout/fanout.go`, `backend/internal/fanout/fanout_test.go`

- [ ] **Step 1: Write the failing test (integration — REAL Redis testcontainer; pub/sub semantics matter)**

`backend/internal/fanout/fanout_test.go`:
```go
package fanout

import (
	"context"
	"testing"
	"time"

	"github.com/messenger/backend/internal/platform/redis"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

func startRedis(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Fatalf("redis container: %v", err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })
	uri, err := ctr.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("conn string: %v", err)
	}
	return uri // redis://host:port
}

func TestPublishReachesSubscribedDevice(t *testing.T) {
	ctx := context.Background()
	uri := startRedis(t)
	rdb, err := redis.NewClient(ctx, uri)
	if err != nil {
		t.Fatalf("redis client: %v", err)
	}

	got := make(chan []byte, 1)
	f := New(rdb, func(deviceID string, payload []byte) {
		if deviceID == "devX" {
			got <- payload
		}
	})
	defer f.Close()

	if err := f.Subscribe(ctx, "devX"); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	// Give the subscription a moment to register.
	time.Sleep(100 * time.Millisecond)

	if err := f.Publish(ctx, "devX", []byte("hello")); err != nil {
		t.Fatalf("publish: %v", err)
	}
	select {
	case p := <-got:
		if string(p) != "hello" {
			t.Fatalf("got %q", p)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for published payload")
	}

	// After unsubscribe, a publish must NOT be delivered.
	f.Unsubscribe(ctx, "devX")
	time.Sleep(100 * time.Millisecond)
	f.Publish(ctx, "devX", []byte("after"))
	select {
	case <-got:
		t.Fatal("should not receive after unsubscribe")
	case <-time.After(500 * time.Millisecond):
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/fanout/`
Expected: FAIL — `New`/`Subscribe` undefined.

- [ ] **Step 3: Write minimal implementation**

`backend/internal/fanout/fanout.go`:
```go
package fanout

import (
	"context"
	"strings"
	"sync"

	goredis "github.com/redis/go-redis/v9"
)

const channelPrefix = "dev:"

// Fanout bridges Redis pub/sub and the local node: it Publishes frames to a device's
// channel, Subscribes the node to channels for its locally-connected devices, and
// invokes deliver(deviceID, payload) for every message received on a subscribed channel.
type Fanout struct {
	rdb     *goredis.Client
	ps      *goredis.PubSub
	deliver func(deviceID string, payload []byte)

	mu     sync.Mutex
	closed bool
	done   chan struct{}
}

// New creates a Fanout and starts its receive loop. deliver is called from a single
// background goroutine.
func New(rdb *goredis.Client, deliver func(deviceID string, payload []byte)) *Fanout {
	f := &Fanout{
		rdb:     rdb,
		ps:      rdb.Subscribe(context.Background()), // no channels yet
		deliver: deliver,
		done:    make(chan struct{}),
	}
	go f.loop()
	return f
}

func (f *Fanout) loop() {
	ch := f.ps.Channel()
	for msg := range ch {
		deviceID := strings.TrimPrefix(msg.Channel, channelPrefix)
		f.deliver(deviceID, []byte(msg.Payload))
	}
	close(f.done)
}

func (f *Fanout) Subscribe(ctx context.Context, deviceID string) error {
	return f.ps.Subscribe(ctx, channelPrefix+deviceID)
}

func (f *Fanout) Unsubscribe(ctx context.Context, deviceID string) error {
	return f.ps.Unsubscribe(ctx, channelPrefix+deviceID)
}

// Publish sends payload to a device's channel (delivered on whatever node is subscribed).
func (f *Fanout) Publish(ctx context.Context, deviceID string, payload []byte) error {
	return f.rdb.Publish(ctx, channelPrefix+deviceID, payload).Err()
}

func (f *Fanout) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	return f.ps.Close()
}
```
Note: `Fanout` implements the `delivery.Publisher` interface (`Publish(ctx, deviceID, payload) error`).

**API-version risk:** `tcredis.Run` and `ConnectionString` are from `testcontainers-go/modules/redis`. If `go get github.com/testcontainers/testcontainers-go/modules/redis` pulls an API where these differ, run `go doc github.com/testcontainers/testcontainers-go/modules/redis` and adjust. `redis.NewClient` (plan 1) calls `goredis.ParseURL`, which accepts the `redis://` URI the module returns.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/fanout/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/internal/fanout backend/go.mod backend/go.sum
git -C /Users/denisurevic/Documents/slack commit -m "feat(backend): Redis pub/sub fan-out (per-device channels)"
```

---

### Task 7: Connection hub

**Files:**
- Create: `backend/internal/hub/hub.go`, `backend/internal/hub/hub_test.go`

- [ ] **Step 1: Write the failing test (unit — no Docker)**

`backend/internal/hub/hub_test.go`:
```go
package hub

import (
	"testing"
	"time"
)

func TestHubDeliversToRegisteredDevice(t *testing.T) {
	h := New(4)
	ch, remove := h.Add("devA")
	defer remove()

	if !h.Deliver("devA", []byte("hi")) {
		t.Fatal("deliver to present device should succeed")
	}
	select {
	case p := <-ch:
		if string(p) != "hi" {
			t.Fatalf("got %q", p)
		}
	case <-time.After(time.Second):
		t.Fatal("expected payload on channel")
	}
}

func TestDeliverToAbsentDeviceReturnsFalse(t *testing.T) {
	h := New(4)
	if h.Deliver("ghost", []byte("x")) {
		t.Fatal("deliver to absent device should return false")
	}
}

func TestRemoveStopsDelivery(t *testing.T) {
	h := New(4)
	_, remove := h.Add("devA")
	remove()
	if h.Deliver("devA", []byte("x")) {
		t.Fatal("deliver after remove should return false")
	}
}

func TestDeliverDropsWhenBufferFull(t *testing.T) {
	h := New(1)
	_, remove := h.Add("devA")
	defer remove()
	if !h.Deliver("devA", []byte("1")) {
		t.Fatal("first deliver should fit the buffer")
	}
	// Buffer (size 1) is now full; next deliver reports overflow (false).
	if h.Deliver("devA", []byte("2")) {
		t.Fatal("deliver into a full buffer should return false (backpressure)")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/hub/`
Expected: FAIL — `New` undefined.

- [ ] **Step 3: Write minimal implementation**

`backend/internal/hub/hub.go`:
```go
package hub

import "sync"

// Hub is a node-local registry of device connections. Each device has a bounded
// send channel drained by that connection's write pump.
type Hub struct {
	mu      sync.RWMutex
	conns   map[string]chan []byte
	bufSize int
}

func New(bufSize int) *Hub {
	return &Hub{conns: make(map[string]chan []byte), bufSize: bufSize}
}

// Add registers a device and returns its receive channel plus a remove func.
// A second Add for the same device replaces the first (newest connection wins).
func (h *Hub) Add(deviceID string) (<-chan []byte, func()) {
	ch := make(chan []byte, h.bufSize)
	h.mu.Lock()
	if old, ok := h.conns[deviceID]; ok {
		close(old)
	}
	h.conns[deviceID] = ch
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if cur, ok := h.conns[deviceID]; ok && cur == ch {
			delete(h.conns, deviceID)
			close(ch)
		}
		h.mu.Unlock()
	}
}

// Deliver pushes payload to a device's channel without blocking. Returns false if
// the device is absent or its buffer is full (backpressure → caller closes the conn).
func (h *Hub) Deliver(deviceID string, payload []byte) bool {
	h.mu.RLock()
	ch, ok := h.conns[deviceID]
	h.mu.RUnlock()
	if !ok {
		return false
	}
	select {
	case ch <- payload:
		return true
	default:
		return false
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/hub/`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/internal/hub
git -C /Users/denisurevic/Documents/slack commit -m "feat(backend): node-local connection hub with bounded delivery"
```

---

### Task 8: WebSocket frame types + decode

**Files:**
- Create: `backend/internal/ws/frames.go`, `backend/internal/ws/frames_test.go`

- [ ] **Step 1: Write the failing test (unit — no Docker)**

`backend/internal/ws/frames_test.go`:
```go
package ws

import "testing"

func TestDecodeFrameType(t *testing.T) {
	typ, err := decodeType([]byte(`{"type":"send","group_id":"g1"}`))
	if err != nil || typ != "send" {
		t.Fatalf("type=%q err=%v", typ, err)
	}
	if _, err := decodeType([]byte(`not json`)); err == nil {
		t.Fatal("expected error on bad JSON")
	}
}

func TestSendFrameRoundTrips(t *testing.T) {
	var f sendFrame
	if err := unmarshalFrame([]byte(`{"type":"send","client_msg_id":"c1","group_id":"g1","content_type":"application","ciphertext":"AQID"}`), &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if f.GroupID != "g1" || f.ClientMsgID != "c1" || len(f.Ciphertext) != 3 {
		t.Fatalf("bad decode: %+v", f)
	}
}
```
(Note: JSON `[]byte` fields are base64 strings; `"AQID"` decodes to bytes {1,2,3}.)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/ws/ -run 'TestDecode|TestSendFrame'`
Expected: FAIL — `decodeType` undefined.

- [ ] **Step 3: Write minimal implementation**

`backend/internal/ws/frames.go`:
```go
package ws

import "encoding/json"

// Client→server frames.
type sendFrame struct {
	Type        string `json:"type"`
	ClientMsgID string `json:"client_msg_id"`
	GroupID     string `json:"group_id"`
	ContentType string `json:"content_type"`
	Ciphertext  []byte `json:"ciphertext"`
}
type ackFrame struct {
	Type    string `json:"type"`
	GroupID string `json:"group_id"`
	UpToSeq int64  `json:"up_to_seq"`
}
type syncFrame struct {
	Type     string `json:"type"`
	GroupID  string `json:"group_id"`
	SinceSeq int64  `json:"since_seq"`
}

// Server→client frames.
type sentFrame struct {
	Type        string `json:"type"`
	ClientMsgID string `json:"client_msg_id"`
	GroupID     string `json:"group_id"`
	Seq         int64  `json:"seq"`
	ServerTS    int64  `json:"server_ts"`
}
type errorFrame struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func decodeType(b []byte) (string, error) {
	var env struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		return "", err
	}
	return env.Type, nil
}

func unmarshalFrame(b []byte, dst any) error { return json.Unmarshal(b, dst) }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/ws/ -run 'TestDecode|TestSendFrame'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/internal/ws/frames.go backend/internal/ws/frames_test.go
git -C /Users/denisurevic/Documents/slack commit -m "feat(backend): ws frame types + decode helpers"
```

---

## Milestone 3 — Gateway, roster API, integration, wiring

### Task 9: WebSocket gateway (auth, pumps, wiring)

**Files:**
- Create: `backend/internal/ws/gateway.go`, `backend/internal/ws/gateway_test.go`

- [ ] **Step 1: Write the failing test (integration — Docker; real WS via httptest + websocket.Dial)**

`backend/internal/ws/gateway_test.go`:
```go
package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/messenger/backend/internal/delivery"
	"github.com/messenger/backend/internal/fanout"
	"github.com/messenger/backend/internal/hub"
	"github.com/messenger/backend/internal/platform/postgres"
	"github.com/messenger/backend/internal/platform/redis"
	"github.com/messenger/backend/internal/session"
	"github.com/messenger/backend/internal/store"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

// testEnv wires a full DS node against real Postgres + Redis.
type testEnv struct {
	server  *httptest.Server
	sess    *session.Manager
	roster  *store.RosterRepo
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	ctx := context.Background()
	pgctr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("as"), tcpostgres.WithUsername("as"), tcpostgres.WithPassword("as"),
		tcpostgres.BasicWaitStrategies())
	if err != nil { t.Fatalf("pg: %v", err) }
	t.Cleanup(func() { _ = pgctr.Terminate(ctx) })
	dsn, _ := pgctr.ConnectionString(ctx, "sslmode=disable")
	pool, _ := postgres.Connect(ctx, dsn)
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool); err != nil { t.Fatalf("migrate: %v", err) }

	rctr, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil { t.Fatalf("redis: %v", err) }
	t.Cleanup(func() { _ = rctr.Terminate(ctx) })
	ruri, _ := rctr.ConnectionString(ctx)
	rdb, _ := redis.NewClient(ctx, ruri)

	sess := session.NewManager(rdb, time.Hour)
	roster := store.NewRosterRepo(pool)
	h := hub.New(64)
	f := fanout.New(rdb, func(deviceID string, payload []byte) { h.Deliver(deviceID, payload) })
	t.Cleanup(func() { _ = f.Close() })
	svc := delivery.NewService(store.NewMessageRepo(pool), roster, store.NewCursorRepo(pool), f)

	gw := NewGateway(sess, svc, h, f, "node-test")
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", gw.Handle)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &testEnv{server: srv, sess: sess, roster: roster}
}

func dial(t *testing.T, env *testEnv, token string) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(env.server.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return c
}

func TestSendThenOtherDeviceReceives(t *testing.T) {
	env := newEnv(t)
	ctx := context.Background()
	alice, _ := env.sess.Issue(ctx, "user-1", "aaaaaaaa-1111-1111-1111-111111111111")
	bob, _ := env.sess.Issue(ctx, "user-2", "bbbbbbbb-1111-1111-1111-111111111111")
	env.roster.AddMember(ctx, "g1", "aaaaaaaa-1111-1111-1111-111111111111", 0)
	env.roster.AddMember(ctx, "g1", "bbbbbbbb-1111-1111-1111-111111111111", 0)

	ca := dial(t, env, alice)
	defer ca.Close(websocket.StatusNormalClosure, "")
	cb := dial(t, env, bob)
	defer cb.Close(websocket.StatusNormalClosure, "")
	time.Sleep(150 * time.Millisecond) // let subscriptions register

	writeJSON(t, ca, map[string]any{
		"type": "send", "client_msg_id": "c1", "group_id": "g1",
		"content_type": "application", "ciphertext": []byte("hello-bob"),
	})

	// Alice gets a `sent` ack; Bob gets the `message`.
	if got := readUntilType(t, ca, "sent"); int64(got["seq"].(float64)) != 1 {
		t.Fatalf("expected sent seq 1, got %v", got["seq"])
	}
	msg := readUntilType(t, cb, "message")
	if msg["group_id"] != "g1" || int64(msg["seq"].(float64)) != 1 {
		t.Fatalf("bob got wrong message: %v", msg)
	}
}

func TestUnauthenticatedDialRejected(t *testing.T) {
	env := newEnv(t)
	url := "ws" + strings.TrimPrefix(env.server.URL, "http") + "/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _, err := websocket.Dial(ctx, url, nil) // no Authorization
	if err == nil {
		t.Fatal("expected dial to fail without a valid session token")
	}
}

// helpers
func writeJSON(t *testing.T, c *websocket.Conn, v any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	b, _ := json.Marshal(v)
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatalf("write: %v", err)
	}
}
func readUntilType(t *testing.T, c *websocket.Conn, typ string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, b, err := c.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		var m map[string]any
		json.Unmarshal(b, &m)
		if m["type"] == typ {
			return m
		}
	}
	t.Fatalf("did not receive frame of type %q", typ)
	return nil
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/ws/ -run 'TestSendThen|TestUnauth'`
Expected: FAIL — `NewGateway` undefined.

- [ ] **Step 3: Write minimal implementation**

`backend/internal/ws/gateway.go`:
```go
package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/messenger/backend/internal/delivery"
	"github.com/messenger/backend/internal/fanout"
	"github.com/messenger/backend/internal/hub"
	"github.com/messenger/backend/internal/session"
)

type Gateway struct {
	sess     *session.Manager
	delivery *delivery.Service
	hub      *hub.Hub
	fanout   *fanout.Fanout
	nodeID   string
}

func NewGateway(sess *session.Manager, d *delivery.Service, h *hub.Hub, f *fanout.Fanout, nodeID string) *Gateway {
	return &Gateway{sess: sess, delivery: d, hub: h, fanout: f, nodeID: nodeID}
}

// Handle authenticates, upgrades, and runs the read/write pumps for one connection.
func (g *Gateway) Handle(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	sess, err := g.sess.Validate(r.Context(), token)
	if err != nil || sess.DeviceID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	deviceID := sess.DeviceID

	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer c.CloseNow()

	// Register locally and subscribe this node to the device's channel.
	sendCh, remove := g.hub.Add(deviceID)
	defer remove()
	ctx := context.Background()
	if err := g.fanout.Subscribe(ctx, deviceID); err != nil {
		c.Close(websocket.StatusInternalError, "subscribe failed")
		return
	}
	defer g.fanout.Unsubscribe(ctx, deviceID)

	// Write pump: drains sendCh to the socket (single writer goroutine).
	writeCtx, cancelWrite := context.WithCancel(ctx)
	defer cancelWrite()
	go func() {
		for {
			select {
			case <-writeCtx.Done():
				return
			case payload, ok := <-sendCh:
				if !ok {
					return
				}
				wctx, cancel := context.WithTimeout(writeCtx, 10*time.Second)
				err := c.Write(wctx, websocket.MessageText, payload)
				cancel()
				if err != nil {
					return
				}
			}
		}
	}()

	// Read pump: handle inbound frames until the client disconnects.
	for {
		rctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		_, data, err := c.Read(rctx)
		cancel()
		if err != nil {
			return // normal close or error → cleanup via defers
		}
		g.handleFrame(ctx, deviceID, data)
	}
}

func (g *Gateway) handleFrame(ctx context.Context, deviceID string, data []byte) {
	typ, err := decodeType(data)
	if err != nil {
		g.push(deviceID, errorFrame{Type: "error", Code: "bad_frame", Message: "invalid frame"})
		return
	}
	switch typ {
	case "send":
		var f sendFrame
		if unmarshalFrame(data, &f) != nil {
			g.push(deviceID, errorFrame{Type: "error", Code: "bad_frame", Message: "invalid send"})
			return
		}
		seq, err := g.delivery.Send(ctx, deviceID, f.GroupID, f.ClientMsgID, f.ContentType, f.Ciphertext)
		if errors.Is(err, delivery.ErrNotMember) {
			g.push(deviceID, errorFrame{Type: "error", Code: "forbidden", Message: "not a group member"})
			return
		}
		if err != nil {
			g.push(deviceID, errorFrame{Type: "error", Code: "internal", Message: "send failed"})
			return
		}
		g.push(deviceID, sentFrame{Type: "sent", ClientMsgID: f.ClientMsgID, GroupID: f.GroupID, Seq: seq, ServerTS: time.Now().Unix()})
	case "ack":
		var f ackFrame
		if unmarshalFrame(data, &f) == nil {
			_ = g.delivery.Ack(ctx, deviceID, f.GroupID, f.UpToSeq)
		}
	case "sync":
		var f syncFrame
		if unmarshalFrame(data, &f) != nil {
			return
		}
		msgs, err := g.delivery.Sync(ctx, deviceID, f.GroupID, f.SinceSeq)
		if err != nil {
			g.push(deviceID, errorFrame{Type: "error", Code: "sync_failed", Message: "sync failed"})
			return
		}
		for _, m := range msgs {
			g.push(deviceID, delivery.OutgoingMessage{
				Type: "message", GroupID: m.GroupID, Seq: m.Seq, SenderDevice: m.SenderDevice,
				ContentType: m.ContentType, Ciphertext: m.Ciphertext, ServerTS: m.ServerTS.Unix(),
			})
		}
	default:
		g.push(deviceID, errorFrame{Type: "error", Code: "unknown_type", Message: "unknown frame type"})
	}
}

// push delivers a server→client frame through the local hub (the write pump sends it).
func (g *Gateway) push(deviceID string, v any) {
	payload, _ := json.Marshal(v)
	g.hub.Deliver(deviceID, payload)
}
```
Notes:
- All server→client writes go through `hub.Deliver` → the single write-pump goroutine, so the ws connection is never written concurrently (coder/websocket forbids concurrent writes).
- Same-node delivery works because `fanout.New`'s deliver callback routes to `hub.Deliver`; the sender's own `sent`/`sync` frames are pushed locally via `push`. Cross-node messages arrive via the Redis subscription.
- `AcceptOptions{InsecureSkipVerify: true}` disables origin checking — acceptable here because auth is by bearer token, not cookies (no CSRF surface). In production, set `OriginPatterns` to the app's domains instead; note this as a hardening follow-up.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/ws/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/internal/ws/gateway.go backend/internal/ws/gateway_test.go backend/go.mod backend/go.sum
git -C /Users/denisurevic/Documents/slack commit -m "feat(backend): websocket gateway — auth, read/write pumps, send/ack/sync"
```

---

### Task 10: Roster HTTP API

**Files:**
- Create: `backend/internal/httpapi/roster_handlers.go`, `backend/internal/httpapi/roster_handlers_test.go`
- Modify: `backend/internal/httpapi/router.go` (register roster routes in `NewRouterFull`), `backend/internal/httpapi/dto.go`

- [ ] **Step 1: Write the failing test (integration — Docker; reuses newFullServer pattern from plan 1)**

Add to `backend/internal/httpapi/roster_handlers_test.go`:
```go
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestRosterAddAndListMembers(t *testing.T) {
	h, env := newRosterServer(t) // helper builds a full server with a roster repo (see below)
	token := registerAndLogin(t, h, "grace@corp", "roster-pass")
	// enroll a device so the session is device-bound (needed to be a sensible committer)
	deviceID := enrollDevice(t, h, token)

	// add a member device to a conversation
	rec := postJSON(t, h, "/conversations/g1/members", map[string]any{
		"device_id": deviceID, "join_seq": 0,
	}, token)
	if rec.Code != http.StatusOK {
		t.Fatalf("add member: %d %s", rec.Code, rec.Body)
	}

	// the roster repo reflects it
	members, _ := env.roster.Members(context.Background(), "g1")
	if len(members) != 1 || members[0].DeviceID != deviceID {
		t.Fatalf("expected device in roster, got %+v", members)
	}
}
```
NOTE: this test needs helpers `newRosterServer(t)` (a `newFullServer`-style builder that also wires a roster repo and the roster routes, returning the handler + an env exposing `*store.RosterRepo`) and `enrollDevice(t,h,token)` (POST /devices, return device_id). Implement `newRosterServer` by extending the existing `newFullServer` in `device_flow_test.go` — or add a new builder in this file that constructs the full router including roster routes. Keep `registerAndLogin` (already in device_flow_test.go) reused. Implement `enrollDevice` as a small helper that POSTs `/devices` with a dummy `signing_public_key` and returns the `device_id`. Report the exact helper wiring you used.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/httpapi/ -run TestRosterAdd`
Expected: FAIL — roster route/handler not defined.

- [ ] **Step 3: Write minimal implementation**

`backend/internal/httpapi/roster_handlers.go`:
```go
package httpapi

import (
	"net/http"

	"github.com/messenger/backend/internal/store"
)

type rosterHandlers struct {
	roster *store.RosterRepo
}

func (h *rosterHandlers) addMember(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("group")
	var req addMemberReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := h.roster.AddMember(r.Context(), groupID, req.DeviceID, req.JoinSeq); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "add member failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (h *rosterHandlers) removeMember(w http.ResponseWriter, r *http.Request) {
	groupID := r.PathValue("group")
	deviceID := r.PathValue("device")
	if err := h.roster.RemoveMember(r.Context(), groupID, deviceID); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "remove member failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
```
Append to `backend/internal/httpapi/dto.go`:
```go
type addMemberReq struct {
	DeviceID string `json:"device_id"`
	JoinSeq  int64  `json:"join_seq"`
}
```
Register in `NewRouterFull` (add a `*store.RosterRepo` parameter, wire two routes under `auth`):
```go
rh := &rosterHandlers{roster: rosterRepo}
mux.Handle("POST /conversations/{group}/members", auth(http.HandlerFunc(rh.addMember)))
mux.Handle("DELETE /conversations/{group}/members/{device}", auth(http.HandlerFunc(rh.removeMember)))
```
Add `rosterRepo *store.RosterRepo` as a new last parameter to `NewRouterFull`, and update all callers (`main.go` and the test builders `newFullServer`/`newRosterServer`) to pass it. (Authorization policy — verifying the caller is allowed to mutate this conversation's roster — is deliberately out of scope per the spec; a basic authenticated check via `auth` is applied.)

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/httpapi/ -run TestRosterAdd`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/internal/httpapi
git -C /Users/denisurevic/Documents/slack commit -m "feat(backend): roster HTTP API (add/remove conversation members)"
```

---

### Task 11: Multi-node delivery test (proves horizontal scaling)

**Files:**
- Create: `backend/internal/ws/multinode_test.go`

- [ ] **Step 1: Write the failing test (integration — Docker; two gateways share Postgres + Redis)**

`backend/internal/ws/multinode_test.go`:
```go
package ws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/messenger/backend/internal/delivery"
	"github.com/messenger/backend/internal/fanout"
	"github.com/messenger/backend/internal/hub"
	"github.com/messenger/backend/internal/platform/postgres"
	"github.com/messenger/backend/internal/platform/redis"
	"github.com/messenger/backend/internal/session"
	"github.com/messenger/backend/internal/store"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

// buildNode creates one DS node (its own hub/fanout/gateway) over shared pool+rdb.
func buildNode(t *testing.T, sess *session.Manager, pool interface{ Close() }, rdbURI string, poolReal *postgresPool, nodeID string) *httptest.Server {
	// see note: pass the concrete *pgxpool.Pool and *redis.Client in; this signature
	// is illustrative — implement with the real types.
	return nil
}

func TestCrossNodeDelivery(t *testing.T) {
	ctx := context.Background()
	pgctr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("as"), tcpostgres.WithUsername("as"), tcpostgres.WithPassword("as"),
		tcpostgres.BasicWaitStrategies())
	if err != nil { t.Fatalf("pg: %v", err) }
	t.Cleanup(func() { _ = pgctr.Terminate(ctx) })
	dsn, _ := pgctr.ConnectionString(ctx, "sslmode=disable")
	pool, _ := postgres.Connect(ctx, dsn)
	t.Cleanup(pool.Close)
	postgres.Migrate(ctx, pool)

	rctr, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil { t.Fatalf("redis: %v", err) }
	t.Cleanup(func() { _ = rctr.Terminate(ctx) })
	ruri, _ := rctr.ConnectionString(ctx)
	rdb, _ := redis.NewClient(ctx, ruri)

	sess := session.NewManager(rdb, time.Hour)
	roster := store.NewRosterRepo(pool)

	// node1 and node2: each its OWN hub + fanout (separate Redis subscriptions),
	// sharing the same pool and Redis server.
	mkNode := func(nodeID string) *httptest.Server {
		h := hub.New(64)
		f := fanout.New(rdb, func(deviceID string, payload []byte) { h.Deliver(deviceID, payload) })
		t.Cleanup(func() { _ = f.Close() })
		svc := delivery.NewService(store.NewMessageRepo(pool), roster, store.NewCursorRepo(pool), f)
		gw := NewGateway(sess, svc, h, f, nodeID)
		mux := http.NewServeMux()
		mux.HandleFunc("/ws", gw.Handle)
		s := httptest.NewServer(mux)
		t.Cleanup(s.Close)
		return s
	}
	node1 := mkNode("n1")
	node2 := mkNode("n2")

	alice := "aaaaaaaa-1111-1111-1111-111111111111"
	bob := "bbbbbbbb-1111-1111-1111-111111111111"
	aliceTok, _ := sess.Issue(ctx, "u1", alice)
	bobTok, _ := sess.Issue(ctx, "u2", bob)
	roster.AddMember(ctx, "g1", alice, 0)
	roster.AddMember(ctx, "g1", bob, 0)

	dialNode := func(s *httptest.Server, token string) *websocket.Conn {
		url := "ws" + strings.TrimPrefix(s.URL, "http") + "/ws"
		dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		c, _, err := websocket.Dial(dctx, url, &websocket.DialOptions{
			HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}},
		})
		if err != nil { t.Fatalf("dial: %v", err) }
		return c
	}

	ca := dialNode(node1, aliceTok) // Alice on node1
	defer ca.Close(websocket.StatusNormalClosure, "")
	cb := dialNode(node2, bobTok)   // Bob on node2
	defer cb.Close(websocket.StatusNormalClosure, "")
	time.Sleep(200 * time.Millisecond)

	writeJSON(t, ca, map[string]any{
		"type": "send", "client_msg_id": "c1", "group_id": "g1",
		"content_type": "application", "ciphertext": []byte("cross-node"),
	})

	// Bob, on a DIFFERENT node, receives via Redis pub/sub.
	msg := readUntilType(t, cb, "message")
	if msg["group_id"] != "g1" {
		t.Fatalf("cross-node delivery failed: %v", msg)
	}
}
```
NOTE: delete the illustrative `buildNode`/`postgresPool` stub at the top — it is a leftover placeholder. Use only the inline `mkNode` closure (which uses the real `*pgxpool.Pool` and `*redis.Client`). The test reuses `writeJSON` and `readUntilType` from `gateway_test.go` (same package).

- [ ] **Step 2: Run test to verify it fails (then passes once Task 9 is in)**

Run: `cd backend && go test ./internal/ws/ -run TestCrossNode`
Expected: PASS (Task 9 already implemented the gateway). If it fails, the most likely cause is a pub/sub subscription race — increase the post-dial `time.Sleep`, or have the gateway confirm subscription before returning from `Subscribe`. Diagnose and report; do not weaken the cross-node assertion.

- [ ] **Step 3: (no new impl expected)** If the test reveals a real gap (e.g. fan-out not reaching another node), fix it in `fanout`/`gateway` and re-run. Document any fix.

- [ ] **Step 4: Run the full ws suite**

Run: `cd backend && go test ./internal/ws/`
Expected: PASS (gateway + cross-node).

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/internal/ws/multinode_test.go
git -C /Users/denisurevic/Documents/slack commit -m "test(backend): cross-node delivery via Redis pub/sub (horizontal scaling)"
```

---

### Task 12: Wire DS into the server + graceful shutdown

**Files:**
- Modify: `backend/cmd/server/main.go`, `backend/internal/httpapi/router.go`

- [ ] **Step 1: Write the failing test**

Add `backend/internal/httpapi/router_ds_test.go`:
```go
package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// The DS routes must be registered (roster API reachable; returns 401 without auth,
// proving the route exists and is auth-protected rather than 404).
func TestRosterRouteIsRegisteredAndProtected(t *testing.T) {
	h, _ := newRosterServer(t)
	req := httptest.NewRequest(http.MethodPost, "/conversations/g1/members", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Fatal("roster route must be registered (got 404)")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails (if route not yet wired) or passes (if Task 10 wired it)**

Run: `cd backend && go test ./internal/httpapi/ -run TestRosterRouteIsRegistered`
Expected: PASS if Task 10 registered the route in `NewRouterFull`. If it 404s, the route wasn't wired — fix `NewRouterFull`.

- [ ] **Step 3: Wire main.go**

Update `backend/cmd/server/main.go` to construct the DS components and mount `/ws`. Add after the existing `kpSvc` construction and before building the handler:
```go
	// Delivery service + websocket gateway.
	hubReg := hub.New(256)
	fan := fanout.New(rdb, func(deviceID string, payload []byte) { hubReg.Deliver(deviceID, payload) })
	defer fan.Close()
	rosterRepo := store.NewRosterRepo(pool)
	deliverySvc := delivery.NewService(store.NewMessageRepo(pool), rosterRepo, store.NewCursorRepo(pool), fan)

	nodeID := os.Getenv("NODE_ID")
	if nodeID == "" {
		nodeID = "node"
	}
	gw := ws.NewGateway(sess, deliverySvc, hubReg, fan, nodeID)
```
Change the handler construction to pass `rosterRepo` to `NewRouterFull` and mount `/ws`:
```go
	apiHandler := httpapi.NewRouterFull(svc, sess, devSvc, kpSvc, rl, rosterRepo)
	root := http.NewServeMux()
	root.Handle("/", apiHandler)
	root.HandleFunc("/ws", gw.Handle)
	handler := root
```
Add imports: `os`, `github.com/messenger/backend/internal/{delivery,fanout,hub,ws,store}` (store already imported).
Use `http.Server` with graceful shutdown instead of `http.ListenAndServe`:
```go
	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: handler}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()
	log.Printf("auth+delivery service listening on %s", cfg.HTTPAddr)
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
```
Add imports `os/signal`, `syscall`. (`context`, `log`, `net/http`, `time` already present.)

- [ ] **Step 4: Verify the whole backend builds and all tests pass**

Run:
```bash
cd backend && go build ./... && go vet ./... && go test ./...
```
Expected: all green (unit + integration; Docker running). Report the per-package summary.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend
git -C /Users/denisurevic/Documents/slack commit -m "feat(backend): mount websocket gateway + roster API, graceful shutdown"
```

---

## Self-Review

**1. Spec coverage:**
- WebSocket gateway + session auth, one conn per device → Task 9 ✓
- Append-only log + per-group monotonic seq → Tasks 1, 2 ✓
- Per-device sync cursors → Tasks 4, 5 ✓
- Cross-node live fan-out via Redis pub/sub (channel per device) → Tasks 6, 11 ✓
- Sync-with-cursor (catch-up) → Tasks 5, 9 ✓
- Roster table + HTTP API → Tasks 1, 3, 10 ✓
- At-least-once + ack + client dedup by (group_id, seq) → Tasks 4, 5, 9 (server side; client dedup is the client's responsibility, noted) ✓
- Welcome via join_seq (no pre-join messages) → Tasks 2 (ListSince floor), 5 (Sync), with tests ✓
- Send forbidden for non-members → Task 5 ✓
- Idempotent send on client_msg_id → Tasks 1 (unique index), 2 ✓
- Stateless nodes / horizontal scale → Task 11 (cross-node proof), Task 12 (wiring) ✓
- Frame protocol (send/ack/sync/sent/message/error) → Tasks 8, 9 ✓
- Graceful shutdown, backpressure (bounded hub buffer) → Tasks 7, 9, 12 ✓
- Out of scope: rich presence/typing/read-receipts, RabbitMQ/push → excluded; the `presence:device:*` key is mentioned in spec as diagnostic and is NOT implemented here (no task) — that's consistent with "presence indicator out of scope". The spec's heartbeat/presence-key line is therefore intentionally not built; the routing uses implicit pub/sub subscription. Gap is intentional.

**2. Placeholder scan:** Task 5 contains a flagged test-helper refactor (replace `store.NewTestPool`/`storePoolOf` with a local `newTestPool` + returned `pool`) — explicit instructions given, not a silent gap. Task 10 flags helper wiring (`newRosterServer`, `enrollDevice`) with concrete implementation guidance. Task 11 flags an illustrative `buildNode` stub to delete in favor of the inline `mkNode`. All production code steps contain complete implementations.

**3. Type consistency:** `MessageRepo.Append(groupID, senderDevice, clientMsgID, contentType, ciphertext) (seq, dup, err)` and `ListSince(groupID, sinceSeq, joinSeq, limit)` are used consistently in `delivery.Service`. `delivery.Service.Send/Sync/Ack` signatures match the ws gateway calls. `delivery.Publisher.Publish(ctx, deviceID, payload)` is implemented by `fanout.Fanout.Publish`. `hub.New(bufSize)`, `Add`→`(<-chan []byte, func())`, `Deliver(deviceID, payload) bool` are consistent across hub, gateway, and the fanout deliver callback. `OutgoingMessage` (delivery) is the shared `message` frame shape used by both `Send` fan-out and `Sync` replay. `NewRouterFull` gains a trailing `*store.RosterRepo` param in Task 10; Task 12 and the test builders pass it.

**4. API-version risk (call out, not placeholder):** `coder/websocket` `Accept`/`Dial`/`Read`/`Write`/`AcceptOptions{InsecureSkipVerify}`/`DialOptions{HTTPHeader}` are taken from the current API; verify with `go doc github.com/coder/websocket` if compilation differs. `testcontainers-go/modules/redis` `Run`/`ConnectionString` flagged in Task 6. go-redis `PubSub.Channel()`/`Subscribe`/`Unsubscribe` are stable in v9. The pub/sub subscription-registration race is called out in Tasks 9/11 with a concrete mitigation (await/sleep) rather than left implicit.
