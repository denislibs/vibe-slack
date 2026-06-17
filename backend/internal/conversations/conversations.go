package conversations

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/messenger/backend/internal/store"
)

var (
	ErrForbidden = errors.New("conversations: forbidden")
	ErrNotMember = errors.New("conversations: not a member")
	ErrInvalid   = errors.New("conversations: invalid input")
)

type Repo interface {
	CreateChannel(ctx context.Context, groupID, wsID, visibility, name, creator string) (*store.Conversation, error)
	GetOrCreateDM(ctx context.Context, groupID, wsID, creator, target string) (*store.Conversation, bool, error)
	Get(ctx context.Context, groupID string) (*store.Conversation, error)
	IsMember(ctx context.Context, groupID, userID string) (bool, error)
	AddUser(ctx context.Context, groupID, userID string) error
	RemoveUser(ctx context.Context, groupID, userID string) error
	MemberUserIDs(ctx context.Context, groupID string) ([]string, error)
	ListForUser(ctx context.Context, wsID, userID string) ([]store.Conversation, error)
}
type WorkspaceRoles interface {
	RoleOf(ctx context.Context, workspaceID, userID string) (string, error)
}
type Users interface {
	FindByEmailOrUsername(ctx context.Context, q string) (*store.User, error)
}

type Service struct {
	repo  Repo
	wsr   WorkspaceRoles
	users Users
}

func NewService(repo Repo, wsr WorkspaceRoles, users Users) *Service {
	return &Service{repo: repo, wsr: wsr, users: users}
}

func newGroupID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (s *Service) requireWSMember(ctx context.Context, wsID, userID string) error {
	_, err := s.wsr.RoleOf(ctx, wsID, userID)
	if errors.Is(err, store.ErrNotFound) {
		return ErrNotMember
	}
	return err
}

func (s *Service) CreateChannel(ctx context.Context, callerID, wsID, visibility, name string) (*store.Conversation, error) {
	if err := s.requireWSMember(ctx, wsID, callerID); err != nil {
		return nil, err
	}
	if visibility != "public" && visibility != "private" {
		return nil, ErrInvalid
	}
	if strings.TrimSpace(name) == "" {
		return nil, ErrInvalid
	}
	return s.repo.CreateChannel(ctx, newGroupID(), wsID, visibility, strings.TrimSpace(name), callerID)
}

func (s *Service) CreateDM(ctx context.Context, callerID, wsID, emailOrUsername string) (*store.Conversation, bool, error) {
	if err := s.requireWSMember(ctx, wsID, callerID); err != nil {
		return nil, false, err
	}
	target, err := s.users.FindByEmailOrUsername(ctx, strings.TrimSpace(emailOrUsername))
	if err != nil {
		return nil, false, err // store.ErrNotFound
	}
	if target.ID == callerID {
		return nil, false, ErrInvalid
	}
	if err := s.requireWSMember(ctx, wsID, target.ID); err != nil {
		// Target isn't in this workspace. Return the SAME error as "no such user" so a
		// caller can't distinguish a real account in another tenant from a non-existent
		// one — that difference would be a cross-tenant enumeration oracle.
		if errors.Is(err, ErrNotMember) {
			return nil, false, store.ErrNotFound
		}
		return nil, false, err
	}
	return s.repo.GetOrCreateDM(ctx, newGroupID(), wsID, callerID, target.ID)
}

func (s *Service) List(ctx context.Context, callerID, wsID string) ([]store.Conversation, error) {
	if err := s.requireWSMember(ctx, wsID, callerID); err != nil {
		return nil, err
	}
	return s.repo.ListForUser(ctx, wsID, callerID)
}

// visibleTo reports whether caller may see/act on conv (member, or public channel + WS member).
func (s *Service) visibleTo(ctx context.Context, callerID string, c *store.Conversation) (bool, error) {
	member, err := s.repo.IsMember(ctx, c.GroupID, callerID)
	if err != nil {
		return false, err
	}
	if member {
		return true, nil
	}
	if c.Type == "channel" && c.Visibility == "public" {
		if err := s.requireWSMember(ctx, c.WorkspaceID, callerID); err != nil {
			if errors.Is(err, ErrNotMember) {
				return false, nil // not a workspace member → not visible
			}
			return false, err // propagate real errors instead of masking them as 404
		}
		return true, nil
	}
	return false, nil
}

func (s *Service) Get(ctx context.Context, callerID, groupID string) (*store.Conversation, error) {
	c, err := s.repo.Get(ctx, groupID)
	if err != nil {
		return nil, err // store.ErrNotFound
	}
	ok, err := s.visibleTo(ctx, callerID, c)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotMember
	}
	return c, nil
}

func (s *Service) Members(ctx context.Context, callerID, groupID string) ([]string, error) {
	if _, err := s.Get(ctx, callerID, groupID); err != nil {
		return nil, err
	}
	return s.repo.MemberUserIDs(ctx, groupID)
}

func (s *Service) Join(ctx context.Context, callerID, groupID string) error {
	c, err := s.repo.Get(ctx, groupID)
	if err != nil {
		return err
	}
	if c.Type != "channel" || c.Visibility != "public" {
		return ErrNotMember // don't reveal private/dm existence to outsiders
	}
	if err := s.requireWSMember(ctx, c.WorkspaceID, callerID); err != nil {
		return err
	}
	if err := s.repo.AddUser(ctx, groupID, callerID); err != nil && !errors.Is(err, store.ErrConflict) {
		return err
	}
	return nil // idempotent
}

func (s *Service) AddUser(ctx context.Context, callerID, groupID, emailOrUsername string) (*store.User, error) {
	c, err := s.repo.Get(ctx, groupID)
	if err != nil {
		return nil, err
	}
	member, err := s.repo.IsMember(ctx, groupID, callerID)
	if err != nil {
		return nil, err
	}
	if c.Visibility == "private" || c.Type == "dm" {
		if !member {
			return nil, ErrNotMember
		}
	} else { // public channel: any WS member may add
		if err := s.requireWSMember(ctx, c.WorkspaceID, callerID); err != nil {
			return nil, err
		}
	}
	if c.Type == "dm" {
		return nil, ErrForbidden // DM membership is fixed
	}
	target, err := s.users.FindByEmailOrUsername(ctx, strings.TrimSpace(emailOrUsername))
	if err != nil {
		return nil, err
	}
	if err := s.requireWSMember(ctx, c.WorkspaceID, target.ID); err != nil {
		return nil, ErrInvalid
	}
	if err := s.repo.AddUser(ctx, groupID, target.ID); err != nil {
		return nil, err // store.ErrConflict → 409
	}
	return target, nil
}

func (s *Service) RemoveUser(ctx context.Context, callerID, groupID, targetUserID string) error {
	c, err := s.repo.Get(ctx, groupID)
	if err != nil {
		return err
	}
	member, err := s.repo.IsMember(ctx, groupID, callerID)
	if err != nil {
		return err
	}
	if !member {
		return ErrNotMember
	}
	if c.Type == "dm" {
		return ErrForbidden // can't leave a DM in v1
	}
	if targetUserID != callerID && c.CreatedBy != callerID {
		return ErrForbidden // only self-removal or the channel creator removing others
	}
	return s.repo.RemoveUser(ctx, groupID, targetUserID)
}

// IsMember is used by the device-roster authz layer.
func (s *Service) IsMember(ctx context.Context, groupID, userID string) (bool, error) {
	return s.repo.IsMember(ctx, groupID, userID)
}
