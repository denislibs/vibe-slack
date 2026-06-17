package kt

import (
	"github.com/transparency-dev/merkle/rfc6962"
	"github.com/transparency-dev/merkle/testonly"
)

// buildTree reconstructs an in-memory Merkle tree from the first `size` stored leaf
// hashes. O(size) per call — correct-by-construction; a persistent node store is a
// future optimization.
func buildTree(leafHashes [][]byte, size int) *testonly.Tree {
	tree := testonly.New(rfc6962.DefaultHasher)
	tree.Append(leafHashes[:size]...)
	return tree
}

// Root returns the RFC 6962 Merkle root over the given leaf hashes.
func Root(leafHashes [][]byte) []byte {
	return buildTree(leafHashes, len(leafHashes)).Hash()
}

// InclusionProof returns the audit path proving leaf `index` is in the tree of
// size len(leafHashes).
func InclusionProof(leafHashes [][]byte, index uint64) ([][]byte, error) {
	tree := buildTree(leafHashes, len(leafHashes))
	return tree.InclusionProof(index, uint64(len(leafHashes)))
}

// ConsistencyProof proves the tree of size `size2` append-only-extends size `size1`.
func ConsistencyProof(leafHashes [][]byte, size1, size2 uint64) ([][]byte, error) {
	tree := buildTree(leafHashes, int(size2))
	return tree.ConsistencyProof(size1, size2)
}
