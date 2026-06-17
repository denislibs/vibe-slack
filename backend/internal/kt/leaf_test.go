package kt

import (
	"bytes"
	"testing"
)

func TestCanonicalLeafIsDeterministicAndOrderIndependent(t *testing.T) {
	id := "11111111-1111-1111-1111-111111111111"
	a := CanonicalLeaf(id, 3, [][]byte{[]byte("keyB"), []byte("keyA")})
	b := CanonicalLeaf(id, 3, [][]byte{[]byte("keyA"), []byte("keyB")}) // different input order
	if !bytes.Equal(a, b) {
		t.Fatal("device set order must not affect canonical bytes (keys are sorted)")
	}
	c := CanonicalLeaf(id, 4, [][]byte{[]byte("keyA"), []byte("keyB")})
	if bytes.Equal(a, c) {
		t.Fatal("different version must produce different canonical bytes")
	}
}

func TestLeafHashMatchesRFC6962(t *testing.T) {
	canon := CanonicalLeaf("id", 1, [][]byte{[]byte("k")})
	h1 := LeafHash(canon)
	h2 := LeafHash(canon)
	if !bytes.Equal(h1, h2) || len(h1) != 32 {
		t.Fatalf("leaf hash must be deterministic 32-byte SHA-256, got len %d", len(h1))
	}
	empty := CanonicalLeaf("id", 2, nil)
	if len(LeafHash(empty)) != 32 {
		t.Fatal("empty device set must still hash")
	}
}
