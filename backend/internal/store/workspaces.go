package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
)

type Workspace struct {
	ID, Name, Slug, OwnerUserID string
	CreatedAt                   time.Time
}
type WorkspaceWithRole struct{ ID, Name, Slug, Role string }
type WorkspaceMember struct{ UserID, Username, Email, Role string }

type WorkspaceRepo struct{ pool *pgxpool.Pool }

func NewWorkspaceRepo(pool *pgxpool.Pool) *WorkspaceRepo { return &WorkspaceRepo{pool: pool} }

// Create inserts a workspace and its owner membership in one transaction.
func (r *WorkspaceRepo) Create(ctx context.Context, name, slug, ownerUserID string) (*Workspace, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var id string
	var createdAt time.Time
	err = tx.QueryRow(ctx,
		`INSERT INTO workspaces (name, slug, owner_user_id) VALUES ($1,$2,$3) RETURNING id, created_at`,
		name, slug, ownerUserID).Scan(&id, &createdAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			// The only unique constraint on this insert is the slug; surface it
			// distinctly so the service can pick a fresh slug and retry.
			if pgErr.ConstraintName == "workspaces_slug_key" {
				return nil, ErrSlugTaken
			}
			return nil, ErrConflict
		}
		return nil, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1,$2,$3)`,
		id, ownerUserID, RoleOwner); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &Workspace{ID: id, Name: name, Slug: slug, OwnerUserID: ownerUserID, CreatedAt: createdAt}, nil
}

func (r *WorkspaceRepo) SlugExists(ctx context.Context, slug string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT FROM workspaces WHERE slug=$1)`, slug).Scan(&exists)
	return exists, err
}

func (r *WorkspaceRepo) ListForUser(ctx context.Context, userID string) ([]WorkspaceWithRole, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT w.id, w.name, w.slug, m.role
		   FROM workspace_members m JOIN workspaces w ON w.id = m.workspace_id
		  WHERE m.user_id=$1 ORDER BY w.created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkspaceWithRole
	for rows.Next() {
		var w WorkspaceWithRole
		if err := rows.Scan(&w.ID, &w.Name, &w.Slug, &w.Role); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (r *WorkspaceRepo) Members(ctx context.Context, workspaceID string) ([]WorkspaceMember, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT u.id, u.username, u.email, m.role
		   FROM workspace_members m JOIN users u ON u.id = m.user_id
		  WHERE m.workspace_id=$1 ORDER BY m.joined_at`, workspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkspaceMember
	for rows.Next() {
		var m WorkspaceMember
		if err := rows.Scan(&m.UserID, &m.Username, &m.Email, &m.Role); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SearchMembers returns workspace members whose username or email starts with q
// (prefix, case-insensitive). Empty q returns no rows.
func (r *WorkspaceRepo) SearchMembers(ctx context.Context, wsID, q string) ([]WorkspaceMember, error) {
	if q == "" {
		return []WorkspaceMember{}, nil
	}
	rows, err := r.pool.Query(ctx,
		`SELECT u.id, u.username, u.email, m.role
		   FROM workspace_members m JOIN users u ON u.id = m.user_id
		  WHERE m.workspace_id=$1 AND (u.username ILIKE $2 OR u.email ILIKE $2)
		  ORDER BY u.username LIMIT 20`, wsID, q+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WorkspaceMember
	for rows.Next() {
		var m WorkspaceMember
		if err := rows.Scan(&m.UserID, &m.Username, &m.Email, &m.Role); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *WorkspaceRepo) RoleOf(ctx context.Context, workspaceID, userID string) (string, error) {
	var role string
	err := r.pool.QueryRow(ctx,
		`SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`,
		workspaceID, userID).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return role, err
}

func (r *WorkspaceRepo) AddMember(ctx context.Context, workspaceID, userID, role string) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO workspace_members (workspace_id, user_id, role) VALUES ($1,$2,$3)`,
		workspaceID, userID, role)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrConflict
		}
	}
	return err
}

func (r *WorkspaceRepo) RemoveMember(ctx context.Context, workspaceID, userID string) error {
	ct, err := r.pool.Exec(ctx,
		`DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, workspaceID, userID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *WorkspaceRepo) SetRole(ctx context.Context, workspaceID, userID, role string) error {
	ct, err := r.pool.Exec(ctx,
		`UPDATE workspace_members SET role=$3 WHERE workspace_id=$1 AND user_id=$2`,
		workspaceID, userID, role)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
