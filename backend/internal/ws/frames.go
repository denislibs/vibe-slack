package ws

import "encoding/json"

// Client→server frames.
type sendFrame struct {
	Type        string `json:"type"`
	ClientMsgID string `json:"client_msg_id"`
	GroupID     string `json:"group_id"`
	ContentType string `json:"content_type"`
	Ciphertext  []byte `json:"ciphertext"`
}
type ackFrame struct {
	Type    string `json:"type"`
	GroupID string `json:"group_id"`
	UpToSeq int64  `json:"up_to_seq"`
}
type syncFrame struct {
	Type     string `json:"type"`
	GroupID  string `json:"group_id"`
	SinceSeq int64  `json:"since_seq"`
}

// Server→client frames.
type sentFrame struct {
	Type        string `json:"type"`
	ClientMsgID string `json:"client_msg_id"`
	GroupID     string `json:"group_id"`
	Seq         int64  `json:"seq"`
	ServerTS    int64  `json:"server_ts"`
}
type errorFrame struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func decodeType(b []byte) (string, error) {
	var env struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		return "", err
	}
	return env.Type, nil
}

func unmarshalFrame(b []byte, dst any) error { return json.Unmarshal(b, dst) }
