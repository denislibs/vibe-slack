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
// KT public key fetched from /kt/pubkey. This is the contract a KT client implements.
func TestKTEndToEndVerifiable(t *testing.T) {
	h, env := newKTServer(t)
	token := registerAndLogin(t, h, "e2e@corp", "e2e-pass")
	enrollDevice(t, h, token)
	if err := env.relay.Tick(context.Background()); err != nil {
		t.Fatalf("relay: %v", err)
	}
	userID := sessionUserID(t, h, token)

	// KT public key.
	var pk struct {
		KTPublicKey string `json:"kt_public_key"`
	}
	json.Unmarshal(serve(h, httptestNewGet("/kt/pubkey", token)).Body.Bytes(), &pk)
	pubRaw, _ := base64.StdEncoding.DecodeString(pk.KTPublicKey)
	if len(pubRaw) != ed25519.PublicKeySize {
		t.Fatalf("kt pubkey wrong size: %d", len(pubRaw))
	}

	// Key record.
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
