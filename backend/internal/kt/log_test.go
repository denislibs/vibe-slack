package kt

import (
	"testing"

	"github.com/transparency-dev/merkle/proof"
	"github.com/transparency-dev/merkle/rfc6962"
)

func leafHashes(n int) [][]byte {
	out := make([][]byte, n)
	for i := 0; i < n; i++ {
		out[i] = LeafHash(CanonicalLeaf("id", int64(i), [][]byte{[]byte{byte(i)}}))
	}
	return out
}

func TestInclusionProofVerifies(t *testing.T) {
	hashes := leafHashes(7)
	root := Root(hashes)
	pf, err := InclusionProof(hashes, 3)
	if err != nil {
		t.Fatalf("inclusion: %v", err)
	}
	if err := proof.VerifyInclusion(rfc6962.DefaultHasher, 3, 7, hashes[3], pf, root); err != nil {
		t.Fatalf("verify inclusion failed: %v", err)
	}
	if err := proof.VerifyInclusion(rfc6962.DefaultHasher, 3, 7, hashes[4], pf, root); err == nil {
		t.Fatal("tampered leaf must not verify")
	}
}

func TestConsistencyProofVerifies(t *testing.T) {
	hashes := leafHashes(8)
	root5 := Root(hashes[:5])
	root8 := Root(hashes[:8])
	pf, err := ConsistencyProof(hashes, 5, 8)
	if err != nil {
		t.Fatalf("consistency: %v", err)
	}
	if err := proof.VerifyConsistency(rfc6962.DefaultHasher, 5, 8, pf, root5, root8); err != nil {
		t.Fatalf("verify consistency failed: %v", err)
	}
	bad := append([]byte(nil), root8...)
	bad[0] ^= 0xff
	if err := proof.VerifyConsistency(rfc6962.DefaultHasher, 5, 8, pf, root5, bad); err == nil {
		t.Fatal("forged root must not verify")
	}
}
