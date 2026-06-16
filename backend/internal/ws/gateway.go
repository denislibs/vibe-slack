package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/messenger/backend/internal/delivery"
	"github.com/messenger/backend/internal/fanout"
	"github.com/messenger/backend/internal/hub"
	"github.com/messenger/backend/internal/session"
)

type Gateway struct {
	sess     *session.Manager
	delivery *delivery.Service
	hub      *hub.Hub
	fanout   *fanout.Fanout
	nodeID   string
}

func NewGateway(sess *session.Manager, d *delivery.Service, h *hub.Hub, f *fanout.Fanout, nodeID string) *Gateway {
	return &Gateway{sess: sess, delivery: d, hub: h, fanout: f, nodeID: nodeID}
}

// Handle authenticates, upgrades, and runs the read/write pumps for one connection.
func (g *Gateway) Handle(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	sess, err := g.sess.Validate(r.Context(), token)
	if err != nil || sess.DeviceID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	deviceID := sess.DeviceID

	// InsecureSkipVerify disables origin checking, which is acceptable here because
	// auth is bearer-token (no cookies/CSRF). Production hardening (OriginPatterns)
	// is a deferred follow-up.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer c.CloseNow()

	sendCh, remove := g.hub.Add(deviceID)
	defer remove()
	ctx := context.Background()
	if err := g.fanout.Subscribe(ctx, deviceID); err != nil {
		c.Close(websocket.StatusInternalError, "subscribe failed")
		return
	}
	defer g.fanout.Unsubscribe(ctx, deviceID)

	// Write pump: the single goroutine allowed to write to c. All server→client
	// frames flow through the hub channel so the connection is never written
	// concurrently (coder/websocket forbids concurrent writes).
	writeCtx, cancelWrite := context.WithCancel(ctx)
	defer cancelWrite()
	go func() {
		for {
			select {
			case <-writeCtx.Done():
				return
			case payload, ok := <-sendCh:
				if !ok {
					return
				}
				wctx, cancel := context.WithTimeout(writeCtx, 10*time.Second)
				err := c.Write(wctx, websocket.MessageText, payload)
				cancel()
				if err != nil {
					return
				}
			}
		}
	}()

	// Read pump.
	for {
		rctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		_, data, err := c.Read(rctx)
		cancel()
		if err != nil {
			return
		}
		g.handleFrame(ctx, deviceID, data)
	}
}

func (g *Gateway) handleFrame(ctx context.Context, deviceID string, data []byte) {
	typ, err := decodeType(data)
	if err != nil {
		g.push(deviceID, errorFrame{Type: "error", Code: "bad_frame", Message: "invalid frame"})
		return
	}
	switch typ {
	case "send":
		var f sendFrame
		if unmarshalFrame(data, &f) != nil {
			g.push(deviceID, errorFrame{Type: "error", Code: "bad_frame", Message: "invalid send"})
			return
		}
		seq, err := g.delivery.Send(ctx, deviceID, f.GroupID, f.ClientMsgID, f.ContentType, f.Ciphertext)
		if errors.Is(err, delivery.ErrNotMember) {
			g.push(deviceID, errorFrame{Type: "error", Code: "forbidden", Message: "not a group member"})
			return
		}
		if err != nil {
			g.push(deviceID, errorFrame{Type: "error", Code: "internal", Message: "send failed"})
			return
		}
		g.push(deviceID, sentFrame{Type: "sent", ClientMsgID: f.ClientMsgID, GroupID: f.GroupID, Seq: seq, ServerTS: time.Now().Unix()})
	case "ack":
		var f ackFrame
		if unmarshalFrame(data, &f) == nil {
			_ = g.delivery.Ack(ctx, deviceID, f.GroupID, f.UpToSeq)
		}
	case "sync":
		var f syncFrame
		if unmarshalFrame(data, &f) != nil {
			return
		}
		msgs, err := g.delivery.Sync(ctx, deviceID, f.GroupID, f.SinceSeq)
		if err != nil {
			g.push(deviceID, errorFrame{Type: "error", Code: "sync_failed", Message: "sync failed"})
			return
		}
		for _, m := range msgs {
			g.push(deviceID, delivery.OutgoingMessage{
				Type: "message", GroupID: m.GroupID, Seq: m.Seq, SenderDevice: m.SenderDevice,
				ContentType: m.ContentType, Ciphertext: m.Ciphertext, ServerTS: m.ServerTS.Unix(),
			})
		}
	default:
		g.push(deviceID, errorFrame{Type: "error", Code: "unknown_type", Message: "unknown frame type"})
	}
}

// push delivers a server→client frame through the local hub (the write pump sends it).
func (g *Gateway) push(deviceID string, v any) {
	payload, _ := json.Marshal(v)
	g.hub.Deliver(deviceID, payload)
}
