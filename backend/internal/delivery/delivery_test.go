package delivery

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/messenger/backend/internal/platform/postgres"
	"github.com/messenger/backend/internal/store"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("as"), tcpostgres.WithUsername("as"), tcpostgres.WithPassword("as"),
		tcpostgres.BasicWaitStrategies())
	if err != nil {
		t.Fatalf("postgres: %v", err)
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

type fakePublisher struct {
	mu   sync.Mutex
	sent map[string]int
}

func newFakePublisher() *fakePublisher { return &fakePublisher{sent: map[string]int{}} }
func (f *fakePublisher) Publish(_ context.Context, deviceID string, _ []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent[deviceID]++
	return nil
}

func newService(t *testing.T) (*Service, *fakePublisher, *pgxpool.Pool) {
	pool := newTestPool(t)
	pub := newFakePublisher()
	svc := NewService(store.NewMessageRepo(pool), store.NewRosterRepo(pool), store.NewCursorRepo(pool), pub)
	return svc, pub, pool
}

func TestSendFansOutToOtherMembersOnly(t *testing.T) {
	ctx := context.Background()
	svc, pub, pool := newService(t)
	roster := store.NewRosterRepo(pool)
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
	if pub.sent[bob] != 1 || pub.sent[alice] != 0 {
		t.Fatalf("fan-out wrong: %+v", pub.sent)
	}
}

func TestSendByNonMemberIsForbidden(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newService(t)
	_, err := svc.Send(ctx, "dddddddd-1111-1111-1111-111111111111", "g1", "cm-1", "application", []byte("ct"))
	if err != ErrNotMember {
		t.Fatalf("expected ErrNotMember, got %v", err)
	}
}

func TestSyncRespectsJoinSeq(t *testing.T) {
	ctx := context.Background()
	svc, _, pool := newService(t)
	roster := store.NewRosterRepo(pool)
	alice := "aaaaaaaa-1111-1111-1111-111111111111"
	late := "eeeeeeee-1111-1111-1111-111111111111"
	roster.AddMember(ctx, "g1", alice, 0)
	for i := 0; i < 3; i++ {
		svc.Send(ctx, alice, "g1", "cm-"+string(rune('a'+i)), "application", []byte("ct"))
	}
	roster.AddMember(ctx, "g1", late, 3) // joins after the 3 messages
	msgs, err := svc.Sync(ctx, late, "g1", 0)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if len(msgs) != 0 {
		t.Fatalf("late joiner must not see pre-join messages, got %d", len(msgs))
	}
	// Alice (joined at 0) syncs and sees all 3.
	amsgs, _ := svc.Sync(ctx, alice, "g1", 0)
	if len(amsgs) != 3 {
		t.Fatalf("alice should see 3 messages, got %d", len(amsgs))
	}
	_ = time.Second
}

func TestAckAdvancesCursor(t *testing.T) {
	ctx := context.Background()
	svc, _, pool := newService(t)
	dev := "aaaaaaaa-1111-1111-1111-111111111111"
	if err := svc.Ack(ctx, dev, "g1", 7); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if got, _ := store.NewCursorRepo(pool).Get(ctx, dev, "g1"); got != 7 {
		t.Fatalf("expected cursor 7, got %d", got)
	}
}
