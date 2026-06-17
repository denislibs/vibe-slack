// Command seed populates a running messenger backend with demo accounts,
// workspaces, memberships, and channels.
//
// Accounts use OPAQUE: the server stores an opaque_record that can only be
// produced by running the OPAQUE registration protocol. So this tool registers
// each user via the real HTTP flow using the SAME OPAQUE client the frontend
// uses (github.com/bytemare/opaque DefaultConfiguration, serverIdentity
// "messenger-as", nil clientIdentity), mirroring
// client/src/shared/lib/auth/opaque-wasm/client.go exactly.
//
// Usage:
//
//	BASE_URL=http://localhost:18080 PASSWORD=password123 go run ./cmd/seed/
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	xopaque "github.com/bytemare/opaque"
)

// serverID is the OPAQUE server identity; matches the AS ("messenger-as").
var serverID = []byte("messenger-as")

func main() {
	baseURL := envOr("BASE_URL", "http://localhost:18080")
	password := []byte(envOr("PASSWORD", "password123"))

	c := &client{baseURL: baseURL, http: &http.Client{Timeout: 30 * time.Second}}

	plan := buildPlan()

	log.Printf("seeding %s (password=%q)", baseURL, string(password))

	// 1. Register + login every user, capturing session tokens by email.
	tokens := map[string]string{}
	var registered, alreadyExisted int
	for _, u := range plan.users {
		newUser, err := c.register(u.email, u.username, password)
		if err != nil {
			log.Fatalf("register %s: %v", u.email, err)
		}
		if newUser {
			registered++
			log.Printf("created user %s (%s)", u.username, u.email)
		} else {
			alreadyExisted++
			log.Printf("user %s (%s) already registered, skipping", u.username, u.email)
		}
		tok, err := c.login(u.email, password)
		if err != nil {
			log.Fatalf("login %s: %v", u.email, err)
		}
		tokens[u.email] = tok
		log.Printf("logged in %s", u.email)
	}

	// 2. Create workspaces (by owner), add members, create channels.
	var wsCreated, membersAdded, channelsCreated int
	for _, ws := range plan.workspaces {
		ownerTok := tokens[ws.ownerEmail]
		if ownerTok == "" {
			log.Fatalf("workspace %q: owner %s has no session token", ws.name, ws.ownerEmail)
		}
		var wr workspaceResp
		if err := c.postJSON(ownerTok, "/workspaces", map[string]string{"name": ws.name}, &wr); err != nil {
			log.Fatalf("create workspace %q: %v", ws.name, err)
		}
		wsCreated++
		log.Printf("created workspace %s (id=%s slug=%s, owner=%s)", wr.Name, wr.ID, wr.Slug, ws.ownerEmail)

		for _, memberEmail := range ws.members {
			var mr wsMemberResp
			err := c.postJSON(ownerTok, "/workspaces/"+wr.ID+"/members",
				map[string]string{"email_or_username": memberEmail}, &mr)
			if err != nil {
				log.Fatalf("add member %s to %q: %v", memberEmail, ws.name, err)
			}
			membersAdded++
			log.Printf("added %s to workspace %s", memberEmail, wr.Name)
		}

		for _, ch := range ws.channels {
			var cr convResp
			err := c.postJSON(ownerTok, "/workspaces/"+wr.ID+"/conversations",
				map[string]string{"type": "channel", "visibility": "public", "name": ch}, &cr)
			if err != nil {
				log.Fatalf("create channel #%s in %q: %v", ch, ws.name, err)
			}
			channelsCreated++
			log.Printf("created channel #%s in %s (group_id=%s)", cr.Name, wr.Name, cr.GroupID)
		}
	}

	log.Printf("==== seed summary ====")
	log.Printf("users: %d new, %d already existed (%d total in plan)", registered, alreadyExisted, len(plan.users))
	log.Printf("workspaces created: %d", wsCreated)
	log.Printf("memberships added:  %d", membersAdded)
	log.Printf("channels created:   %d", channelsCreated)
	log.Printf("note: re-running against a non-fresh DB will skip existing registrations (409) but duplicate workspaces get deduped slugs.")
}

// ---------------------------------------------------------------------------
// Seed plan
// ---------------------------------------------------------------------------

type seedUser struct{ email, username string }

type seedWorkspace struct {
	name       string
	ownerEmail string
	members    []string // emails of existing users to add
	channels   []string
}

type seedPlan struct {
	users      []seedUser
	workspaces []seedWorkspace
}

func buildPlan() seedPlan {
	users := []seedUser{
		{"bob@corp.com", "bob"},
		{"carol@corp.com", "carol"},
		{"dave@corp.com", "dave"},
		{"erin@corp.com", "erin"},
		{"frank@corp.com", "frank"},
		{"grace@corp.com", "grace"},
		{"heidi@corp.com", "heidi"},
		{"ivan@corp.com", "ivan"},
	}
	workspaces := []seedWorkspace{
		{
			name:       "Engineering",
			ownerEmail: "bob@corp.com",
			members:    []string{"erin@corp.com", "frank@corp.com", "grace@corp.com"},
			channels:   []string{"general", "backend", "random"},
		},
		{
			name:       "Design",
			ownerEmail: "carol@corp.com",
			members:    []string{"grace@corp.com", "heidi@corp.com"},
			channels:   []string{"general", "figma"},
		},
		{
			name:       "Marketing",
			ownerEmail: "dave@corp.com",
			members:    []string{"heidi@corp.com", "ivan@corp.com"},
			channels:   []string{"general", "campaigns"},
		},
	}
	return seedPlan{users: users, workspaces: workspaces}
}

// ---------------------------------------------------------------------------
// HTTP client helper
// ---------------------------------------------------------------------------

type client struct {
	baseURL string
	http    *http.Client
}

// postJSON POSTs body as JSON to path. If token != "" it sets Bearer auth.
// out (if non-nil) is JSON-decoded from the response. Non-2xx returns an error
// including the response body. The HTTP status code is also returned so callers
// can detect conditions like 409.
func (c *client) postJSONStatus(token, path string, body, out any) (int, error) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(body); err != nil {
		return 0, fmt.Errorf("encode body: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, &buf)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	// The AS rate-limits /auth/register/start and /auth/login/start (fixed
	// window, ~10/min per IP). On 429 we wait out the window and retry so a
	// full seed run completes; the window is 1 minute, so sleeping ~15s and
	// retrying enough times reliably crosses a window boundary.
	var resp *http.Response
	var raw []byte
	const maxAttempts = 12
	for attempt := 0; ; attempt++ {
		// rewind the body for each attempt.
		req.Body = io.NopCloser(bytes.NewReader(buf.Bytes()))
		var derr error
		resp, derr = c.http.Do(req)
		if derr != nil {
			return 0, derr
		}
		raw, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusTooManyRequests || attempt >= maxAttempts {
			break
		}
		log.Printf("rate limited on %s, waiting for window reset (attempt %d/%d)", path, attempt+1, maxAttempts)
		time.Sleep(15 * time.Second)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("POST %s -> %d: %s", path, resp.StatusCode, bytes.TrimSpace(raw))
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return resp.StatusCode, fmt.Errorf("decode response from %s: %w (body=%s)", path, err, raw)
		}
	}
	return resp.StatusCode, nil
}

func (c *client) postJSON(token, path string, body, out any) error {
	_, err := c.postJSONStatus(token, path, body, out)
	return err
}

// ---------------------------------------------------------------------------
// Wire DTOs (subset, matching backend/internal/httpapi/dto.go)
// ---------------------------------------------------------------------------

type registerStartReq struct {
	Email                     string `json:"email"`
	OpaqueRegistrationRequest string `json:"opaque_registration_request"`
}
type registerStartResp struct {
	OpaqueRegistrationResponse string `json:"opaque_registration_response"`
}
type registerFinishReq struct {
	Email                    string `json:"email"`
	Username                 string `json:"username"`
	OpaqueRegistrationRecord string `json:"opaque_registration_record"`
}
type loginStartReq struct {
	Email string `json:"email"`
	KE1   string `json:"ke1"`
}
type loginStartResp struct {
	LoginID string `json:"login_id"`
	KE2     string `json:"ke2"`
}
type loginFinishReq struct {
	LoginID string `json:"login_id"`
	KE3     string `json:"ke3"`
}
type loginFinishResp struct {
	SessionToken         string `json:"session_token"`
	DeviceEnrollRequired bool   `json:"device_enroll_required"`
}
type workspaceResp struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	Role string `json:"role"`
}
type wsMemberResp struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Role     string `json:"role"`
}
type convResp struct {
	GroupID    string `json:"group_id"`
	Type       string `json:"type"`
	Visibility string `json:"visibility"`
	Name       string `json:"name"`
}

// ---------------------------------------------------------------------------
// OPAQUE flows (mirror client.go + auth_handlers.go)
//
// Registration: RegistrationInit -> POST /auth/register/start ->
//   RegistrationFinalize -> POST /auth/register/finish.
// Login:        GenerateKE1 -> POST /auth/login/start ->
//   GenerateKE3 -> POST /auth/login/finish.
//
// The bytemare Client is stateful between Init/Finalize and KE1/KE3, so we keep
// the same *xopaque.Client instance across each round trip (sequential per
// user, so plain locals suffice). Server payloads are StdEncoding base64,
// matching the handlers' b64enc/decodeB64.
// ---------------------------------------------------------------------------

func newOpaqueClient() (*xopaque.Client, error) {
	return xopaque.DefaultConfiguration().Client()
}

// register runs the full OPAQUE registration. It returns true if a new account
// was created, false if the account already existed (HTTP 409), and an error
// otherwise. A 409 is treated as "already registered" and is not fatal.
func (c *client) register(email, username string, password []byte) (created bool, err error) {
	oc, err := newOpaqueClient()
	if err != nil {
		return false, err
	}

	// Step 1: RegistrationInit(password) -> registration request bytes.
	regReq, err := oc.RegistrationInit(password)
	if err != nil {
		return false, fmt.Errorf("registration init: %w", err)
	}

	var startResp registerStartResp
	err = c.postJSON("", "/auth/register/start", registerStartReq{
		Email:                     email,
		OpaqueRegistrationRequest: b64enc(regReq.Serialize()),
	}, &startResp)
	if err != nil {
		return false, fmt.Errorf("register/start: %w", err)
	}

	// Step 2: deserialize server response, RegistrationFinalize -> record.
	respBytes, err := b64dec(startResp.OpaqueRegistrationResponse)
	if err != nil {
		return false, fmt.Errorf("decode registration response: %w", err)
	}
	regResp, err := oc.Deserialize.RegistrationResponse(respBytes)
	if err != nil {
		return false, fmt.Errorf("deserialize registration response: %w", err)
	}
	// clientIdentity = nil, serverIdentity = serverID, exactly as client.go.
	record, _, err := oc.RegistrationFinalize(regResp, nil, serverID)
	if err != nil {
		return false, fmt.Errorf("registration finalize: %w", err)
	}

	// Step 3: finish. 409 = account already exists -> treat as OK, skip.
	status, err := c.postJSONStatus("", "/auth/register/finish", registerFinishReq{
		Email:                    email,
		Username:                 username,
		OpaqueRegistrationRecord: b64enc(record.Serialize()),
	}, nil)
	if status == http.StatusConflict {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("register/finish: %w", err)
	}
	return true, nil
}

// login runs the full OPAQUE login and returns the session token.
func (c *client) login(email string, password []byte) (string, error) {
	oc, err := newOpaqueClient()
	if err != nil {
		return "", err
	}

	// Step 1: GenerateKE1(password) -> ke1 bytes.
	ke1, err := oc.GenerateKE1(password)
	if err != nil {
		return "", fmt.Errorf("generate ke1: %w", err)
	}

	var startResp loginStartResp
	err = c.postJSON("", "/auth/login/start", loginStartReq{
		Email: email,
		KE1:   b64enc(ke1.Serialize()),
	}, &startResp)
	if err != nil {
		return "", fmt.Errorf("login/start: %w", err)
	}

	// Step 2: deserialize ke2, GenerateKE3 -> ke3.
	ke2Bytes, err := b64dec(startResp.KE2)
	if err != nil {
		return "", fmt.Errorf("decode ke2: %w", err)
	}
	ke2, err := oc.Deserialize.KE2(ke2Bytes)
	if err != nil {
		return "", fmt.Errorf("deserialize ke2: %w", err)
	}
	// clientIdentity = nil, serverIdentity = serverID, exactly as client.go.
	ke3, _, _, err := oc.GenerateKE3(ke2, nil, serverID)
	if err != nil {
		return "", fmt.Errorf("generate ke3: %w", err)
	}

	// Step 3: finish -> session token.
	var finResp loginFinishResp
	err = c.postJSON("", "/auth/login/finish", loginFinishReq{
		LoginID: startResp.LoginID,
		KE3:     b64enc(ke3.Serialize()),
	}, &finResp)
	if err != nil {
		return "", fmt.Errorf("login/finish: %w", err)
	}
	if finResp.SessionToken == "" {
		return "", fmt.Errorf("login/finish: empty session token")
	}
	return finResp.SessionToken, nil
}

// ---------------------------------------------------------------------------
// small helpers
// ---------------------------------------------------------------------------

func b64enc(b []byte) string         { return base64.StdEncoding.EncodeToString(b) }
func b64dec(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(s) }

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
