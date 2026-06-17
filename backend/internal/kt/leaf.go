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
