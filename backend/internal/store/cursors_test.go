package store

import (
	"context"
	"testing"
)

func TestCursorAdvanceIsMonotonic(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	repo := NewCursorRepo(pool)
	dev := "cccccccc-1111-1111-1111-111111111111"

	if got, _ := repo.Get(ctx, dev, "g1"); got != 0 {
		t.Fatalf("fresh cursor should be 0, got %d", got)
	}
	if err := repo.Advance(ctx, dev, "g1", 5); err != nil {
		t.Fatalf("advance: %v", err)
	}
	if got, _ := repo.Get(ctx, dev, "g1"); got != 5 {
		t.Fatalf("expected 5, got %d", got)
	}
	if err := repo.Advance(ctx, dev, "g1", 3); err != nil {
		t.Fatalf("advance lower: %v", err)
	}
	if got, _ := repo.Get(ctx, dev, "g1"); got != 5 {
		t.Fatalf("cursor must not regress; expected 5, got %d", got)
	}
}
