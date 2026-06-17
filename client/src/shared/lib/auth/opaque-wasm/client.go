package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"

	xopaque "github.com/bytemare/opaque"
)

// State for in-flight OPAQUE flows. The bytemare Client is stateful between
// Init→Finalize and KE1→KE3, so we keep the instance keyed by a flow id.
var (
	mu      sync.Mutex
	clients = map[string]*xopaque.Client{}
)

func newFlowID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func putClient(id string, c *xopaque.Client) { mu.Lock(); clients[id] = c; mu.Unlock() }
func takeClient(id string) (*xopaque.Client, bool) {
	mu.Lock()
	c, ok := clients[id]
	delete(clients, id)
	mu.Unlock()
	return c, ok
}

func newClient() (*xopaque.Client, error) { return xopaque.DefaultConfiguration().Client() }

func RegInit(password []byte) (flowID string, request []byte, err error) {
	c, err := newClient()
	if err != nil {
		return "", nil, err
	}
	req, err := c.RegistrationInit(password)
	if err != nil {
		return "", nil, err
	}
	id, err := newFlowID()
	if err != nil {
		return "", nil, err
	}
	putClient(id, c)
	return id, req.Serialize(), nil
}

func RegFinalize(flowID string, responseBytes, serverID []byte) (record []byte, exportKey []byte, err error) {
	c, ok := takeClient(flowID)
	if !ok {
		return nil, nil, errors.New("unknown flow id")
	}
	resp, err := c.Deserialize.RegistrationResponse(responseBytes)
	if err != nil {
		return nil, nil, err
	}
	rec, ek, err := c.RegistrationFinalize(resp, nil, serverID)
	if err != nil {
		return nil, nil, err
	}
	return rec.Serialize(), ek, nil
}

func LoginKE1(password []byte) (flowID string, ke1 []byte, err error) {
	c, err := newClient()
	if err != nil {
		return "", nil, err
	}
	m, err := c.GenerateKE1(password)
	if err != nil {
		return "", nil, err
	}
	id, err := newFlowID()
	if err != nil {
		return "", nil, err
	}
	putClient(id, c)
	return id, m.Serialize(), nil
}

func LoginKE3(flowID string, ke2Bytes, serverID []byte) (ke3 []byte, sessionKey []byte, err error) {
	c, ok := takeClient(flowID)
	if !ok {
		return nil, nil, errors.New("unknown flow id")
	}
	ke2, err := c.Deserialize.KE2(ke2Bytes)
	if err != nil {
		return nil, nil, err
	}
	m, sessionKey, _, err := c.GenerateKE3(ke2, nil, serverID)
	if err != nil {
		return nil, nil, err
	}
	return m.Serialize(), sessionKey, nil
}
