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
