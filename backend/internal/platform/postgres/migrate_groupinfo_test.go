package postgres

import (
	"context"
	"testing"

	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestMigrateGroupInfo(t *testing.T) {
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
	var col string
	if err := pool.QueryRow(ctx, `SELECT column_name FROM information_schema.columns WHERE table_name='conversation_meta' AND column_name='group_info'`).Scan(&col); err != nil {
		t.Fatalf("group_info column missing: %v", err)
	}
}
