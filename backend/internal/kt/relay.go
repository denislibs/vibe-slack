package kt

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/messenger/backend/internal/store"
)

// advisoryLockKey is a fixed key so only one node appends to the KT log at a time.
const advisoryLockKey int64 = 0x4B54_4C4F_4700_0001

type deviceReader interface {
	ActiveSigningKeys(ctx context.Context, userID string) ([][]byte, error)
}

// Relay drains kt_outbox into the Merkle log and issues STHs. Single-appender via
// a Postgres session advisory lock.
type Relay struct {
	pool    *pgxpool.Pool
	kt      *store.KTRepo
	devices deviceReader
	signer  *STHSigner
}

func NewRelay(pool *pgxpool.Pool, kt *store.KTRepo, devices deviceReader, signer *STHSigner) *Relay {
	return &Relay{pool: pool, kt: kt, devices: devices, signer: signer}
}

// Tick processes all currently-unrelayed outbox events, appends leaves, and issues
// a fresh STH if any leaf was added. No-op (no new STH) when nothing is pending.
func (r *Relay) Tick(ctx context.Context) error {
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	var got bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, advisoryLockKey).Scan(&got); err != nil {
		return err
	}
	if !got {
		return nil
	}
	defer conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, advisoryLockKey)

	rows, err := conn.Query(ctx,
		`SELECT id, user_id FROM kt_outbox WHERE relayed_at IS NULL ORDER BY id`)
	if err != nil {
		return err
	}
	type ev struct {
		id     int64
		userID string
	}
	var events []ev
	for rows.Next() {
		var e ev
		if err := rows.Scan(&e.id, &e.userID); err != nil {
			rows.Close()
			return err
		}
		events = append(events, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(events) == 0 {
		return nil
	}

	// Append all leaves AND mark their outbox rows relayed in ONE transaction so the
	// batch is all-or-nothing: if Tick errors mid-loop the tx rolls back and a retry
	// reprocesses cleanly (no duplicate leaves / inflated versions).
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, e := range events {
		keys, err := r.devices.ActiveSigningKeys(ctx, e.userID)
		if err != nil {
			return err
		}
		maxV, err := r.kt.MaxVersionTx(ctx, tx, e.userID)
		if err != nil {
			return err
		}
		version := maxV + 1
		canonical := CanonicalLeaf(e.userID, version, keys)
		if _, err := r.kt.AppendLeafTx(ctx, tx, e.userID, version, canonical, LeafHash(canonical)); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE kt_outbox SET relayed_at=now() WHERE id=$1`, e.id); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	// Issue a fresh STH over the new tree size (after the batch is durably committed).
	var size int64
	if err := conn.QueryRow(ctx, `SELECT COUNT(*) FROM kt_leaves`).Scan(&size); err != nil {
		return err
	}
	hashes, err := r.kt.LeafHashes(ctx, size)
	if err != nil {
		return err
	}
	root := Root(hashes)
	return r.kt.PutSTH(ctx, size, root, r.signer.Sign(size, root))
}
