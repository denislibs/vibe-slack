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

func (r *UserRepo) Create(ctx context.Context, email, username string, opaqueRecord []byte) (*User, error) {
	var id string
	err := r.pool.QueryRow(ctx,
		`INSERT INTO users (email, username, opaque_record) VALUES ($1, $2, $3) RETURNING id`,
		email, username, opaqueRecord).Scan(&id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			if pgErr.ConstraintName == "users_username_key" {
				return nil, ErrUsernameTaken
			}
			return nil, ErrConflict
		}
		return nil, err
	}
	return &User{ID: id, Email: email, Username: username, OpaqueRecord: opaqueRecord}, nil
}

func (r *UserRepo) GetByEmail(ctx context.Context, email string) (*User, error) {
	var u User
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, username, opaque_record FROM users WHERE email=$1`, email).
		Scan(&u.ID, &u.Email, &u.Username, &u.OpaqueRecord)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// FindByEmailOrUsername resolves an existing account for workspace invites/adds.
func (r *UserRepo) FindByEmailOrUsername(ctx context.Context, q string) (*User, error) {
	var u User
	err := r.pool.QueryRow(ctx,
		`SELECT id, email, username, opaque_record FROM users WHERE email=$1 OR username=$1`, q).
		Scan(&u.ID, &u.Email, &u.Username, &u.OpaqueRecord)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}
