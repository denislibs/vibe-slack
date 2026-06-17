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
	pool.Exec(ctx, `INSERT INTO conversations (group_id) VALUES ('d1')`)
	pool.Exec(ctx, `INSERT INTO conversations (group_id) VALUES ('d2')`)
	if _, err := pool.Exec(ctx, `INSERT INTO conversation_meta (group_id, workspace_id, type, visibility, created_by, dm_key) VALUES ('d1',$1,'dm','private',$2,'u1|u2')`, wid, uid); err != nil {
		t.Fatalf("dm1: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO conversation_meta (group_id, workspace_id, type, visibility, created_by, dm_key) VALUES ('d2',$1,'dm','private',$2,'u1|u2')`, wid, uid); err == nil {
		t.Fatal("expected dm_key uniqueness violation")
	}
}
