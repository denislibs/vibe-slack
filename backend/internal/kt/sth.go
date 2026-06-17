package kt

import (
	"crypto/ed25519"
	"encoding/binary"
)

// sthMessage is the canonical signed-over bytes: "KTSTHv1" || tree_size(u64 BE) || root_hash.
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
