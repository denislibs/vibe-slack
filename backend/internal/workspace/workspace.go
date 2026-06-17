package workspace

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/messenger/backend/internal/store"
)

var (
	ErrForbidden = errors.New("workspace: forbidden")
	ErrNotMember = errors.New("workspace: not a member")
	ErrInvalid   = errors.New("workspace: invalid input")
)

type Repo interface {
	Create(ctx context.Context, name, slug, ownerUserID string) (*store.Workspace, error)
	SlugExists(ctx context.Context, slug string) (bool, error)
	ListForUser(ctx context.Context, userID string) ([]store.WorkspaceWithRole, error)
	Members(ctx context.Context, workspaceID string) ([]store.WorkspaceMember, error)
	RoleOf(ctx context.Context, workspaceID, userID string) (string, error)
	AddMember(ctx context.Context, workspaceID, userID, role string) error
	RemoveMember(ctx context.Context, workspaceID, userID string) error
	SetRole(ctx context.Context, workspaceID, userID, role string) error
}

type UserLookup interface {
	FindByEmailOrUsername(ctx context.Context, q string) (*store.User, error)
}

type Service struct {
	repo  Repo
	users UserLookup
}

func NewService(repo Repo, users UserLookup) *Service { return &Service{repo: repo, users: users} }

func (s *Service) Create(ctx context.Context, ownerUserID, name string) (*store.Workspace, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalid
	}
	slug, err := s.uniqueSlug(ctx, slugify(name))
	if err != nil {
		return nil, err
	}
	return s.repo.Create(ctx, name, slug, ownerUserID)
}

func (s *Service) ListForUser(ctx context.Context, userID string) ([]store.WorkspaceWithRole, error) {
	return s.repo.ListForUser(ctx, userID)
}

func (s *Service) requireRole(ctx context.Context, workspaceID, userID string) (string, error) {
	role, err := s.repo.RoleOf(ctx, workspaceID, userID)
	if errors.Is(err, store.ErrNotFound) {
		return "", ErrNotMember
	}
	return role, err
}

func (s *Service) Members(ctx context.Context, callerID, workspaceID string) ([]store.WorkspaceMember, error) {
	if _, err := s.requireRole(ctx, workspaceID, callerID); err != nil {
		return nil, err
	}
	return s.repo.Members(ctx, workspaceID)
}

func (s *Service) AddMember(ctx context.Context, callerID, workspaceID, emailOrUsername string) (*store.WorkspaceMember, error) {
	role, err := s.requireRole(ctx, workspaceID, callerID)
	if err != nil {
		return nil, err
	}
	if role != store.RoleOwner && role != store.RoleAdmin {
		return nil, ErrForbidden
	}
	u, err := s.users.FindByEmailOrUsername(ctx, strings.TrimSpace(emailOrUsername))
	if err != nil {
		return nil, err
	}
	if err := s.repo.AddMember(ctx, workspaceID, u.ID, store.RoleMember); err != nil {
		return nil, err
	}
	return &store.WorkspaceMember{UserID: u.ID, Username: u.Username, Email: u.Email, Role: store.RoleMember}, nil
}

func (s *Service) SetRole(ctx context.Context, callerID, workspaceID, targetUserID, role string) error {
	if role != store.RoleAdmin && role != store.RoleMember {
		return ErrInvalid
	}
	callerRole, err := s.requireRole(ctx, workspaceID, callerID)
	if err != nil {
		return err
	}
	if callerRole != store.RoleOwner {
		return ErrForbidden
	}
	targetRole, err := s.repo.RoleOf(ctx, workspaceID, targetUserID)
	if errors.Is(err, store.ErrNotFound) {
		return store.ErrNotFound
	}
	if err != nil {
		return err
	}
	if targetRole == store.RoleOwner {
		return ErrForbidden
	}
	return s.repo.SetRole(ctx, workspaceID, targetUserID, role)
}

func (s *Service) RemoveMember(ctx context.Context, callerID, workspaceID, targetUserID string) error {
	callerRole, err := s.requireRole(ctx, workspaceID, callerID)
	if err != nil {
		return err
	}
	targetRole, err := s.repo.RoleOf(ctx, workspaceID, targetUserID)
	if errors.Is(err, store.ErrNotFound) {
		return store.ErrNotFound
	}
	if err != nil {
		return err
	}
	if targetRole == store.RoleOwner {
		return ErrForbidden
	}
	switch callerRole {
	case store.RoleOwner:
	case store.RoleAdmin:
		if targetRole != store.RoleMember {
			return ErrForbidden
		}
	default:
		return ErrForbidden
	}
	return s.repo.RemoveMember(ctx, workspaceID, targetUserID)
}

func (s *Service) Leave(ctx context.Context, callerID, workspaceID string) error {
	role, err := s.requireRole(ctx, workspaceID, callerID)
	if err != nil {
		return err
	}
	if role == store.RoleOwner {
		return ErrForbidden
	}
	return s.repo.RemoveMember(ctx, workspaceID, callerID)
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(name string) string {
	s := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if s == "" {
		s = "ws"
	}
	return s
}

func (s *Service) uniqueSlug(ctx context.Context, base string) (string, error) {
	candidate := base
	for i := 2; i <= 1000; i++ {
		exists, err := s.repo.SlugExists(ctx, candidate)
		if err != nil {
			return "", err
		}
		if !exists {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
	return "", ErrInvalid
}
