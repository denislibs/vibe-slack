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

	var col string
	if err := pool.QueryRow(ctx,
		`SELECT column_name FROM information_schema.columns WHERE table_name='users' AND column_name='username'`).
		Scan(&col); err != nil {
		t.Fatalf("users.username missing: %v", err)
	}

	for _, tbl := range []string{"workspaces", "workspace_members"} {
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM information_schema.tables WHERE table_name=$1`, tbl).Scan(&n); err != nil || n != 1 {
			t.Fatalf("table %s missing (n=%d err=%v)", tbl, n, err)
		}
	}

	var uid string
	pool.QueryRow(ctx, `INSERT INTO users (email, username, opaque_record) VALUES ('a@c','alice','x') RETURNING id`).Scan(&uid)
	var wid string
	pool.QueryRow(ctx, `INSERT INTO workspaces (name, slug, owner_user_id) VALUES ('W','w',$1) RETURNING id`, uid).Scan(&wid)
	if _, err := pool.Exec(ctx,
		`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1,$2,'wizard')`, wid, uid); err == nil {
		t.Fatal("expected CHECK violation for bogus role")
	}
}
