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
	if VerifySTH(pub, 43, root, sig) {
		t.Fatal("signature must not verify for a different tree_size")
	}
	bad := append([]byte(nil), root...)
	bad[0] ^= 0xff
	if VerifySTH(pub, 42, bad, sig) {
		t.Fatal("signature must not verify for a different root")
	}
}
