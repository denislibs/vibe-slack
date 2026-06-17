package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"

	xopaque "github.com/bytemare/opaque"
)

// genkeys prints OPAQUE server key material as base64, set as env vars shared
// across all instances. Run once per deployment.
func main() {
	cfg := xopaque.DefaultConfiguration()
	sk, pk := cfg.KeyGen()
	seed := cfg.GenerateOPRFSeed()
	enc := base64.StdEncoding.EncodeToString
	fmt.Printf("OPAQUE_SERVER_PRIVATE_KEY=%s\n", enc(sk.Encode()))
	fmt.Printf("OPAQUE_SERVER_PUBLIC_KEY=%s\n", enc(pk.Encode()))
	fmt.Printf("OPAQUE_OPRF_SEED=%s\n", enc(seed))

	ktPub, ktPriv, _ := ed25519.GenerateKey(rand.Reader)
	fmt.Printf("KT_SIGNING_KEY=%s\n", base64.StdEncoding.EncodeToString(ktPriv))
	fmt.Printf("# KT public key (give to clients): %s\n", base64.StdEncoding.EncodeToString(ktPub))
}
