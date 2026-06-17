package kt

import (
	"context"

	"github.com/messenger/backend/internal/store"
)

type Service struct{ kt *store.KTRepo }

func NewService(kt *store.KTRepo) *Service { return &Service{kt: kt} }

// LookupResult is a verifiable answer to "what are identity's current device keys".
type LookupResult struct {
	LeafIndex int64
	Version   int64
	DeviceSet []byte
	LeafHash  []byte
	AuditPath [][]byte
	STH       store.KTSTH
}

func (s *Service) LatestSTH(ctx context.Context) (*store.KTSTH, error) {
	return s.kt.LatestSTH(ctx)
}

// Lookup returns identity's latest leaf plus an inclusion proof against the latest STH.
// It fetches the STH first, then a leaf bounded by the STH's tree size, so the returned
// leaf is always covered by the STH (no out-of-range proofs in the commit/issue window).
func (s *Service) Lookup(ctx context.Context, identity string) (*LookupResult, error) {
	sth, err := s.kt.LatestSTH(ctx)
	if err != nil {
		return nil, err
	}
	leaf, err := s.kt.LatestLeafForIdentityAt(ctx, identity, sth.TreeSize)
	if err != nil {
		return nil, err
	}
	hashes, err := s.kt.LeafHashes(ctx, sth.TreeSize)
	if err != nil {
		return nil, err
	}
	path, err := InclusionProof(hashes, uint64(leaf.LeafIndex))
	if err != nil {
		return nil, err
	}
	return &LookupResult{
		LeafIndex: leaf.LeafIndex, Version: leaf.Version, DeviceSet: leaf.DeviceSet,
		LeafHash: leaf.LeafHash, AuditPath: path, STH: *sth,
	}, nil
}

// Inclusion returns the audit path for a leaf in a tree of the given size.
func (s *Service) Inclusion(ctx context.Context, leafIndex, treeSize int64) ([][]byte, error) {
	hashes, err := s.kt.LeafHashes(ctx, treeSize)
	if err != nil {
		return nil, err
	}
	return InclusionProof(hashes, uint64(leafIndex))
}

// Consistency returns a consistency proof between two tree sizes.
func (s *Service) Consistency(ctx context.Context, from, to int64) ([][]byte, error) {
	hashes, err := s.kt.LeafHashes(ctx, to)
	if err != nil {
		return nil, err
	}
	return ConsistencyProof(hashes, uint64(from), uint64(to))
}
