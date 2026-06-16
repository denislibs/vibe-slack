package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

var ErrInvalidSession = errors.New("session: invalid or expired")

// Session is the state stored per token.
type Session struct {
	UserID   string `json:"user_id"`
	DeviceID string `json:"device_id"`
}

// Manager issues and validates revocable sessions backed by Redis.
// Keys: session:{token} -> JSON; user_sessions:{userID} -> SET of tokens.
type Manager struct {
	rdb *goredis.Client
	ttl time.Duration
}

func NewManager(rdb *goredis.Client, ttl time.Duration) *Manager {
	return &Manager{rdb: rdb, ttl: ttl}
}

func sessionKey(token string) string { return "session:" + token }
func userKey(userID string) string   { return "user_sessions:" + userID }

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (m *Manager) Issue(ctx context.Context, userID, deviceID string) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	data, _ := json.Marshal(Session{UserID: userID, DeviceID: deviceID})
	pipe := m.rdb.TxPipeline()
	pipe.Set(ctx, sessionKey(token), data, m.ttl)
	pipe.SAdd(ctx, userKey(userID), token)
	pipe.Expire(ctx, userKey(userID), m.ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return "", err
	}
	return token, nil
}

func (m *Manager) Validate(ctx context.Context, token string) (*Session, error) {
	data, err := m.rdb.Get(ctx, sessionKey(token)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return nil, ErrInvalidSession
	}
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("corrupt session: %w", err)
	}
	m.rdb.Expire(ctx, sessionKey(token), m.ttl) // sliding expiry
	return &s, nil
}

func (m *Manager) BindDevice(ctx context.Context, token, deviceID string) error {
	s, err := m.Validate(ctx, token)
	if err != nil {
		return err
	}
	s.DeviceID = deviceID
	data, _ := json.Marshal(s)
	return m.rdb.Set(ctx, sessionKey(token), data, m.ttl).Err()
}

func (m *Manager) Revoke(ctx context.Context, token string) error {
	s, err := m.Validate(ctx, token)
	if err == nil {
		m.rdb.SRem(ctx, userKey(s.UserID), token)
	}
	return m.rdb.Del(ctx, sessionKey(token)).Err()
}

func (m *Manager) RevokeAll(ctx context.Context, userID string) error {
	tokens, err := m.rdb.SMembers(ctx, userKey(userID)).Result()
	if err != nil {
		return err
	}
	pipe := m.rdb.TxPipeline()
	for _, tok := range tokens {
		pipe.Del(ctx, sessionKey(tok))
	}
	pipe.Del(ctx, userKey(userID))
	_, err = pipe.Exec(ctx)
	return err
}
