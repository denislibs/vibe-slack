package main

import (
	"bytes"
	"testing"

	xopaque "github.com/bytemare/opaque"
)

// A bytemare Server with DefaultConfiguration + fresh key material, mirroring the AS.
func testServer(t *testing.T) *xopaque.Server {
	t.Helper()
	cfg := xopaque.DefaultConfiguration()
	sk, pk := cfg.KeyGen()
	srv, err := cfg.Server()
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	priv := cfg.AKE.Group().NewScalar()
	if err := priv.Decode(sk.Encode()); err != nil {
		t.Fatalf("decode sk: %v", err)
	}
	skm := &xopaque.ServerKeyMaterial{
		PrivateKey: priv, PublicKeyBytes: pk.Encode(),
		OPRFGlobalSeed: cfg.GenerateOPRFSeed(), Identity: []byte("messenger-as"),
	}
	if err := srv.SetKeyMaterial(skm); err != nil {
		t.Fatalf("set key material: %v", err)
	}
	return srv
}

func TestClientInteropRegisterThenLogin(t *testing.T) {
	srv := testServer(t)
	credID := []byte("cred:alice@corp")
	serverID := []byte("messenger-as")
	password := []byte("correct horse battery staple")

	regFlow, reqBytes, err := RegInit(password)
	if err != nil {
		t.Fatalf("RegInit: %v", err)
	}
	req, err := srv.Deserialize.RegistrationRequest(reqBytes)
	if err != nil {
		t.Fatalf("server deserialize req: %v", err)
	}
	resp, err := srv.RegistrationResponse(req, credID, nil)
	if err != nil {
		t.Fatalf("server RegistrationResponse: %v", err)
	}
	recordBytes, _, err := RegFinalize(regFlow, resp.Serialize(), serverID)
	if err != nil {
		t.Fatalf("RegFinalize: %v", err)
	}

	loginFlow, ke1Bytes, err := LoginKE1(password)
	if err != nil {
		t.Fatalf("LoginKE1: %v", err)
	}
	ke1, _ := srv.Deserialize.KE1(ke1Bytes)
	rec, _ := srv.Deserialize.RegistrationRecord(recordBytes)
	cr := &xopaque.ClientRecord{RegistrationRecord: rec, CredentialIdentifier: credID}
	ke2, serverOut, err := srv.GenerateKE2(ke1, cr)
	if err != nil {
		t.Fatalf("server GenerateKE2: %v", err)
	}
	ke3Bytes, clientSession, err := LoginKE3(loginFlow, ke2.Serialize(), serverID)
	if err != nil {
		t.Fatalf("LoginKE3: %v", err)
	}
	ke3, _ := srv.Deserialize.KE3(ke3Bytes)
	if err := srv.LoginFinish(ke3, serverOut.ClientMAC); err != nil {
		t.Fatalf("server LoginFinish: %v", err)
	}
	if !bytes.Equal(clientSession, serverOut.SessionSecret) {
		t.Fatal("client and server session keys must match — interop proven")
	}
}

func TestWrongPasswordFails(t *testing.T) {
	srv := testServer(t)
	credID := []byte("cred:bob@corp")
	serverID := []byte("messenger-as")

	regFlow, reqBytes, _ := RegInit([]byte("right"))
	req, _ := srv.Deserialize.RegistrationRequest(reqBytes)
	resp, _ := srv.RegistrationResponse(req, credID, nil)
	recordBytes, _, _ := RegFinalize(regFlow, resp.Serialize(), serverID)

	loginFlow, ke1Bytes, _ := LoginKE1([]byte("WRONG"))
	ke1, _ := srv.Deserialize.KE1(ke1Bytes)
	rec, _ := srv.Deserialize.RegistrationRecord(recordBytes)
	ke2, serverOut, _ := srv.GenerateKE2(ke1, &xopaque.ClientRecord{RegistrationRecord: rec, CredentialIdentifier: credID})
	ke3Bytes, _, err := LoginKE3(loginFlow, ke2.Serialize(), serverID)
	if err != nil {
		return // client-side failure acceptable
	}
	ke3, _ := srv.Deserialize.KE3(ke3Bytes)
	if srv.LoginFinish(ke3, serverOut.ClientMAC) == nil {
		t.Fatal("wrong password must fail at LoginFinish")
	}
}
