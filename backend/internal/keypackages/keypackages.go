package keypackages

import "context"

type Repo interface {
	Upload(ctx context.Context, deviceID string, packages [][]byte, lastResort bool) error
	CountAvailable(ctx context.Context, deviceID string) (int, error)
	Consume(ctx context.Context, deviceID string) ([]byte, bool, error)
}

type Service struct{ repo Repo }

func NewService(repo Repo) *Service { return &Service{repo: repo} }

func (s *Service) Upload(ctx context.Context, deviceID string, packages [][]byte) error {
	return s.repo.Upload(ctx, deviceID, packages, false)
}
func (s *Service) Count(ctx context.Context, deviceID string) (int, error) {
	return s.repo.CountAvailable(ctx, deviceID)
}
func (s *Service) Consume(ctx context.Context, deviceID string) ([]byte, bool, error) {
	return s.repo.Consume(ctx, deviceID)
}
