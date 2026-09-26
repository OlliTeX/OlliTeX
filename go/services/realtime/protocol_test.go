package realtime

import (
	"encoding/json"
	"testing"
)

// TestWirePins — the exact strings observed on the live socket.io 0.9-overleaf
// stack (client 0.9.17-overleaf-6 / server 0.9.19-overleaf-12), captured with
// a WebSocket frame tap during D28a contract mapping. These are the
// compatibility contract; a change here breaks the browser client.
func TestWirePins(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Packet
	}{
		{"connectEcho", "1::", Packet{Type: TypeConnect, Endpoint: ""}},
		{"serverEventJoinProject", `5:::{"name":"joinProjectResponse","args":[{"publicId":"P.abc"}]}`,
			Packet{Type: TypeEvent, Name: EvJoinProjectResponse, Args: []json.RawMessage{[]byte(`{"publicId":"P.abc"}`)}}},
		{"clientAckedEventNoArgs", `5:1+::{"name":"clientTracking.getConnectedUsers"}`,
			Packet{Type: TypeEvent, ID: "1", Ack: true, Name: EvClientTrackingGetUsers, Args: nil}},
		{"clientAckedEventWithArgs", `5:2+::{"name":"clientTracking.updatePosition","args":[{"row":3,"column":7,"doc_id":"6ab73507941509df73b96a35"}]}`,
			Packet{Type: TypeEvent, ID: "2", Ack: true, Name: EvClientTrackingUpdate, Args: []json.RawMessage{[]byte(`{"row":3,"column":7,"doc_id":"6ab73507941509df73b96a35"}`)}}},
		{"serverAckWithData", `6:::1+[null,[{"client_id":"P.abc","connected":true}]]`,
			Packet{Type: TypeAck, AckID: "1", Args: []json.RawMessage{[]byte("null"), []byte(`[{"client_id":"P.abc","connected":true}]`)}}},
		{"serverAckNoData", `6:::7`, Packet{Type: TypeAck, AckID: "7"}},
		{"heartbeat", "2:", Packet{Type: TypeHeartbeat}},
		{"heartbeatDoubleColon", "2::", Packet{Type: TypeHeartbeat}},
		{"clientDisconnect", "0:", Packet{Type: TypeDisconnect}},
		{"clientDisconnectBare", "0::", Packet{Type: TypeDisconnect, Endpoint: ""}},
		{"serverPingFourArgs", `5:::{"name":"serverPing","args":[4,1790418896000,"websocket","SID"]}`,
			Packet{Type: TypeEvent, Name: EvServerPing, Args: []json.RawMessage{[]byte("4"), []byte("1790418896000"), []byte(`"websocket"`), []byte(`"SID"`)}}},
		{"clientPongSixArgs", `5:::{"name":"clientPong","args":[4,1790418896000,"websocket","SID","websocket","SID2"]}`,
			Packet{Type: TypeEvent, Name: EvClientPong, Args: []json.RawMessage{[]byte("4"), []byte("1790418896000"), []byte(`"websocket"`), []byte(`"SID"`), []byte(`"websocket"`), []byte(`"SID2"`)}}},
		{"connectionRejected", `5:::{"name":"connectionRejected","args":[{"message":"invalid session"}]}`,
			Packet{Type: TypeEvent, Name: EvConnectionRejected, Args: []json.RawMessage{[]byte(`{"message":"invalid session"}`)}}},
		{"clientDisconnectedOneArg", `5:::{"name":"clientTracking.clientDisconnected","args":["P.PpenaKTTzleNPGjQAAAN"]}`,
			Packet{Type: TypeEvent, Name: EvClientTrackingDisc, Args: []json.RawMessage{[]byte(`"P.PpenaKTTzleNPGjQAAAN"`)}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := Decode(c.in)
			if !ok {
				t.Fatalf("Decode(%q) not ok", c.in)
			}
			if got.Type != c.want.Type {
				t.Fatalf("type = %d, want %d", got.Type, c.want.Type)
			}
			if got.ID != c.want.ID || got.Ack != c.want.Ack {
				t.Fatalf("id/ack = (%q,%v), want (%q,%v)", got.ID, got.Ack, c.want.ID, c.want.Ack)
			}
			if got.Name != c.want.Name {
				t.Fatalf("name = %q, want %q", got.Name, c.want.Name)
			}
			if got.Endpoint != c.want.Endpoint {
				t.Fatalf("endpoint = %q, want %q", got.Endpoint, c.want.Endpoint)
			}
			if got.AckID != c.want.AckID {
				t.Fatalf("ackID = %q, want %q", got.AckID, c.want.AckID)
			}
			if len(got.Args) != len(c.want.Args) {
				t.Fatalf("args = %d, want %d (got=%s)", len(got.Args), len(c.want.Args), c.in)
			}
			for i := range c.want.Args {
				if string(got.Args[i]) != string(c.want.Args[i]) {
					t.Fatalf("args[%d] = %s, want %s", i, got.Args[i], c.want.Args[i])
				}
			}
		})
	}
}

func TestEncodePins(t *testing.T) {
	if got := EncodeConnect(); got != "1::" {
		t.Fatalf("EncodeConnect = %q, want 1::", got)
	}
	if got := EncodeHeartbeat(); got != "2::" {
		t.Fatalf("EncodeHeartbeat = %q, want 2::", got)
	}
	ev, err := EncodeEvent("joinProjectResponse", map[string]any{"publicId": "P.abc"})
	if err != nil {
		t.Fatal(err)
	}
	if want := `5:::{"name":"joinProjectResponse","args":[{"publicId":"P.abc"}]}`; ev != want {
		t.Fatalf("EncodeEvent = %q, want %q", ev, want)
	}
	ack, err := EncodeAck("1", []any{nil, []any{map[string]any{"client_id": "P.abc", "connected": true}}})
	if err != nil {
		t.Fatal(err)
	}
	if want := `6:::1+[null,[{"client_id":"P.abc","connected":true}]]`; ack != want {
		t.Fatalf("EncodeAck = %q, want %q", ack, want)
	}
	ack7, _ := EncodeAck("7", nil)
	if want := `6:::7`; ack7 != want {
		t.Fatalf("EncodeAck(7) = %q, want %q", ack7, want)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	ev, err := EncodeEvent(EvClientTrackingUpdated, map[string]any{
		"id": "P.x", "user_id": "u1", "email": "a@b.c",
		"row": float64(3), "column": float64(7), "doc_id": "d1",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := Decode(ev)
	if !ok {
		t.Fatalf("round-trip decode failed: %s", ev)
	}
	if got.Name != EvClientTrackingUpdated || len(got.Args) != 1 {
		t.Fatalf("bad round-trip: %+v", got)
	}
	if _, ok := Decode(`5:9+::{"name":"debug","args":[null]}`); !ok {
		t.Fatal("debug event with null arg should decode")
	}
}

func TestDecodeRejectsGarbage(t *testing.T) {
	for _, s := range []string{"", "x", "5:1:x", "5:::notjson", "9:::{}", "6:::x+"} {
		if _, ok := Decode(s); ok {
			t.Fatalf("Decode(%q) unexpectedly ok", s)
		}
	}
	// Bare heartbeat (some clients send it with no colon).
	if p, ok := Decode("2"); !ok || p.Type != TypeHeartbeat {
		t.Fatal("bare '2' heartbeat should decode")
	}
}
