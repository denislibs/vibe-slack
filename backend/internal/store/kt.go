package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// KTLeaf is a stored transparency-log leaf.
type KTLeaf struct {
	LeafIndex int64
	Identity  string
	Version   int64
	DeviceSet []byte // full RFC6962-preimage canonical leaf bytes (identity||version||keys), NOT just keys
	LeafHash  []byte
}

// KTSTH is a stored signed tree head.
type KTSTH struct {
	TreeSize  int64
	RootHash  []byte
	Signature []byte
}

type KTRepo struct{ pool *pgxpool.Pool }

func NewKTRepo(pool *pgxpool.Pool) *KTRepo { return &KTRepo{pool: pool} }

// AppendLeaf inserts a leaf at the next monotonic leaf_index (= current count) and
// returns that index. Must be called under the relay advisory lock (single appender).
func (r *KTRepo) AppendLeaf(ctx context.Context, identity string, version int64, deviceSet, leafHash []byte) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var idx int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(leaf_index)+1, 0) FROM kt_leaves`).Scan(&idx); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO kt_leaves (leaf_index, identity, version, device_set, leaf_hash) VALUES ($1,$2,$3,$4,$5)`,
		idx, identity, version, deviceSet, leafHash); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return idx, nil
}

func (r *KTRepo) MaxVersion(ctx context.Context, identity string) (int64, error) {
	var v int64
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM kt_leaves WHERE identity=$1`, identity).Scan(&v)
	return v, err
}

// AppendLeafTx appends a leaf within the caller's transaction (used by the relay to
// batch leaf appends + outbox marking atomically). Returns the assigned leaf_index.
func (r *KTRepo) AppendLeafTx(ctx context.Context, tx pgx.Tx, identity string, version int64, deviceSet, leafHash []byte) (int64, error) {
	var idx int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(leaf_index)+1, 0) FROM kt_leaves`).Scan(&idx); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO kt_leaves (leaf_index, identity, version, device_set, leaf_hash) VALUES ($1,$2,$3,$4,$5)`,
		idx, identity, version, deviceSet, leafHash); err != nil {
		return 0, err
	}
	return idx, nil
}

// MaxVersionTx reads the identity's current max version within the caller's tx, so
// it observes leaves appended earlier in the same batch.
func (r *KTRepo) MaxVersionTx(ctx context.Context, tx pgx.Tx, identity string) (int64, error) {
	var v int64
	err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version), 0) FROM kt_leaves WHERE identity=$1`, identity).Scan(&v)
	return v, err
}

// LeafHashes returns the first `size` leaf hashes in index order.
func (r *KTRepo) LeafHashes(ctx context.Context, size int64) ([][]byte, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT leaf_hash FROM kt_leaves WHERE leaf_index < $1 ORDER BY leaf_index`, size)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][]byte
	for rows.Next() {
		var h []byte
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (r *KTRepo) LatestLeafForIdentity(ctx context.Context, identity string) (*KTLeaf, error) {
	var l KTLeaf
	err := r.pool.QueryRow(ctx,
		`SELECT leaf_index, identity, version, device_set, leaf_hash FROM kt_leaves
		 WHERE identity=$1 ORDER BY version DESC LIMIT 1`, identity).
		Scan(&l.LeafIndex, &l.Identity, &l.Version, &l.DeviceSet, &l.LeafHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// LatestLeafForIdentityAt returns the identity's highest-version leaf whose
// leaf_index < maxExclusive (i.e. covered by an STH of tree_size == maxExclusive).
// Returns ErrNotFound if the identity has no leaf within that bound yet.
func (r *KTRepo) LatestLeafForIdentityAt(ctx context.Context, identity string, maxExclusive int64) (*KTLeaf, error) {
	var l KTLeaf
	err := r.pool.QueryRow(ctx,
		`SELECT leaf_index, identity, version, device_set, leaf_hash FROM kt_leaves
		 WHERE identity=$1 AND leaf_index < $2 ORDER BY version DESC LIMIT 1`, identity, maxExclusive).
		Scan(&l.LeafIndex, &l.Identity, &l.Version, &l.DeviceSet, &l.LeafHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func (r *KTRepo) PutSTH(ctx context.Context, treeSize int64, rootHash, signature []byte) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO kt_sths (tree_size, root_hash, signature) VALUES ($1,$2,$3)
		 ON CONFLICT (tree_size) DO NOTHING`, treeSize, rootHash, signature)
	return err
}

func (r *KTRepo) LatestSTH(ctx context.Context) (*KTSTH, error) {
	var s KTSTH
	err := r.pool.QueryRow(ctx,
		`SELECT tree_size, root_hash, signature FROM kt_sths ORDER BY tree_size DESC LIMIT 1`).
		Scan(&s.TreeSize, &s.RootHash, &s.Signature)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}
