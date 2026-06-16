package devices

import (
	"context"

	"github.com/messenger/backend/internal/store"
)

type Repo interface {
	Enroll(ctx context.Context, userID string, signingPubKey []byte, label string) (*store.Device, error)
	ListByUser(ctx context.Context, userID string) ([]store.Device, error)
	Revoke(ctx context.Context, deviceID string) error
}

type Service struct{ repo Repo }

func NewService(repo Repo) *Service { return &Service{repo: repo} }

func (s *Service) Enroll(ctx context.Context, userID string, signingPubKey []byte, label string) (*store.Device, error) {
	return s.repo.Enroll(ctx, userID, signingPubKey, label)
}
func (s *Service) List(ctx context.Context, userID string) ([]store.Device, error) {
	return s.repo.ListByUser(ctx, userID)
}
func (s *Service) Revoke(ctx context.Context, deviceID string) error {
	return s.repo.Revoke(ctx, deviceID)
}
