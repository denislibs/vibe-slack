package store

import (
	"context"
	"errors"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Conversation struct {
	GroupID, WorkspaceID, Type, Visibility, Name, CreatedBy string
	// Member reports whether a specific caller belongs to the conversation. It is
	// only populated by caller-scoped queries (ListForUser); other reads leave it
	// false. The client uses it to decide whether a public channel still needs an
	// external-commit join before the user can send.
	Member bool
}

type ConvRepo struct{ pool *pgxpool.Pool }

func NewConvRepo(pool *pgxpool.Pool) *ConvRepo { return &ConvRepo{pool: pool} }

func dmKey(a, b string) string {
	pair := []string{a, b}
	sort.Strings(pair)
	return pair[0] + "|" + pair[1]
}

// insert creates the DS journal row + meta (+ optional members) in one tx.
func (r *ConvRepo) insert(ctx context.Context, groupID, wsID, typ, visibility, name, creator, dmk string, members []string) (*Conversation, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `INSERT INTO conversations (group_id) VALUES ($1)`, groupID); err != nil {
		return nil, err
	}
	var dmArg any
	if dmk != "" {
		dmArg = dmk
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO conversation_meta (group_id, workspace_id, type, visibility, name, created_by, dm_key)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		groupID, wsID, typ, visibility, name, creator, dmArg); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrConflict
		}
		return nil, err
	}
	for _, uid := range members {
		if _, err := tx.Exec(ctx,
			`INSERT INTO conversation_user_members (group_id, user_id) VALUES ($1,$2)`, groupID, uid); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	// The creator (and, for DMs, the target) are inserted into
	// conversation_user_members above, so the caller creating it is a member.
	return &Conversation{GroupID: groupID, WorkspaceID: wsID, Type: typ, Visibility: visibility, Name: name, CreatedBy: creator, Member: true}, nil
}

func (r *ConvRepo) CreateChannel(ctx context.Context, groupID, wsID, visibility, name, creator string) (*Conversation, error) {
	return r.insert(ctx, groupID, wsID, "channel", visibility, name, creator, "", []string{creator})
}

func (r *ConvRepo) GetOrCreateDM(ctx context.Context, groupID, wsID, creator, target string) (*Conversation, bool, error) {
	key := dmKey(creator, target)
	if existing, err := r.getByDMKey(ctx, wsID, key); err == nil {
		return existing, false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}
	conv, err := r.insert(ctx, groupID, wsID, "dm", "private", "", creator, key, []string{creator, target})
	if errors.Is(err, ErrConflict) { // lost a race; return the now-existing DM
		existing, gerr := r.getByDMKey(ctx, wsID, key)
		return existing, false, gerr
	}
	if err != nil {
		return nil, false, err
	}
	return conv, true, nil
}

func (r *ConvRepo) getByDMKey(ctx context.Context, wsID, key string) (*Conversation, error) {
	var c Conversation
	err := r.pool.QueryRow(ctx,
		`SELECT group_id, workspace_id, type, visibility, name, created_by FROM conversation_meta
		 WHERE workspace_id=$1 AND dm_key=$2`, wsID, key).
		Scan(&c.GroupID, &c.WorkspaceID, &c.Type, &c.Visibility, &c.Name, &c.CreatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}

func (r *ConvRepo) Get(ctx context.Context, groupID string) (*Conversation, error) {
	var c Conversation
	err := r.pool.QueryRow(ctx,
		`SELECT group_id, workspace_id, type, visibility, name, created_by FROM conversation_meta WHERE group_id=$1`, groupID).
		Scan(&c.GroupID, &c.WorkspaceID, &c.Type, &c.Visibility, &c.Name, &c.CreatedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &c, err
}

func (r *ConvRepo) IsMember(ctx context.Context, groupID, userID string) (bool, error) {
	var ok bool
	err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM conversation_user_members WHERE group_id=$1 AND user_id=$2)`, groupID, userID).Scan(&ok)
	return ok, err
}

func (r *ConvRepo) AddUser(ctx context.Context, groupID, userID string) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO conversation_user_members (group_id, user_id) VALUES ($1,$2)`, groupID, userID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrConflict
		}
	}
	return err
}

func (r *ConvRepo) RemoveUser(ctx context.Context, groupID, userID string) error {
	ct, err := r.pool.Exec(ctx, `DELETE FROM conversation_user_members WHERE group_id=$1 AND user_id=$2`, groupID, userID)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *ConvRepo) MemberUserIDs(ctx context.Context, groupID string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT user_id FROM conversation_user_members WHERE group_id=$1`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SetGroupInfo stores the latest published GroupInfo (for external-commit joins).
func (r *ConvRepo) SetGroupInfo(ctx context.Context, groupID string, info []byte) error {
	ct, err := r.pool.Exec(ctx, `UPDATE conversation_meta SET group_info=$2 WHERE group_id=$1`, groupID, info)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GroupInfo returns the last published GroupInfo, or ErrNotFound if none/unknown group.
func (r *ConvRepo) GroupInfo(ctx context.Context, groupID string) ([]byte, error) {
	var gi []byte
	err := r.pool.QueryRow(ctx, `SELECT group_info FROM conversation_meta WHERE group_id=$1`, groupID).Scan(&gi)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if gi == nil {
		return nil, ErrNotFound // not yet published
	}
	return gi, nil
}

// ListForUser returns conversations in the workspace the user can see:
// any public channel, plus any conversation the user is a member of.
func (r *ConvRepo) ListForUser(ctx context.Context, wsID, userID string) ([]Conversation, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT m.group_id, m.workspace_id, m.type, m.visibility, m.name, m.created_by,
		        EXISTS (SELECT 1 FROM conversation_user_members cm WHERE cm.group_id=m.group_id AND cm.user_id=$2) AS is_member
		   FROM conversation_meta m
		  WHERE m.workspace_id=$1
		    AND ( m.visibility='public'
		       OR EXISTS (SELECT 1 FROM conversation_user_members cm WHERE cm.group_id=m.group_id AND cm.user_id=$2) )
		  ORDER BY m.created_at`, wsID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Conversation
	for rows.Next() {
		var c Conversation
		if err := rows.Scan(&c.GroupID, &c.WorkspaceID, &c.Type, &c.Visibility, &c.Name, &c.CreatedBy, &c.Member); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
