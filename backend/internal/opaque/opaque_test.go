package opaque

import (
	"bytes"
	"testing"

	xopaque "github.com/bytemare/opaque"
)

func testKeyMaterial(t *testing.T) (priv, pub, seed []byte) {
	t.Helper()
	cfg := xopaque.DefaultConfiguration()
	sk, pk := cfg.KeyGen()
	return sk.Encode(), pk.Encode(), cfg.GenerateOPRFSeed()
}

func TestRegisterThenLoginSucceeds(t *testing.T) {
	priv, pub, seed := testKeyMaterial(t)
	srv, err := NewServer(priv, pub, seed, []byte("messenger-as"))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	credID := []byte("user-credential-id")
	password := []byte("correct horse battery staple")

	cfg := xopaque.DefaultConfiguration()
	client, _ := cfg.Client()
	regReq, _ := client.RegistrationInit(password)

	regRespBytes, err := srv.RegistrationResponse(regReq.Serialize(), credID)
	if err != nil {
		t.Fatalf("RegistrationResponse: %v", err)
	}
	regResp, err := client.Deserialize.RegistrationResponse(regRespBytes)
	if err != nil {
		t.Fatalf("client deserialize reg resp: %v", err)
	}
	record, _, err := client.RegistrationFinalize(regResp, nil, []byte("messenger-as"))
	if err != nil {
		t.Fatalf("RegistrationFinalize: %v", err)
	}
	recordBytes := record.Serialize()

	client2, _ := cfg.Client()
	ke1, _ := client2.GenerateKE1(password)
	ke2Bytes, loginState, err := srv.LoginStart(ke1.Serialize(), credID, recordBytes)
	if err != nil {
		t.Fatalf("LoginStart: %v", err)
	}
	ke2, err := client2.Deserialize.KE2(ke2Bytes)
	if err != nil {
		t.Fatalf("client deserialize ke2: %v", err)
	}
	ke3, clientSession, _, err := client2.GenerateKE3(ke2, nil, []byte("messenger-as"))
	if err != nil {
		t.Fatalf("GenerateKE3: %v", err)
	}
	serverSession, err := srv.LoginFinish(ke3.Serialize(), loginState)
	if err != nil {
		t.Fatalf("LoginFinish: %v", err)
	}
	if !bytes.Equal(clientSession, serverSession) {
		t.Fatal("client and server session secrets must match on success")
	}
}

func TestLoginWrongPasswordFails(t *testing.T) {
	priv, pub, seed := testKeyMaterial(t)
	srv, _ := NewServer(priv, pub, seed, []byte("messenger-as"))
	credID := []byte("user-credential-id")

	cfg := xopaque.DefaultConfiguration()
	client, _ := cfg.Client()
	regReq, _ := client.RegistrationInit([]byte("right-password"))
	regRespBytes, _ := srv.RegistrationResponse(regReq.Serialize(), credID)
	regResp, _ := client.Deserialize.RegistrationResponse(regRespBytes)
	record, _, _ := client.RegistrationFinalize(regResp, nil, []byte("messenger-as"))

	client2, _ := cfg.Client()
	ke1, _ := client2.GenerateKE1([]byte("WRONG-password"))
	ke2Bytes, loginState, err := srv.LoginStart(ke1.Serialize(), credID, record.Serialize())
	if err != nil {
		t.Fatalf("LoginStart: %v", err)
	}
	ke2, _ := client2.Deserialize.KE2(ke2Bytes)
	ke3, _, _, err := client2.GenerateKE3(ke2, nil, []byte("messenger-as"))
	if err != nil {
		return // some configs surface failure here; acceptable
	}
	if _, err := srv.LoginFinish(ke3.Serialize(), loginState); err == nil {
		t.Fatal("expected LoginFinish to fail on wrong password")
	}
}

func TestFakeRecordIsDeterministicPerCredID(t *testing.T) {
	priv, pub, seed := testKeyMaterial(t)
	srv, _ := NewServer(priv, pub, seed, []byte("messenger-as"))
	a, err := srv.FakeRecord([]byte("unknown@corp"))
	if err != nil {
		t.Fatalf("FakeRecord: %v", err)
	}
	b, _ := srv.FakeRecord([]byte("unknown@corp"))
	if !bytes.Equal(a, b) {
		t.Fatal("fake record must be stable for the same credential id (anti-enumeration)")
	}
}
