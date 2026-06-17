package ws

import "testing"

func TestDecodeFrameType(t *testing.T) {
	typ, err := decodeType([]byte(`{"type":"send","group_id":"g1"}`))
	if err != nil || typ != "send" {
		t.Fatalf("type=%q err=%v", typ, err)
	}
	if _, err := decodeType([]byte(`not json`)); err == nil {
		t.Fatal("expected error on bad JSON")
	}
}

func TestSendFrameRoundTrips(t *testing.T) {
	var f sendFrame
	if err := unmarshalFrame([]byte(`{"type":"send","client_msg_id":"c1","group_id":"g1","content_type":"application","ciphertext":"AQID"}`), &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if f.GroupID != "g1" || f.ClientMsgID != "c1" || len(f.Ciphertext) != 3 {
		t.Fatalf("bad decode: %+v", f)
	}
}
