package kt

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// jsonSTH is the wire shape of a signed tree head in the interop fixture.
type jsonSTH struct {
	TreeSize  int64  `json:"tree_size"`
	RootHash  string `json:"root_hash"`
	Signature string `json:"signature"`
}

type jsonLookup struct {
	Identity  string   `json:"identity"`
	LeafIndex int64    `json:"leaf_index"`
	Version   int64    `json:"version"`
	DeviceSet string   `json:"device_set"`
	AuditPath []string `json:"audit_path"`
	STH       jsonSTH  `json:"sth"`
}

type jsonVectors struct {
	KTPublicKey string     `json:"kt_public_key"`
	Lookup      jsonLookup `json:"lookup"`
	// LookupFrom is a second lookup (leaf 0) proven against the earlier tree of
	// size = consistency.from, so the TS verifier can seed a trusted STH that has
	// a real inclusion proof before exercising the from->to consistency step.
	LookupFrom jsonLookup `json:"lookup_from"`
	// Fork is a validly-signed STH at the SAME tree size as lookup.sth but with a
	// different root hash, modelling a split-view / forked log. It lets the TS
	// verifier exercise the equal-size fork-detection branch with a signature that
	// actually passes verification.
	Fork        jsonSTH `json:"fork"`
	Consistency struct {
		From    int64    `json:"from"`
		To      int64    `json:"to"`
		Proof   []string `json:"proof"`
		STHFrom jsonSTH  `json:"sth_from"`
		STHTo   jsonSTH  `json:"sth_to"`
	} `json:"consistency"`
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func b64Slice(bs [][]byte) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = b64(b)
	}
	return out
}

// TestExportVectors generates a ground-truth interop fixture for the TS KT proof
// verifier, using the real RFC6962 prover + a real signed STH. It always writes
// (no flag): the output is harmless testdata under the client tree.
func TestExportVectors(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	signer := NewSTHSigner(priv)

	// Build 5 leaves, one per identity. Each leaf is the canonical preimage
	// (identity || version || device keys) — the same bytes the store persists
	// as DeviceSet, so the TS side recomputes leafHash(device_set).
	const n = 5
	identities := []string{"alice@corp", "bob@corp", "carol@corp", "dave@corp", "erin@corp"}
	versions := []int64{1, 1, 2, 1, 3}
	canon := make([][]byte, n)
	hashes := make([][]byte, n)
	for i := 0; i < n; i++ {
		canon[i] = CanonicalLeaf(identities[i], versions[i], [][]byte{[]byte(fmt.Sprintf("dev-key-%d", i))})
		hashes[i] = LeafHash(canon[i])
	}

	// Lookup target: leaf index 2 against the final tree of size 5.
	const targetIdx = 2
	rootFinal := Root(hashes[:n])
	auditPath, err := InclusionProof(hashes[:n], targetIdx)
	if err != nil {
		t.Fatalf("inclusion: %v", err)
	}
	sthFinal := jsonSTH{
		TreeSize:  n,
		RootHash:  b64(rootFinal),
		Signature: b64(signer.Sign(n, rootFinal)),
	}

	// Consistency between an earlier tree (size 3) and the final tree (size 5).
	const fromSize = 3
	rootFrom := Root(hashes[:fromSize])
	consProof, err := ConsistencyProof(hashes[:n], fromSize, n)
	if err != nil {
		t.Fatalf("consistency: %v", err)
	}
	sthFrom := jsonSTH{
		TreeSize:  fromSize,
		RootHash:  b64(rootFrom),
		Signature: b64(signer.Sign(fromSize, rootFrom)),
	}

	// Second lookup: leaf index 0 proven against the earlier tree of size fromSize.
	const fromTargetIdx = 0
	auditPathFrom, err := InclusionProof(hashes[:fromSize], fromTargetIdx)
	if err != nil {
		t.Fatalf("inclusion (from): %v", err)
	}

	// Forked tree: same size n, but one leaf differs, yielding a different root.
	// Signed with the same key so the STH signature verifies; the verifier must
	// reject it at the equal-size monotonicity check (different root, same size).
	forkHashes := make([][]byte, n)
	copy(forkHashes, hashes[:n])
	forkCanon := CanonicalLeaf(identities[n-1], versions[n-1]+100, [][]byte{[]byte("dev-key-forked")})
	forkHashes[n-1] = LeafHash(forkCanon)
	forkRoot := Root(forkHashes)
	forkSTH := jsonSTH{
		TreeSize:  n,
		RootHash:  b64(forkRoot),
		Signature: b64(signer.Sign(n, forkRoot)),
	}

	var v jsonVectors
	v.KTPublicKey = b64(pub)
	v.Lookup.Identity = identities[targetIdx]
	v.Lookup.LeafIndex = targetIdx
	v.Lookup.Version = versions[targetIdx]
	v.Lookup.DeviceSet = b64(canon[targetIdx])
	v.Lookup.AuditPath = b64Slice(auditPath)
	v.Lookup.STH = sthFinal
	v.LookupFrom.Identity = identities[fromTargetIdx]
	v.LookupFrom.LeafIndex = fromTargetIdx
	v.LookupFrom.Version = versions[fromTargetIdx]
	v.LookupFrom.DeviceSet = b64(canon[fromTargetIdx])
	v.LookupFrom.AuditPath = b64Slice(auditPathFrom)
	v.LookupFrom.STH = sthFrom
	v.Fork = forkSTH
	v.Consistency.From = fromSize
	v.Consistency.To = n
	v.Consistency.Proof = b64Slice(consProof)
	v.Consistency.STHFrom = sthFrom
	v.Consistency.STHTo = sthFinal

	data, err := json.MarshalIndent(&v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Repo root is two levels up from internal/kt (backend/internal/kt -> backend -> root).
	outDir := filepath.Join("..", "..", "..", "client", "src", "shared", "lib", "kt", "testdata")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir testdata: %v", err)
	}
	outPath := filepath.Join(outDir, "kt_vectors.json")
	if err := os.WriteFile(outPath, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	t.Logf("wrote interop fixture: %s (%d bytes)", outPath, len(data))
}
