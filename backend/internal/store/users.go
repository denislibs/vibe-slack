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

func (r *UserRepo) Create(ctx context.Context, email string, opaqueRecord []byte) (*User, error) {
	var id string
	err := r.pool.QueryRow(ctx,
		`INSERT INTO users (email, opaque_record) VALUES ($1, $2) RETURNING id`,
		email, opaqueRecord).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			return nil, ErrConflict
		}
		return nil, err
	}
	return &User{ID: id, Email: email, OpaqueRecord: opaqueRecord}, nil
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, opaque_record FROM users WHERE email=$1`, email).
		Scan(&u.ID, &u.Email, &u.OpaqueRecord)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}
