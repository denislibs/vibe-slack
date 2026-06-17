package delivery

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/messenger/backend/internal/store"
)

var ErrNotMember = errors.New("delivery: sender is not a member of the group")

// Publisher delivers a serialized frame to a device's live channel (Redis pub/sub
// in production; a fake in tests).
type Publisher interface {
	Publish(ctx context.Context, deviceID string, payload []byte) error
}

// OutgoingMessage is the wire shape pushed to recipients (matches the ws "message" frame).
type OutgoingMessage struct {
	Type         string `json:"type"`
	GroupID      string `json:"group_id"`
	Seq          int64  `json:"seq"`
	SenderDevice string `json:"sender_device"`
	ContentType  string `json:"content_type"`
	Ciphertext   []byte `json:"ciphertext"`
	ServerTS     int64  `json:"server_ts"`
}

type Service struct {
	messages *store.MessageRepo
	roster   *store.RosterRepo
	cursors  *store.CursorRepo
	pub      Publisher
}

func NewService(m *store.MessageRepo, r *store.RosterRepo, c *store.CursorRepo, pub Publisher) *Service {
	return &Service{messages: m, roster: r, cursors: c, pub: pub}
}

// Send appends a message and fans out a live notification to every member device
// except the sender's own device. Returns the assigned seq.
func (s *Service) Send(ctx context.Context, senderDevice, groupID, clientMsgID, contentType string, ciphertext []byte) (int64, error) {
	ok, err := s.roster.IsMember(ctx, groupID, senderDevice)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, ErrNotMember
	}
	seq, dup, err := s.messages.Append(ctx, groupID, senderDevice, clientMsgID, contentType, ciphertext)
	if err != nil {
		return 0, err
	}
	if dup {
		return seq, nil
	}
	members, err := s.roster.Members(ctx, groupID)
	if err != nil {
		return seq, err
	}
	frame := OutgoingMessage{
		Type: "message", GroupID: groupID, Seq: seq, SenderDevice: senderDevice,
		ContentType: contentType, Ciphertext: ciphertext,
	}
	payload, _ := json.Marshal(frame)
	for _, m := range members {
		if m.DeviceID == senderDevice {
			continue
		}
		// join_seq is the last seq that existed at join time; only fan out messages
		// strictly newer than that so a member never receives pre-join messages.
		if m.JoinSeq >= seq {
			continue
		}
		_ = s.pub.Publish(ctx, m.DeviceID, payload)
	}
	return seq, nil
}

// Sync returns messages a device missed, from max(sinceSeq, joinSeq) up to a limit.
func (s *Service) Sync(ctx context.Context, deviceID, groupID string, sinceSeq int64) ([]store.Message, error) {
	members, err := s.roster.Members(ctx, groupID)
	if err != nil {
		return nil, err
	}
	var joinSeq int64 = -1
	for _, m := range members {
		if m.DeviceID == deviceID {
			joinSeq = m.JoinSeq
			break
		}
	}
	if joinSeq < 0 {
		return nil, ErrNotMember
	}
	// join_seq records the last seq that existed when the member joined; the member
	// is entitled to strictly newer messages. ListSince treats its joinSeq arg as the
	// first entitled seq (seq > joinSeq-1), so pass join_seq+1.
	return s.messages.ListSince(ctx, groupID, sinceSeq, joinSeq+1, 500)
}

// Ack advances the device's delivery cursor.
func (s *Service) Ack(ctx context.Context, deviceID, groupID string, uptoSeq int64) error {
	return s.cursors.Advance(ctx, deviceID, groupID, uptoSeq)
}
