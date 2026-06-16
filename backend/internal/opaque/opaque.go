package opaque

import (
	"crypto/hmac"
	"crypto/sha512"
	"encoding/binary"
	"fmt"

	xopaque "github.com/bytemare/opaque"
)

// Server wraps the bytemare/opaque server with this service's fixed configuration
// and persistent key material. The wire protocol it speaks is the contract the
// OPAQUE client (separate plan) implements against.
type Server struct {
	cfg      *xopaque.Configuration
	identity []byte
	skm      *xopaque.ServerKeyMaterial
}

// NewServer builds a server from encoded key material (see cmd/genkeys).
func NewServer(privEncoded, pubEncoded, oprfSeed, identity []byte) (*Server, error) {
	cfg := xopaque.DefaultConfiguration()
	sk := cfg.AKE.Group().NewScalar()
	if err := sk.Decode(privEncoded); err != nil {
		return nil, fmt.Errorf("decode server private key: %w", err)
	}
	skm := &xopaque.ServerKeyMaterial{
		PrivateKey:     sk,
		PublicKeyBytes: pubEncoded,
		OPRFGlobalSeed: oprfSeed,
		Identity:       identity,
	}
	s := &Server{cfg: cfg, identity: identity, skm: skm}
	if _, err := s.newServer(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) newServer() (*xopaque.Server, error) {
	srv, err := s.cfg.Server()
	if err != nil {
		return nil, err
	}
	if err := srv.SetKeyMaterial(s.skm); err != nil {
		return nil, err
	}
	return srv, nil
}

func (s *Server) RegistrationResponse(reqBytes, credID []byte) ([]byte, error) {
	srv, err := s.newServer()
	if err != nil {
		return nil, err
	}
	req, err := srv.Deserialize.RegistrationRequest(reqBytes)
	if err != nil {
		return nil, fmt.Errorf("deserialize registration request: %w", err)
	}
	resp, err := srv.RegistrationResponse(req, credID, nil)
	if err != nil {
		return nil, err
	}
	return resp.Serialize(), nil
}

// LoginState is the short-lived per-login secret the server keeps between
// LoginStart (GenerateKE2) and LoginFinish. Stored in Redis keyed by login_id.
type LoginState struct {
	ExpectedClientMAC []byte
	SessionSecret     []byte
}

func (s *Server) LoginStart(ke1Bytes, credID, recordBytes []byte) ([]byte, *LoginState, error) {
	srv, err := s.newServer()
	if err != nil {
		return nil, nil, err
	}
	ke1, err := srv.Deserialize.KE1(ke1Bytes)
	if err != nil {
		return nil, nil, fmt.Errorf("deserialize ke1: %w", err)
	}
	rec, err := srv.Deserialize.RegistrationRecord(recordBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("deserialize record: %w", err)
	}
	cr := &xopaque.ClientRecord{
		RegistrationRecord:   rec,
		CredentialIdentifier: credID,
		ClientIdentity:       nil,
	}
	ke2, out, err := srv.GenerateKE2(ke1, cr)
	if err != nil {
		return nil, nil, err
	}
	return ke2.Serialize(), &LoginState{
		ExpectedClientMAC: out.ClientMAC,
		SessionSecret:     out.SessionSecret,
	}, nil
}

func (s *Server) LoginFinish(ke3Bytes []byte, st *LoginState) ([]byte, error) {
	srv, err := s.newServer()
	if err != nil {
		return nil, err
	}
	ke3, err := srv.Deserialize.KE3(ke3Bytes)
	if err != nil {
		return nil, fmt.Errorf("deserialize ke3: %w", err)
	}
	if err := srv.LoginFinish(ke3, st.ExpectedClientMAC); err != nil {
		return nil, err
	}
	return st.SessionSecret, nil
}

// fakeRecordDST domain-separates the fake-record KDF from any other use of the
// server's OPRF seed.
var fakeRecordDST = []byte("messenger-opaque-fake-record-v1")

// FakeRecord returns a deterministic fake registration record for an unknown
// credential id, so login attempts on non-existent accounts are indistinguishable
// from real ones (anti-enumeration).
//
// The library's Configuration.GetFakeRecord generates a fresh random record on
// every call (verified against bytemare/opaque v0.18.0 opaque.go:212-232), which
// would let an attacker distinguish unknown accounts by observing that the
// "registration record" differs between two login attempts on the same identity.
// To get a record that is both correctly shaped for the active configuration and
// stable per credential id, we take one correctly-sized record from the library
// and deterministically overwrite its fields with values derived from the
// server's secret OPRF seed and the credential id.
func (s *Server) FakeRecord(credID []byte) ([]byte, error) {
	rec, err := s.cfg.GetFakeRecord(credID)
	if err != nil {
		return nil, err
	}
	rr := rec.RegistrationRecord

	// Deterministic, secret-keyed expansion: HMAC-SHA512(seed, DST || credID || counter).
	expand := func(label byte, n int) []byte {
		out := make([]byte, 0, n)
		var counter uint32
		for len(out) < n {
			mac := hmac.New(sha512.New, s.skm.OPRFGlobalSeed)
			mac.Write(fakeRecordDST)
			mac.Write([]byte{label})
			var c [4]byte
			binary.BigEndian.PutUint32(c[:], counter)
			mac.Write(c[:])
			mac.Write(credID)
			out = mac.Sum(out)
			counter++
		}
		return out[:n]
	}

	group := s.cfg.AKE.Group()
	scalar := group.HashToScalar(expand(0x00, group.ScalarLength()), fakeRecordDST)
	rr.ClientPublicKey = group.Base().Multiply(scalar)
	rr.MaskingKey = expand(0x01, len(rr.MaskingKey))
	rr.Envelope = expand(0x02, len(rr.Envelope))

	return rr.Serialize(), nil
}
