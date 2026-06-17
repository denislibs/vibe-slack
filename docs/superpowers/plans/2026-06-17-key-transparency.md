# Key Transparency Log (KT) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the append-only Key Transparency log — a chronological RFC 6962 Merkle log of "identity → device-set" bindings fed from `kt_outbox`, with signed tree heads (STH), inclusion/consistency proofs, and a verifiable key-lookup API — so a malicious server cannot silently swap a user's device keys without detection.

**Architecture:** Extends the Go modular monolith `backend/`. A background relay (single-appender via Postgres advisory lock) drains `kt_outbox`, builds a canonical leaf per identity-version capturing the user's current active device set, appends it to a Merkle log stored in Postgres, and signs a new STH. Proof math uses `github.com/transparency-dev/merkle` (RFC 6962); the tree is rebuilt in memory from stored leaf hashes per request (correct-by-construction; a persistent node store is a future optimization). A read-only HTTP API serves the latest STH, inclusion/consistency proofs, and verifiable key lookups.

**Tech Stack:** Go 1.26, `github.com/transparency-dev/merkle` v0.0.2 (`rfc6962`, `testonly`, `proof`), `crypto/ed25519` (STH signing), `github.com/jackc/pgx/v5`, `github.com/testcontainers/testcontainers-go`. Reuses `internal/{config,platform/postgres,store,httpapi,session}` and `cmd/genkeys` from plans 1–2.

**Spec:** `docs/superpowers/specs/2026-06-17-key-transparency-design.md`. VRF privacy / CONIKS-AKD, an external auditor + gossip, and the client-side KT verifier are out of scope (separate plans); this plan pins the STH/proof/leaf-canonicalization formats as their contract.

**Environment prerequisite:** Integration tests (Tasks 1, 5, 6, 7, 8, 10) use testcontainers and REQUIRE a running Docker daemon (use `postgres.BasicWaitStrategies()`). Unit tests (Tasks 2, 3, 4) need no Docker. Run `go` from inside `backend/`; commit from the repo root. If Docker is unavailable, report it — do not fake/skip integration tests.

---

## File Structure

```
backend/
  internal/platform/postgres/migrations/0003_kt.sql   # kt_leaves, kt_sths
  internal/kt/leaf.go            # canonical leaf serialization + RFC6962 leaf hash
  internal/kt/log.go             # Merkle log: Root, InclusionProof, ConsistencyProof (over stored leaf hashes)
  internal/kt/sth.go             # STH sign/verify (Ed25519) + canonical STH bytes
  internal/kt/relay.go           # outbox→leaf relay loop under advisory lock; issues STH
  internal/kt/service.go         # ties log+sth+store: Lookup, Inclusion, Consistency, LatestSTH
  internal/store/kt.go           # KTRepo: AppendLeaf, LeafHashes, LatestLeafForIdentity, PutSTH, LatestSTH, MaxVersion
  internal/httpapi/kt_handlers.go# /kt/* endpoints
  internal/config/config.go      # + KTSigningKey
  cmd/genkeys/main.go            # + print KT_SIGNING_KEY
  cmd/server/main.go             # start relay loop (background), mount /kt routes
```

`internal/kt/leaf.go` and `sth.go` are pure (no DB) so they unit-test without Docker. `log.go` wraps the merkle library. `relay.go` and `service.go` orchestrate over `store`.

---

## Milestone 1 — Schema, leaf canonicalization, Merkle log

### Task 1: KT schema migration

**Files:**
- Create: `backend/internal/platform/postgres/migrations/0003_kt.sql`
- Create: `backend/internal/platform/postgres/migrate_kt_test.go`

- [ ] **Step 1: Write the failing test (integration — Docker)**

`backend/internal/platform/postgres/migrate_kt_test.go`:
```go
package postgres

import (
	"context"
	"testing"

	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestMigrateCreatesKTTables(t *testing.T) {
	ctx := context.Background()
	ctr, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("as"), postgres.WithUsername("as"), postgres.WithPassword("as"),
		postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })
	dsn, _ := ctr.ConnectionString(ctx, "sslmode=disable")
	pool, err := Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	for _, table := range []string{"kt_leaves", "kt_sths"} {
		var exists bool
		pool.QueryRow(ctx,
			`SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name=$1)`, table).Scan(&exists)
		if !exists {
			t.Fatalf("expected table %q to exist", table)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/platform/postgres/ -run TestMigrateCreatesKTTables`
Expected: FAIL — tables absent.

- [ ] **Step 3: Write minimal implementation**

`backend/internal/platform/postgres/migrations/0003_kt.sql`:
```sql
CREATE TABLE kt_leaves (
    leaf_index BIGINT PRIMARY KEY,
    identity   UUID NOT NULL,
    version    BIGINT NOT NULL,
    device_set BYTEA NOT NULL,
    leaf_hash  BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_kt_leaves_identity ON kt_leaves (identity, version);

CREATE TABLE kt_sths (
    tree_size  BIGINT PRIMARY KEY,
    root_hash  BYTEA NOT NULL,
    signature  BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```
(The embedded migration runner picks up `0003_kt.sql` by filename order.)

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/platform/postgres/ -run TestMigrateCreatesKTTables`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/internal/platform/postgres
git -C /Users/denisurevic/Documents/slack commit -m "feat(backend): key-transparency schema (kt_leaves, kt_sths)"
```

---

### Task 2: Leaf canonicalization + RFC 6962 leaf hash

**Files:**
- Create: `backend/internal/kt/leaf.go`, `backend/internal/kt/leaf_test.go`

- [ ] **Step 1: Write the failing test (unit — no Docker)**

`backend/internal/kt/leaf_test.go`:
```go
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

	// Different version → different bytes.
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
	// Empty device set is valid ("no keys").
	empty := CanonicalLeaf("id", 2, nil)
	if len(LeafHash(empty)) != 32 {
		t.Fatal("empty device set must still hash")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/kt/ -run 'TestCanonicalLeaf|TestLeafHash'`
Expected: FAIL — `CanonicalLeaf` undefined.

- [ ] **Step 3: Write minimal implementation**

Add dep: `cd backend && go get github.com/transparency-dev/merkle@v0.0.2`

`backend/internal/kt/leaf.go`:
```go
package kt

import (
	"bytes"
	"encoding/binary"
	"sort"

	"github.com/transparency-dev/merkle/rfc6962"
)

// CanonicalLeaf produces the deterministic byte encoding of an identity→device-set
// binding at a given version. The encoding is the contract verified by KT clients:
//
//	identity_len(u32 BE) || identity ||
//	version(u64 BE) ||
//	count(u32 BE) || for each key (sorted asc): key_len(u32 BE) || key
//
// Device keys are sorted by raw bytes so input order is irrelevant.
func CanonicalLeaf(identity string, version int64, deviceSet [][]byte) []byte {
	keys := make([][]byte, len(deviceSet))
	copy(keys, deviceSet)
	sort.Slice(keys, func(i, j int) bool { return bytes.Compare(keys[i], keys[j]) < 0 })

	var buf bytes.Buffer
	writeBytes := func(b []byte) {
		var l [4]byte
		binary.BigEndian.PutUint32(l[:], uint32(len(b)))
		buf.Write(l[:])
		buf.Write(b)
	}
	writeBytes([]byte(identity))
	var v [8]byte
	binary.BigEndian.PutUint64(v[:], uint64(version))
	buf.Write(v[:])
	var c [4]byte
	binary.BigEndian.PutUint32(c[:], uint32(len(keys)))
	buf.Write(c[:])
	for _, k := range keys {
		writeBytes(k)
	}
	return buf.Bytes()
}

// LeafHash is the RFC 6962 leaf hash of canonical leaf bytes (SHA-256, 0x00 prefix).
func LeafHash(canonical []byte) []byte {
	return rfc6962.DefaultHasher.HashLeaf(canonical)
}
```
**API-version risk:** `rfc6962.DefaultHasher` is the package's default `LogHasher` in v0.0.2. If the symbol differs, run `go doc github.com/transparency-dev/merkle/rfc6962` and use the correct default-hasher accessor (it must implement `HashLeaf`/`HashChildren`/`EmptyRoot`/`Size`). Report any change.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/kt/ -run 'TestCanonicalLeaf|TestLeafHash'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/internal/kt/leaf.go backend/internal/kt/leaf_test.go backend/go.mod backend/go.sum
git -C /Users/denisurevic/Documents/slack commit -m "feat(backend): KT canonical leaf encoding + RFC6962 leaf hash"
```

---

### Task 3: Merkle log — root + inclusion/consistency proofs

**Files:**
- Create: `backend/internal/kt/log.go`, `backend/internal/kt/log_test.go`

- [ ] **Step 1: Write the failing test (unit — no Docker; round-trips against the library verifier)**

`backend/internal/kt/log_test.go`:
```go
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
	pf, err := InclusionProof(hashes, 3) // prove leaf 3 in tree of size 7
	if err != nil {
		t.Fatalf("inclusion: %v", err)
	}
	if err := proof.VerifyInclusion(rfc6962.DefaultHasher, 3, 7, hashes[3], pf, root); err != nil {
		t.Fatalf("verify inclusion failed: %v", err)
	}
	// Tamper: a wrong leaf hash must fail verification.
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
	// A forged root8 must fail.
	bad := append([]byte(nil), root8...)
	bad[0] ^= 0xff
	if err := proof.VerifyConsistency(rfc6962.DefaultHasher, 5, 8, pf, root5, bad); err == nil {
		t.Fatal("forged root must not verify")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/kt/ -run 'TestInclusionProof|TestConsistencyProof'`
Expected: FAIL — `Root` undefined.

- [ ] **Step 3: Write minimal implementation**

`backend/internal/kt/log.go`:
```go
package kt

import (
	"github.com/transparency-dev/merkle/rfc6962"
	"github.com/transparency-dev/merkle/testonly"
)

// buildTree reconstructs an in-memory Merkle tree from the first `size` stored leaf
// hashes. O(size) per call — correct-by-construction; a persistent node store is a
// future optimization (see spec).
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
```
**API-version risk:** `testonly.New`, `Tree.Append`, `Tree.Hash`, `Tree.InclusionProof(index,size)`, `Tree.ConsistencyProof(size1,size2)` are from v0.0.2. The `testonly` package is the module's reference in-memory tree; using it for proof generation is acceptable for this plan (the math is the library's). If method names differ, `go doc github.com/transparency-dev/merkle/testonly` and adjust; report changes.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/kt/ -run 'TestInclusionProof|TestConsistencyProof'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/internal/kt/log.go backend/internal/kt/log_test.go
git -C /Users/denisurevic/Documents/slack commit -m "feat(backend): KT Merkle log — root + inclusion/consistency proofs (RFC6962)"
```

---

## Milestone 2 — STH signing, repository, relay

### Task 4: STH sign/verify + genkeys + config

**Files:**
- Create: `backend/internal/kt/sth.go`, `backend/internal/kt/sth_test.go`
- Modify: `backend/internal/config/config.go` (+ `KTSigningKey`), `backend/cmd/genkeys/main.go` (+ KT key)

- [ ] **Step 1: Write the failing test (unit — no Docker)**

`backend/internal/kt/sth_test.go`:
```go
package kt

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func TestSTHSignAndVerify(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	signer := NewSTHSigner(priv)

	root := []byte("00000000000000000000000000000000")
	sig := signer.Sign(42, root)
	if !VerifySTH(pub, 42, root, sig) {
		t.Fatal("valid STH signature must verify")
	}
	// Tampered tree_size must fail.
	if VerifySTH(pub, 43, root, sig) {
		t.Fatal("signature must not verify for a different tree_size")
	}
	// Tampered root must fail.
	bad := append([]byte(nil), root...)
	bad[0] ^= 0xff
	if VerifySTH(pub, 42, bad, sig) {
		t.Fatal("signature must not verify for a different root")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/kt/ -run TestSTHSign`
Expected: FAIL — `NewSTHSigner` undefined.

- [ ] **Step 3: Write minimal implementation**

`backend/internal/kt/sth.go`:
```go
package kt

import (
	"crypto/ed25519"
	"encoding/binary"
)

// sthMessage is the canonical signed-over bytes for an STH: "KTSTHv1" || tree_size(u64 BE) || root_hash.
func sthMessage(treeSize int64, rootHash []byte) []byte {
	msg := make([]byte, 0, 7+8+len(rootHash))
	msg = append(msg, []byte("KTSTHv1")...)
	var s [8]byte
	binary.BigEndian.PutUint64(s[:], uint64(treeSize))
	msg = append(msg, s[:]...)
	msg = append(msg, rootHash...)
	return msg
}

// STHSigner signs Signed Tree Heads with the KT Ed25519 private key.
type STHSigner struct{ priv ed25519.PrivateKey }

func NewSTHSigner(priv ed25519.PrivateKey) *STHSigner { return &STHSigner{priv: priv} }

func (s *STHSigner) Sign(treeSize int64, rootHash []byte) []byte {
	return ed25519.Sign(s.priv, sthMessage(treeSize, rootHash))
}

// VerifySTH checks an STH signature against the KT public key.
func VerifySTH(pub ed25519.PublicKey, treeSize int64, rootHash, sig []byte) bool {
	return ed25519.Verify(pub, sthMessage(treeSize, rootHash), sig)
}
```

In `backend/internal/config/config.go` add `KTSigningKey string` to the `Config` struct and load it as a required env var. Add to the struct field list:
```go
	KTSigningKey string
```
In `Load()`, add `KTSigningKey: os.Getenv("KT_SIGNING_KEY"),` to the struct literal and add `"KT_SIGNING_KEY": c.KTSigningKey,` to the required-vars map.

Update the config test that loads defaults to also set the new env var (find `TestLoadFromEnvDefaults` in `config_test.go` and add `t.Setenv("KT_SIGNING_KEY", "DDDD")` alongside the others, so the existing test still passes).

In `backend/cmd/genkeys/main.go`, append KT key generation (after the OPAQUE keys):
```go
	ktPub, ktPriv, _ := ed25519.GenerateKey(rand.Reader)
	fmt.Printf("KT_SIGNING_KEY=%s\n", base64.StdEncoding.EncodeToString(ktPriv))
	fmt.Printf("# KT public key (give to clients): %s\n", base64.StdEncoding.EncodeToString(ktPub))
```
Add imports `crypto/ed25519`, `crypto/rand` to genkeys if not present.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/kt/ -run TestSTHSign && go test ./internal/config/`
Expected: PASS (both).

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/internal/kt/sth.go backend/internal/kt/sth_test.go backend/internal/config backend/cmd/genkeys
git -C /Users/denisurevic/Documents/slack commit -m "feat(backend): KT STH sign/verify (Ed25519), KT key config + genkeys"
```

---

### Task 5: KT repository (leaves + STHs)

**Files:**
- Create: `backend/internal/store/kt.go`, `backend/internal/store/kt_test.go`

- [ ] **Step 1: Write the failing test (integration — Docker; reuses `newTestPool`)**

`backend/internal/store/kt_test.go`:
```go
package store

import (
	"context"
	"testing"
)

func TestKTRepoAppendAndQuery(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	repo := NewKTRepo(pool)
	id := "11111111-1111-1111-1111-111111111111"

	// MaxVersion of an unknown identity is 0.
	if v, _ := repo.MaxVersion(ctx, id); v != 0 {
		t.Fatalf("expected 0, got %d", v)
	}

	idx0, err := repo.AppendLeaf(ctx, id, 1, []byte("set-v1"), []byte("hash0"))
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	idx1, _ := repo.AppendLeaf(ctx, id, 2, []byte("set-v2"), []byte("hash1"))
	if idx0 != 0 || idx1 != 1 {
		t.Fatalf("leaf_index must be monotonic from 0, got %d,%d", idx0, idx1)
	}
	if v, _ := repo.MaxVersion(ctx, id); v != 2 {
		t.Fatalf("expected max version 2, got %d", v)
	}

	// LeafHashes returns hashes in index order.
	hashes, _ := repo.LeafHashes(ctx, 2)
	if len(hashes) != 2 || string(hashes[0]) != "hash0" || string(hashes[1]) != "hash1" {
		t.Fatalf("leaf hashes wrong: %v", hashes)
	}

	// LatestLeafForIdentity returns the highest-version leaf.
	leaf, err := repo.LatestLeafForIdentity(ctx, id)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	if leaf.LeafIndex != 1 || leaf.Version != 2 || string(leaf.DeviceSet) != "set-v2" {
		t.Fatalf("latest leaf wrong: %+v", leaf)
	}
	if _, err := repo.LatestLeafForIdentity(ctx, "22222222-2222-2222-2222-222222222222"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for unknown identity, got %v", err)
	}

	// STH round-trip.
	if err := repo.PutSTH(ctx, 2, []byte("root"), []byte("sig")); err != nil {
		t.Fatalf("put sth: %v", err)
	}
	sth, err := repo.LatestSTH(ctx)
	if err != nil || sth.TreeSize != 2 || string(sth.RootHash) != "root" {
		t.Fatalf("latest sth wrong: %+v err=%v", sth, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/store/ -run TestKTRepo`
Expected: FAIL — `NewKTRepo` undefined.

- [ ] **Step 3: Write minimal implementation**

`backend/internal/store/kt.go`:
```go
package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// KTLeaf is a stored transparency-log leaf.
type KTLeaf struct {
	LeafIndex int64
	Identity  string
	Version   int64
	DeviceSet []byte
	LeafHash  []byte
}

// KTSTH is a stored signed tree head.
type KTSTH struct {
	TreeSize  int64
	RootHash  []byte
	Signature []byte
}

type KTRepo struct{ pool *pgxpool.Pool }

func NewKTRepo(pool *pgxpool.Pool) *KTRepo { return &KTRepo{pool: pool} }

// AppendLeaf inserts a leaf at the next monotonic leaf_index (= current count) and
// returns that index. Must be called under the relay advisory lock (single appender).
func (r *KTRepo) AppendLeaf(ctx context.Context, identity string, version int64, deviceSet, leafHash []byte) (int64, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var idx int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(leaf_index)+1, 0) FROM kt_leaves`).Scan(&idx); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO kt_leaves (leaf_index, identity, version, device_set, leaf_hash) VALUES ($1,$2,$3,$4,$5)`,
		idx, identity, version, deviceSet, leafHash); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return idx, nil
}

func (r *KTRepo) MaxVersion(ctx context.Context, identity string) (int64, error) {
	var v int64
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM kt_leaves WHERE identity=$1`, identity).Scan(&v)
	return v, err
}

// LeafHashes returns the first `size` leaf hashes in index order.
func (r *KTRepo) LeafHashes(ctx context.Context, size int64) ([][]byte, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT leaf_hash FROM kt_leaves WHERE leaf_index < $1 ORDER BY leaf_index`, size)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][]byte
	for rows.Next() {
		var h []byte
		if err := rows.Scan(&h); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (r *KTRepo) LatestLeafForIdentity(ctx context.Context, identity string) (*KTLeaf, error) {
	var l KTLeaf
	err := r.pool.QueryRow(ctx,
		`SELECT leaf_index, identity, version, device_set, leaf_hash FROM kt_leaves
		 WHERE identity=$1 ORDER BY version DESC LIMIT 1`, identity).
		Scan(&l.LeafIndex, &l.Identity, &l.Version, &l.DeviceSet, &l.LeafHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

func (r *KTRepo) PutSTH(ctx context.Context, treeSize int64, rootHash, signature []byte) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO kt_sths (tree_size, root_hash, signature) VALUES ($1,$2,$3)
		 ON CONFLICT (tree_size) DO NOTHING`, treeSize, rootHash, signature)
	return err
}

func (r *KTRepo) LatestSTH(ctx context.Context) (*KTSTH, error) {
	var s KTSTH
	err := r.pool.QueryRow(ctx,
		`SELECT tree_size, root_hash, signature FROM kt_sths ORDER BY tree_size DESC LIMIT 1`).
		Scan(&s.TreeSize, &s.RootHash, &s.Signature)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}
```
(`ErrNotFound` already exists in `internal/store/store.go` from plan 1.)

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/store/ -run TestKTRepo`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/internal/store/kt.go backend/internal/store/kt_test.go
git -C /Users/denisurevic/Documents/slack commit -m "feat(backend): KT repository (leaves + STHs)"
```

---

### Task 6: Relay loop (outbox → leaf → STH) under advisory lock

**Files:**
- Create: `backend/internal/kt/relay.go`, `backend/internal/kt/relay_test.go`

The relay needs the active device set for a user. Add a small read method to the device repo first.

- [ ] **Step 1: Write the failing test (integration — Docker)**

`backend/internal/kt/relay_test.go`:
```go
package kt

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/messenger/backend/internal/platform/postgres"
	"github.com/messenger/backend/internal/store"
	"github.com/transparency-dev/merkle/proof"
	"github.com/transparency-dev/merkle/rfc6962"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	ctr, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("as"), tcpostgres.WithUsername("as"), tcpostgres.WithPassword("as"),
		tcpostgres.BasicWaitStrategies())
	if err != nil { t.Fatalf("pg: %v", err) }
	t.Cleanup(func() { _ = ctr.Terminate(ctx) })
	dsn, _ := ctr.ConnectionString(ctx, "sslmode=disable")
	pool, _ := postgres.Connect(ctx, dsn)
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool); err != nil { t.Fatalf("migrate: %v", err) }
	return pool
}

func TestRelayBuildsLeavesAndSTH(t *testing.T) {
	ctx := context.Background()
	pool := newPool(t)
	users := store.NewUserRepo(pool)
	devices := store.NewDeviceRepo(pool)
	kt := store.NewKTRepo(pool)

	u, _ := users.Create(ctx, "rl@corp", []byte("rec"))
	// Enrolling devices writes device_added events to kt_outbox (same as production).
	d1, _ := devices.Enroll(ctx, u.ID, []byte("keyA"), "laptop")
	devices.Enroll(ctx, u.ID, []byte("keyB"), "phone")

	_, priv, _ := ed25519.GenerateKey(rand.Reader)
	relay := NewRelay(pool, kt, store.NewDeviceRepo(pool), NewSTHSigner(priv))

	// One relay tick drains both outbox events.
	if err := relay.Tick(ctx); err != nil {
		t.Fatalf("tick: %v", err)
	}

	// Two leaves appended; the latest captures the user's current active set.
	leaf, err := kt.LatestLeafForIdentity(ctx, u.ID)
	if err != nil {
		t.Fatalf("latest leaf: %v", err)
	}
	if leaf.Version != 2 {
		t.Fatalf("expected version 2 after two events, got %d", leaf.Version)
	}

	// An STH exists for the current tree size, and the latest leaf's inclusion proof
	// verifies against it.
	sth, err := kt.LatestSTH(ctx)
	if err != nil {
		t.Fatalf("sth: %v", err)
	}
	hashes, _ := kt.LeafHashes(ctx, sth.TreeSize)
	pf, _ := InclusionProof(hashes, uint64(leaf.LeafIndex))
	if err := proof.VerifyInclusion(rfc6962.DefaultHasher, uint64(leaf.LeafIndex), uint64(sth.TreeSize), leaf.LeafHash, pf, sth.RootHash); err != nil {
		t.Fatalf("inclusion against STH failed: %v", err)
	}

	// Idempotency: a second tick with no new events appends nothing.
	if err := relay.Tick(ctx); err != nil {
		t.Fatalf("tick 2: %v", err)
	}
	sth2, _ := kt.LatestSTH(ctx)
	if sth2.TreeSize != sth.TreeSize {
		t.Fatalf("idempotent tick must not grow the tree: %d -> %d", sth.TreeSize, sth2.TreeSize)
	}

	// Revoking d1 produces a new version whose device set no longer contains keyA.
	devices.Revoke(ctx, u.ID, d1.ID)
	relay.Tick(ctx)
	leaf3, _ := kt.LatestLeafForIdentity(ctx, u.ID)
	if leaf3.Version != 3 {
		t.Fatalf("expected version 3 after revoke, got %d", leaf3.Version)
	}
}
```
This test needs `store.DeviceRepo.ActiveSigningKeys(ctx, userID) ([][]byte, error)`. Add it to `internal/store/devices.go`:
```go
// ActiveSigningKeys returns the signing public keys of a user's active devices,
// for the KT relay to snapshot the current device set.
func (r *DeviceRepo) ActiveSigningKeys(ctx context.Context, userID string) ([][]byte, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT signing_public_key FROM devices WHERE user_id=$1 AND status='active' ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][]byte
	for rows.Next() {
		var k []byte
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/kt/ -run TestRelayBuilds`
Expected: FAIL — `NewRelay` / `ActiveSigningKeys` undefined.

- [ ] **Step 3: Write minimal implementation**

Add `ActiveSigningKeys` (above) to `internal/store/devices.go`.

`backend/internal/kt/relay.go`:
```go
package kt

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/messenger/backend/internal/store"
)

// advisoryLockKey is a fixed key so only one node appends to the KT log at a time.
const advisoryLockKey int64 = 0x4B54_4C4F_4700_0001 // "KTLOG\0\0\1"

// deviceReader is the subset of the device repo the relay needs.
type deviceReader interface {
	ActiveSigningKeys(ctx context.Context, userID string) ([][]byte, error)
}

// Relay drains kt_outbox into the Merkle log and issues STHs. Single-appender via
// a Postgres session advisory lock.
type Relay struct {
	pool    *pgxpool.Pool
	kt      *store.KTRepo
	devices deviceReader
	signer  *STHSigner
}

func NewRelay(pool *pgxpool.Pool, kt *store.KTRepo, devices deviceReader, signer *STHSigner) *Relay {
	return &Relay{pool: pool, kt: kt, devices: devices, signer: signer}
}

// Tick processes all currently-unrelayed outbox events, appends leaves, and issues
// a fresh STH if any leaf was added. No-op (and no new STH) when nothing is pending.
func (r *Relay) Tick(ctx context.Context) error {
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	// Single-appender: bail out quietly if another node holds the lock.
	var got bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, advisoryLockKey).Scan(&got); err != nil {
		return err
	}
	if !got {
		return nil
	}
	defer conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, advisoryLockKey)

	rows, err := conn.Query(ctx,
		`SELECT id, user_id FROM kt_outbox WHERE relayed_at IS NULL ORDER BY id`)
	if err != nil {
		return err
	}
	type ev struct {
		id     int64
		userID string
	}
	var events []ev
	for rows.Next() {
		var e ev
		if err := rows.Scan(&e.id, &e.userID); err != nil {
			rows.Close()
			return err
		}
		events = append(events, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(events) == 0 {
		return nil
	}

	for _, e := range events {
		keys, err := r.devices.ActiveSigningKeys(ctx, e.userID)
		if err != nil {
			return err
		}
		maxV, err := r.kt.MaxVersion(ctx, e.userID)
		if err != nil {
			return err
		}
		version := maxV + 1
		canonical := CanonicalLeaf(e.userID, version, keys)
		if _, err := r.kt.AppendLeaf(ctx, e.userID, version, canonical, LeafHash(canonical)); err != nil {
			return err
		}
		if _, err := conn.Exec(ctx, `UPDATE kt_outbox SET relayed_at=now() WHERE id=$1`, e.id); err != nil {
			return err
		}
	}

	// Issue a fresh STH over the new tree size.
	var size int64
	if err := conn.QueryRow(ctx, `SELECT COUNT(*) FROM kt_leaves`).Scan(&size); err != nil {
		return err
	}
	hashes, err := r.kt.LeafHashes(ctx, size)
	if err != nil {
		return err
	}
	root := Root(hashes)
	return r.kt.PutSTH(ctx, size, root, r.signer.Sign(size, root))
}
```
Note: the relay stores the FULL canonical leaf bytes in `device_set` (so lookups can return the exact bytes the client re-hashes). `LeafHash` is computed from the same canonical bytes — consistent with `CanonicalLeaf`/`LeafHash` in Task 2.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/kt/ -run TestRelayBuilds`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/internal/kt/relay.go backend/internal/store/devices.go backend/internal/kt/relay_test.go
git -C /Users/denisurevic/Documents/slack commit -m "feat(backend): KT relay — outbox→leaf→STH under advisory lock"
```

---

## Milestone 3 — Service, HTTP API, wiring, E2E

### Task 7: KT service (lookup + proofs)

**Files:**
- Create: `backend/internal/kt/service.go`, `backend/internal/kt/service_test.go`

- [ ] **Step 1: Write the failing test (integration — Docker)**

`backend/internal/kt/service_test.go`:
```go
package kt

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	"github.com/messenger/backend/internal/store"
	"github.com/transparency-dev/merkle/proof"
	"github.com/transparency-dev/merkle/rfc6962"
)

func TestServiceLookupReturnsVerifiableProof(t *testing.T) {
	ctx := context.Background()
	pool := newPool(t) // from relay_test.go (same package)
	users := store.NewUserRepo(pool)
	devices := store.NewDeviceRepo(pool)
	ktRepo := store.NewKTRepo(pool)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)

	u, _ := users.Create(ctx, "svc@corp", []byte("rec"))
	devices.Enroll(ctx, u.ID, []byte("keyA"), "laptop")
	relay := NewRelay(pool, ktRepo, store.NewDeviceRepo(pool), NewSTHSigner(priv))
	relay.Tick(ctx)

	svc := NewService(ktRepo)

	res, err := svc.Lookup(ctx, u.ID)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	// The returned inclusion proof verifies against the returned STH root.
	if err := proof.VerifyInclusion(rfc6962.DefaultHasher,
		uint64(res.LeafIndex), uint64(res.STH.TreeSize), res.LeafHash, res.AuditPath, res.STH.RootHash); err != nil {
		t.Fatalf("returned proof must verify: %v", err)
	}
	// And the STH signature verifies with the KT public key.
	if !VerifySTH(pub, res.STH.TreeSize, res.STH.RootHash, res.STH.Signature) {
		t.Fatal("returned STH signature must verify")
	}

	if _, err := svc.Lookup(ctx, "99999999-9999-9999-9999-999999999999"); err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/kt/ -run TestServiceLookup`
Expected: FAIL — `NewService` undefined.

- [ ] **Step 3: Write minimal implementation**

`backend/internal/kt/service.go`:
```go
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
func (s *Service) Lookup(ctx context.Context, identity string) (*LookupResult, error) {
	leaf, err := s.kt.LatestLeafForIdentity(ctx, identity)
	if err != nil {
		return nil, err
	}
	sth, err := s.kt.LatestSTH(ctx)
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/kt/ -run TestServiceLookup`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/internal/kt/service.go backend/internal/kt/service_test.go
git -C /Users/denisurevic/Documents/slack commit -m "feat(backend): KT service — verifiable lookup + proof accessors"
```

---

### Task 8: KT HTTP handlers

**Files:**
- Create: `backend/internal/httpapi/kt_handlers.go`, `backend/internal/httpapi/kt_handlers_test.go`
- Modify: `backend/internal/httpapi/router.go` (NewRouterFull gains `ktSvc *kt.Service` + KT public key; register routes), `backend/internal/httpapi/dto.go`

- [ ] **Step 1: Write the failing test (integration — Docker)**

`backend/internal/httpapi/kt_handlers_test.go`:
```go
package httpapi

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/messenger/backend/internal/kt"
	"github.com/messenger/backend/internal/store"
)

func TestKTKeyLookupEndpoint(t *testing.T) {
	h, env := newKTServer(t) // helper: full server + KT relay run once; see note
	token := registerAndLogin(t, h, "kt@corp", "kt-pass")
	deviceID := enrollDevice(t, h, token)

	// Drive a relay tick so the enrolled device is in the log.
	if err := env.relay.Tick(context.Background()); err != nil {
		t.Fatalf("relay tick: %v", err)
	}

	// The user's own user_id is the KT identity; fetch it from the session.
	userID := sessionUserID(t, h, token)

	rec := serve(h, httptestNewGet("/kt/key/"+userID, token))
	if rec.Code != http.StatusOK {
		t.Fatalf("kt key: %d %s", rec.Code, rec.Body)
	}
	var resp struct {
		Version   int64    `json:"version"`
		LeafIndex int64    `json:"leaf_index"`
		AuditPath []string `json:"audit_path"`
		STH       struct {
			TreeSize  int64  `json:"tree_size"`
			RootHash  string `json:"root_hash"`
			Signature string `json:"signature"`
		} `json:"sth"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Version < 1 || resp.STH.TreeSize < 1 {
		t.Fatalf("unexpected kt key response: %s", rec.Body)
	}
	_ = deviceID

	// /kt/pubkey returns a usable Ed25519 key.
	rec2 := serve(h, httptestNewGet("/kt/pubkey", token))
	var pk struct {
		KTPublicKey string `json:"kt_public_key"`
	}
	json.Unmarshal(rec2.Body.Bytes(), &pk)
	raw, _ := base64.StdEncoding.DecodeString(pk.KTPublicKey)
	if len(raw) != ed25519.PublicKeySize {
		t.Fatalf("kt pubkey wrong size: %d", len(raw))
	}
}
```
NOTE — helpers to write: `newKTServer(t) (http.Handler, *ktEnv)` where `ktEnv` exposes `relay *kt.Relay`. Build it like `newFullServer` but also: generate an Ed25519 KT keypair, build `store.NewKTRepo(pool)`, `kt.NewService(ktRepo)`, `kt.NewSTHSigner(priv)`, `kt.NewRelay(pool, ktRepo, store.NewDeviceRepo(pool), signer)`, and pass `ktSvc` + the KT public key to `NewRouterFull`. Also write `sessionUserID(t,h,token)` — GET `/auth/session` and return its `user_id`. Reuse `registerAndLogin`, `enrollDevice`, `serve`, `httptestNewGet`. Report the exact wiring. Also update the existing `newFullServer`/`newRosterServer` builders to pass the two new `NewRouterFull` args (a KT service + KT pubkey) so they compile — they can construct a throwaway KT service/key.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/httpapi/ -run TestKTKeyLookup`
Expected: FAIL — KT routes/handlers + `NewRouterFull` arg missing.

- [ ] **Step 3: Write minimal implementation**

`backend/internal/httpapi/kt_handlers.go`:
```go
package httpapi

import (
	"crypto/ed25519"
	"encoding/base64"
	"net/http"
	"strconv"

	"github.com/messenger/backend/internal/kt"
	"github.com/messenger/backend/internal/store"
)

type ktHandlers struct {
	svc    *kt.Service
	pubKey ed25519.PublicKey
}

func b64slice(in [][]byte) []string {
	out := make([]string, len(in))
	for i, b := range in {
		out[i] = base64.StdEncoding.EncodeToString(b)
	}
	return out
}

func (h *ktHandlers) pubkey(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"kt_public_key": base64.StdEncoding.EncodeToString(h.pubKey),
	})
}

func (h *ktHandlers) sth(w http.ResponseWriter, r *http.Request) {
	sth, err := h.svc.LatestSTH(r.Context())
	if err != nil {
		writeError(w, http.StatusNotFound, "no_sth", "no STH yet")
		return
	}
	writeJSON(w, http.StatusOK, sthDTO(sth))
}

func (h *ktHandlers) key(w http.ResponseWriter, r *http.Request) {
	identity := r.PathValue("identity")
	res, err := h.svc.Lookup(r.Context(), identity)
	if err == store.ErrNotFound {
		writeError(w, http.StatusNotFound, "not_found", "no key record for identity")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, ktKeyResp{
		LeafIndex: res.LeafIndex,
		Version:   res.Version,
		DeviceSet: base64.StdEncoding.EncodeToString(res.DeviceSet),
		AuditPath: b64slice(res.AuditPath),
		STH:       sthDTO(&res.STH),
	})
}

func (h *ktHandlers) inclusion(w http.ResponseWriter, r *http.Request) {
	leafIndex, err1 := strconv.ParseInt(r.URL.Query().Get("leaf_index"), 10, 64)
	treeSize, err2 := strconv.ParseInt(r.URL.Query().Get("tree_size"), 10, 64)
	if err1 != nil || err2 != nil || leafIndex < 0 || treeSize <= leafIndex {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid leaf_index/tree_size")
		return
	}
	path, err := h.svc.Inclusion(r.Context(), leafIndex, treeSize)
	if err != nil {
		writeError(w, http.StatusBadRequest, "proof_failed", "could not build inclusion proof")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"leaf_index": leafIndex, "tree_size": treeSize, "audit_path": b64slice(path)})
}

func (h *ktHandlers) consistency(w http.ResponseWriter, r *http.Request) {
	from, err1 := strconv.ParseInt(r.URL.Query().Get("from"), 10, 64)
	to, err2 := strconv.ParseInt(r.URL.Query().Get("to"), 10, 64)
	if err1 != nil || err2 != nil || from < 0 || to < from {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid from/to")
		return
	}
	pf, err := h.svc.Consistency(r.Context(), from, to)
	if err != nil {
		writeError(w, http.StatusBadRequest, "proof_failed", "could not build consistency proof")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"from": from, "to": to, "proof": b64slice(pf)})
}
```
Append to `backend/internal/httpapi/dto.go`:
```go
type sthResp struct {
	TreeSize  int64  `json:"tree_size"`
	RootHash  string `json:"root_hash"`
	Signature string `json:"signature"`
}
type ktKeyResp struct {
	LeafIndex int64    `json:"leaf_index"`
	Version   int64    `json:"version"`
	DeviceSet string   `json:"device_set"`
	AuditPath []string `json:"audit_path"`
	STH       sthResp  `json:"sth"`
}
```
Add a helper (in kt_handlers.go) bridging `store.KTSTH`→`sthResp` using base64:
```go
func sthDTO(s *store.KTSTH) sthResp {
	return sthResp{
		TreeSize:  s.TreeSize,
		RootHash:  base64.StdEncoding.EncodeToString(s.RootHash),
		Signature: base64.StdEncoding.EncodeToString(s.Signature),
	}
}
```
In `router.go`, add params `ktSvc *kt.Service, ktPub ed25519.PublicKey` to `NewRouterFull`, build `kh := &ktHandlers{svc: ktSvc, pubKey: ktPub}`, and register (all under `auth`):
```go
mux.Handle("GET /kt/pubkey", auth(http.HandlerFunc(kh.pubkey)))
mux.Handle("GET /kt/sth", auth(http.HandlerFunc(kh.sth)))
mux.Handle("GET /kt/key/{identity}", auth(http.HandlerFunc(kh.key)))
mux.Handle("GET /kt/proof/inclusion", auth(http.HandlerFunc(kh.inclusion)))
mux.Handle("GET /kt/proof/consistency", auth(http.HandlerFunc(kh.consistency)))
```
Add imports (`crypto/ed25519`, `github.com/messenger/backend/internal/kt`). Update every caller of `NewRouterFull` (main.go, and test builders `newFullServer`/`newRosterServer`/`newKTServer`) to pass the two new args.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./internal/httpapi/ -run TestKTKeyLookup`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/internal/httpapi
git -C /Users/denisurevic/Documents/slack commit -m "feat(backend): KT HTTP API (pubkey, sth, key lookup, inclusion/consistency proofs)"
```

---

### Task 9: Wire relay loop + KT routes into main.go

**Files:**
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Write the failing test**

Add `backend/internal/httpapi/router_kt_test.go`:
```go
package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKTRouteRegisteredAndProtected(t *testing.T) {
	h, _ := newKTServer(t)
	req := httptest.NewRequest(http.MethodGet, "/kt/sth", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Fatal("kt route must be registered (got 404)")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails/passes**

Run: `cd backend && go test ./internal/httpapi/ -run TestKTRouteRegistered`
Expected: PASS if Task 8 registered the routes (the `/kt/sth` route exists and is auth-protected). If 404, fix `NewRouterFull`.

- [ ] **Step 3: Wire main.go**

In `backend/cmd/server/main.go`, after the existing service construction and before building the handler, add KT wiring (decode the KT key, build service + relay, start a background relay ticker):
```go
	// Key Transparency: log signer, service, and background relay.
	ktPrivBytes, err := base64.StdEncoding.DecodeString(cfg.KTSigningKey)
	if err != nil || len(ktPrivBytes) != ed25519.PrivateKeySize {
		log.Fatalf("KT_SIGNING_KEY invalid: must be base64 of a %d-byte ed25519 private key", ed25519.PrivateKeySize)
	}
	ktPriv := ed25519.PrivateKey(ktPrivBytes)
	ktPub := ktPriv.Public().(ed25519.PublicKey)
	ktRepo := store.NewKTRepo(pool)
	ktSvc := kt.NewService(ktRepo)
	ktRelay := kt.NewRelay(pool, ktRepo, store.NewDeviceRepo(pool), kt.NewSTHSigner(ktPriv))

	relayCtx, stopRelay := context.WithCancel(context.Background())
	defer stopRelay()
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-relayCtx.Done():
				return
			case <-ticker.C:
				if err := ktRelay.Tick(relayCtx); err != nil {
					log.Printf("kt relay tick: %v", err)
				}
			}
		}
	}()
```
Pass `ktSvc, ktPub` to `NewRouterFull(...)` (now the final args). Add imports `crypto/ed25519` and `github.com/messenger/backend/internal/kt` (`base64`, `context`, `time`, `log`, `store` already present). On shutdown, `stopRelay()` is called via defer; ensure it runs before/around `srv.Shutdown` (place `stopRelay()` explicitly before the final shutdown if defer ordering matters — calling it twice is harmless).

- [ ] **Step 4: Verify the whole backend builds + tests pass**

Run:
```bash
cd backend && go build ./... && go vet ./...
cd backend && go test ./...
```
Expected: all green (Docker running; integration suites slow). Report the per-package summary.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/cmd/server/main.go backend/internal/httpapi/router_kt_test.go
git -C /Users/denisurevic/Documents/slack commit -m "feat(backend): start KT relay loop + mount KT routes"
```

---

### Task 10: End-to-end — enroll → relay → verifiable lookup

**Files:**
- Create: `backend/internal/httpapi/kt_e2e_test.go`

- [ ] **Step 1: Write the failing test (integration — Docker)**

`backend/internal/httpapi/kt_e2e_test.go`:
```go
package httpapi

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/messenger/backend/internal/kt"
	"github.com/transparency-dev/merkle/proof"
	"github.com/transparency-dev/merkle/rfc6962"
)

// Full chain: AS enroll → kt_outbox → relay → GET /kt/key returns a record whose
// inclusion proof verifies against the signed STH, and the STH verifies with the
// KT public key fetched from /kt/pubkey.
func TestKTEndToEndVerifiable(t *testing.T) {
	h, env := newKTServer(t)
	token := registerAndLogin(t, h, "e2e@corp", "e2e-pass")
	enrollDevice(t, h, token)
	if err := env.relay.Tick(context.Background()); err != nil {
		t.Fatalf("relay: %v", err)
	}
	userID := sessionUserID(t, h, token)

	// Fetch KT public key.
	var pk struct{ KTPublicKey string `json:"kt_public_key"` }
	json.Unmarshal(serve(h, httptestNewGet("/kt/pubkey", token)).Body.Bytes(), &pk)
	pubRaw, _ := base64.StdEncoding.DecodeString(pk.KTPublicKey)

	// Look up the key record.
	rec := serve(h, httptestNewGet("/kt/key/"+userID, token))
	if rec.Code != http.StatusOK {
		t.Fatalf("kt key: %d %s", rec.Code, rec.Body)
	}
	var resp ktKeyResp
	json.Unmarshal(rec.Body.Bytes(), &resp)

	// Reconstruct the leaf hash from the returned device_set and verify inclusion.
	deviceSet, _ := base64.StdEncoding.DecodeString(resp.DeviceSet)
	leafHash := kt.LeafHash(deviceSet)
	rootHash, _ := base64.StdEncoding.DecodeString(resp.STH.RootHash)
	sig, _ := base64.StdEncoding.DecodeString(resp.STH.Signature)
	auditPath := make([][]byte, len(resp.AuditPath))
	for i, s := range resp.AuditPath {
		auditPath[i], _ = base64.StdEncoding.DecodeString(s)
	}

	if err := proof.VerifyInclusion(rfc6962.DefaultHasher,
		uint64(resp.LeafIndex), uint64(resp.STH.TreeSize), leafHash, auditPath, rootHash); err != nil {
		t.Fatalf("client-side inclusion verification failed: %v", err)
	}
	if !kt.VerifySTH(ed25519.PublicKey(pubRaw), resp.STH.TreeSize, rootHash, sig) {
		t.Fatal("client-side STH signature verification failed")
	}
}
```
This proves the contract a real KT client will implement: re-hash the returned `device_set`, verify inclusion against the STH root, verify the STH signature with the published public key.

- [ ] **Step 2: Run test to verify it fails (then passes)**

Run: `cd backend && go test ./internal/httpapi/ -run TestKTEndToEnd`
Expected: PASS (all pieces exist after Tasks 6–9). If the reconstructed `leafHash` doesn't match (inclusion fails), the bug is a mismatch between what the relay stores in `device_set` and what `CanonicalLeaf` produces — they MUST be the same canonical bytes. Confirm the relay stores `CanonicalLeaf(...)` output as `device_set` (Task 6 does). Diagnose and fix; do not weaken the verification.

- [ ] **Step 3: (no new impl expected)** Fix any contract mismatch surfaced.

- [ ] **Step 4: Run the full backend suite**

Run: `cd backend && go test ./...`
Expected: all green.

- [ ] **Step 5: Commit**

```bash
git -C /Users/denisurevic/Documents/slack add backend/internal/httpapi/kt_e2e_test.go
git -C /Users/denisurevic/Documents/slack commit -m "test(backend): KT end-to-end — enroll→relay→client-verifiable key lookup"
```

---

## Self-Review

**1. Spec coverage:**
- Append-only RFC6962 Merkle log over Postgres → Tasks 1, 3, 5 ✓
- Leaf = canonical {identity, version, device_set} + RFC6962 leaf hash → Task 2 ✓
- Relay consumes `kt_outbox`, snapshots active device set, new version, marks relayed → Task 6 ✓
- Signed STH (Ed25519) per relay tick → Tasks 4, 6 ✓
- Inclusion + consistency proofs (verified via library) → Tasks 3, 7 ✓
- HTTP API: pubkey, sth, key lookup, inclusion, consistency → Task 8 ✓
- Single-appender via Postgres advisory lock → Task 6 ✓
- Verifiable key lookup (latest leaf + inclusion + STH) → Tasks 7, 8, 10 ✓
- Monotonic per-identity version; empty device set valid → Tasks 5, 6 (relay computes maxV+1; empty keys hash fine per Task 2 test) ✓
- KT key from env + genkeys + /kt/pubkey → Tasks 4, 8 ✓
- Relay started in main.go (background ticker) → Task 9 ✓
- E2E through AS proving the client contract → Task 10 ✓
- Out of scope (VRF/CONIKS, auditor/gossip, KT client) → correctly excluded; formats pinned as contract.

**2. Placeholder scan:** Task 8 flags helper wiring (`newKTServer`, `sessionUserID`, and updating `newFullServer`/`newRosterServer` to the new `NewRouterFull` signature) with concrete guidance — not silent gaps. No "TBD"/"handle errors" placeholders; all production steps contain complete code.

**3. Type consistency:** `CanonicalLeaf(identity string, version int64, deviceSet [][]byte) []byte` and `LeafHash([]byte) []byte` are used identically in Task 6 (relay) and Task 10 (client-side re-hash). `Root`/`InclusionProof`/`ConsistencyProof` signatures match between Task 3, the service (Task 7), and tests. `store.KTRepo` methods (`AppendLeaf`, `LeafHashes`, `LatestLeafForIdentity`, `MaxVersion`, `PutSTH`, `LatestSTH`) and types (`KTLeaf`, `KTSTH`) match across Tasks 5–8. `NewSTHSigner(priv).Sign(treeSize, root)` / `VerifySTH(pub, treeSize, root, sig)` consistent across Tasks 4, 6, 7, 10. `NewRouterFull` gains `ktSvc *kt.Service, ktPub ed25519.PublicKey` (Task 8); Task 9 and all test builders updated to pass them. The `deviceReader` interface in Task 6 is satisfied by `store.DeviceRepo.ActiveSigningKeys` added in the same task.

**4. API-version risk (call out, not placeholder):** `transparency-dev/merkle` v0.0.2 symbols flagged inline: `rfc6962.DefaultHasher` (Task 2), `testonly.New`/`Tree.Append`/`Hash`/`InclusionProof`/`ConsistencyProof` (Task 3), `proof.VerifyInclusion`/`VerifyConsistency` (used in tests — confirmed from source). The `testonly` package is the module's reference in-memory tree; using it for proof generation is a conscious choice for this plan (documented in Task 3), with a persistent node store as the future optimization. `pg_try_advisory_lock`/`pg_advisory_unlock` are standard Postgres.
