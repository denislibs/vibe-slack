package as

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/messenger/backend/internal/opaque"
	"github.com/messenger/backend/internal/session"
	"github.com/messenger/backend/internal/store"
	goredis "github.com/redis/go-redis/v9"
)

var ErrAuthFailed = errors.New("as: authentication failed")

const loginStateTTL = 30 * time.Second

// UserStore is the subset of the user repository the AS needs.
type UserStore interface {
	Create(ctx context.Context, email string, opaqueRecord []byte) (*store.User, error)
	GetByEmail(ctx context.Context, email string) (*store.User, error)
}

type Service struct {
	opaque *opaque.Server
	users  UserStore
	sess   *session.Manager
	rdb    *goredis.Client
}

func NewService(o *opaque.Server, users UserStore, sess *session.Manager, rdb *goredis.Client) *Service {
	return &Service{opaque: o, users: users, sess: sess, rdb: rdb}
}

// credID derives the stable OPAQUE credential identifier from the email.
func credID(email string) []byte { return []byte("cred:" + email) }

func (s *Service) RegisterStart(ctx context.Context, email string, regReq []byte) ([]byte, error) {
	return s.opaque.RegistrationResponse(regReq, credID(email))
}

func (s *Service) RegisterFinish(ctx context.Context, email string, record []byte) error {
	_, err := s.users.Create(ctx, email, record)
	return err
}

type loginState struct {
	UserID            string `json:"user_id"` // empty for fake/unknown user
	Email             string `json:"email"`
	ExpectedClientMAC []byte `json:"mac"`
	SessionSecret     []byte `json:"secret"`
}

func loginKey(id string) string { return "login:" + id }

// LoginStart returns a login_id and serialized KE2. For unknown users it uses a
// deterministic fake record so the response is indistinguishable (anti-enumeration).
//
// Timing note: the real path performs a DB lookup (GetByEmail) that the fake path
// does not; the dominant cost on both paths is the OPAQUE GenerateKE2 crypto, so the
// residual timing difference is small. Accepted for this milestone; if a constant-time
// guarantee is later required, perform an equivalent dummy lookup on the fake path.
func (s *Service) LoginStart(ctx context.Context, email string, ke1 []byte) (string, []byte, error) {
	var (
		recordBytes []byte
		userID      string
	)
	u, err := s.users.GetByEmail(ctx, email)
	switch {
	case err == nil:
		recordBytes, userID = u.OpaqueRecord, u.ID
	case errors.Is(err, store.ErrNotFound):
		fake, ferr := s.opaque.FakeRecord(credID(email))
		if ferr != nil {
			return "", nil, ferr
		}
		recordBytes = fake
	default:
		return "", nil, err
	}

	ke2, st, err := s.opaque.LoginStart(ke1, credID(email), recordBytes)
	if err != nil {
		return "", nil, err
	}

	id, err := randID()
	if err != nil {
		return "", nil, err
	}
	data, _ := json.Marshal(loginState{
		UserID: userID, Email: email,
		ExpectedClientMAC: st.ExpectedClientMAC, SessionSecret: st.SessionSecret,
	})
	if err := s.rdb.Set(ctx, loginKey(id), data, loginStateTTL).Err(); err != nil {
		return "", nil, err
	}
	return id, ke2, nil
}

// LoginFinish verifies KE3. Returns a session token and whether device enrollment
// is still required. Unknown users and wrong passwords both yield ErrAuthFailed.
func (s *Service) LoginFinish(ctx context.Context, loginID string, ke3 []byte) (string, bool, error) {
	data, err := s.rdb.Get(ctx, loginKey(loginID)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return "", false, ErrAuthFailed
	}
	if err != nil {
		return "", false, err
	}
	s.rdb.Del(ctx, loginKey(loginID)) // single-use

	var ls loginState
	if err := json.Unmarshal(data, &ls); err != nil {
		return "", false, err
	}

	if _, err := s.opaque.LoginFinish(ke3, &opaque.LoginState{
		ExpectedClientMAC: ls.ExpectedClientMAC, SessionSecret: ls.SessionSecret,
	}); err != nil {
		return "", false, ErrAuthFailed
	}
	if ls.UserID == "" { // fake-record path can't truly succeed; guard defensively.
		return "", false, ErrAuthFailed
	}

	token, err := s.sess.Issue(ctx, ls.UserID, "")
	if err != nil {
		return "", false, err
	}
	return token, true, nil // device_enroll_required true until a device is bound
}

func randID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
