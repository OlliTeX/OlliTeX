// Package realtime is the Go replacement for the Node `real-time` service:
// the project event bus (socket.io 0.9, overleaf fork wire protocol) behind
// /socket.io — presence (clientTracking.*), project join, drain, message
// relay, and the ops HTTP API.
//
// D28a (S4 FLIP): the OT text-sync substrate (joinDoc/applyOtUpdate) is
// retired with the F2 client hard-cut; text collaboration runs over Yjs at
// the Go collab service (:3450). What remains of real-time — and what this
// package ports 1:1 — is the socket.io event bus the IDE still depends on:
//   - handshake + websocket/xhr-polling transports (socket.io 0.9 framing)
//   - joinProjectResponse / connectionRejected (via the Go web private API)
//   - clientTracking.getConnectedUsers / updatePosition / clientUpdated /
//     clientDisconnected (redis-backed presence, same keys as Node)
//   - serverPing/clientPong, reconnectGracefully, drain
//   - /socket.io/socket.io.js client bundle, /clients, /drain, etc.
//
// Wire protocol source of truth (verified empirically against the live Node
// service + pinned in protocol_test.go):
//   - socket.io server 0.9.19-overleaf-12 (socket-parser encode/decodePacket)
//   - socket.io client 0.9.17-overleaf-6 (the bundle served at /socket.io.js)
//   - live capture: handshake body "S:60:60:websocket,xhr-polling", connect
//     echo "1::", server event "5:::{"name":...,"args":[...]}", client acked
//     event "5:1+::{"name":"clientTracking.getConnectedUsers"}", server ack
//     "6:::1+[null,[...]]".
package realtime

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// PacketType — socket.io 0.9 socket-parser types (pin: parser.js `packets`).
type PacketType int

const (
	TypeDisconnect PacketType = iota // 0
	TypeConnect                      // 1
	TypeHeartbeat                    // 2
	TypeMessage                      // 3
	TypeJSON                         // 4
	TypeEvent                        // 5
	TypeAck                          // 6
	TypeError                        // 7
	TypeNoop                         // 8
)

// Packet is a decoded socket.io 0.9 packet.
type Packet struct {
	Type     PacketType
	ID       string // numeric id when the packet carries an ack flag
	Ack      bool   // the '+' flag on the wire
	Endpoint string // namespace (overleaf: always the default "")
	// Type-specific payload:
	Name  string            // Event
	Args  []json.RawMessage // Event (nil = none)
	Data  json.RawMessage   // Message/JSON (raw) / Error (reason, raw string)
	AckID string            // Ack
}

// encodeParts implements socket-parser encodePacket for the 0.9 family:
// [type, id(+flag), endpoint] joined with ':' plus ':data' when the packet
// carries data. The exact slot emptiness (e.g. "1::" for connect, "5:::{...}"
// for a default-namespace event) is client-visible and pinned by tests.
func encodeParts(t PacketType, id string, ack bool, endpoint string, data string) string {
	idSlot := id
	if ack {
		idSlot += "+"
	}
	parts := []string{strconv.Itoa(int(t)), idSlot, endpoint}
	if data != "" {
		parts = append(parts, data)
	}
	return strings.Join(parts, ":")
}

// EncodeEvent — server->client event, socket-parser 'event':
// data = JSON {name, args} (args always serialized, [] when empty).
func EncodeEvent(name string, args ...any) (string, error) {
	argsJSON := "[]"
	if len(args) > 0 {
		b, err := json.Marshal(args) // args is []any -> JSON array
		if err != nil {
			return "", err
		}
		argsJSON = string(b)
	}
	data := `{"name":` + jsonString(name) + `,"args":` + argsJSON + `}`
	return encodeParts(TypeEvent, "", false, "", data), nil
}

func EncodeAck(ackID string, args []any) (string, error) {
	data := ackID
	if len(args) > 0 {
		b, err := json.Marshal(args)
		if err != nil {
			return "", err
		}
		data = ackID + "+" + string(b)
	}
	return encodeParts(TypeAck, "", false, "", data), nil
}

// EncodeConnect — the transport-open handshake echo (wire: "1::").
func EncodeConnect() string { return encodeParts(TypeConnect, "", false, "", "") }

// EncodeHeartbeat — wire "2::".
func EncodeHeartbeat() string { return encodeParts(TypeHeartbeat, "", false, "", "") }

func EncodeError(reason string) string { return encodeParts(TypeError, "", false, "", reason) }

func EncodeDisconnect(endpoint string) string {
	return encodeParts(TypeDisconnect, "", false, endpoint, "")
}

// EncodeAckRaw — server->client ack from already-JSON'ed args: data =
// ackId[+argsJSON] (argsJSON = the full JSON array, [] when empty).
func EncodeAckRaw(ackID string, argsRaw json.RawMessage) (string, error) {
	data := ackID
	raw := trimJSONBytes(argsRaw)
	if len(raw) > 0 && string(raw) != "null" {
		data = ackID + "+" + string(raw)
	}
	return encodeParts(TypeAck, "", false, "", data), nil
}

// EncodeEventJSON — server->client event from already-JSON'ed args array.
func EncodeEventJSON(name string, argsRaw []json.RawMessage) (string, error) {
	if len(argsRaw) == 0 {
		argsRaw = []json.RawMessage{}
	}
	var sb []byte
	sb = append(sb, '[')
	for i, a := range argsRaw {
		if i > 0 {
			sb = append(sb, ',')
		}
		sb = append(sb, trimJSONBytes(a)...)
	}
	sb = append(sb, ']')
	data := `{"name":` + jsonString(name) + `,"args":` + string(sb) + `}`
	return encodeParts(TypeEvent, "", false, "", data), nil
}

func trimJSONBytes(b json.RawMessage) []byte {
	out := b
	for len(out) > 0 && (out[0] == ' ' || out[0] == '\t' || out[0] == '\n' || out[0] == '\r') {
		out = out[1:]
	}
	return out
}

func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}

// decodeRe — the client's decodePacket regex, kept byte-identical so the
// server accepts exactly what the bundled client emits (and vice versa).
var decodeRe = regexp.MustCompile(`^([^:]+):([0-9]+)?(\+)?:([^:]+)?:?([\s\S]*)?`)

// Decode parses one wire packet. ok=false on malformed type.
func Decode(s string) (p Packet, ok bool) {
	// Short forms ("2", "2:", "0:") — the V8 socket.io 0.9 parsers accept
	// them (all-optional-tail regex) but RE2's leftmost strategy does not;
	// accept them explicitly. The overleaf client encodes "2::"/"0::"
	// (verified in the 0.9.17-overleaf-6 encodePacket source).
	if len(s) > 0 && s[0] >= '0' && s[0] <= '8' && s[len(s)-1] == ':' && len(s) <= 2 {
		t := PacketType(s[0] - '0')
		return Packet{Type: t}, true
	}
	if s == "2" {
		return Packet{Type: TypeHeartbeat}, true
	}
	m := decodeRe.FindStringSubmatch(s)
	if m == nil {
		return Packet{}, false
	}
	t, cerr := strconv.Atoi(m[1])
	if cerr != nil {
		return Packet{}, false
	}
	p = Packet{ID: m[2], Ack: m[3] == "+", Endpoint: m[4]}
	p.Type = PacketType(t)
	data := m[5]
	switch p.Type {
	case TypeConnect:
		if data != "" {
			return Packet{}, false
		}
	case TypeDisconnect:
		if data != "" {
			return Packet{}, false
		}
	case TypeHeartbeat, TypeNoop:
	case TypeEvent:
		var body struct {
			Name string            `json:"name"`
			Args []json.RawMessage `json:"args"`
		}
		if err := json.Unmarshal([]byte(data), &body); err != nil || body.Name == "" {
			return Packet{}, false
		}
		p.Name = body.Name
		p.Args = body.Args
	case TypeAck:
		// data = ackId[+args]
		i := 0
		for i < len(data) && data[i] >= '0' && data[i] <= '9' {
			i++
		}
		if i == 0 {
			return Packet{}, false
		}
		p.AckID = data[:i]
		if i < len(data) {
			if data[i] != '+' {
				return Packet{}, false
			}
			rest := data[i+1:]
			var args []json.RawMessage
			if rest == "" || rest == "null" {
				p.Args = nil
			} else if err := json.Unmarshal([]byte(rest), &args); err != nil {
				return Packet{}, false
			} else {
				p.Args = args
			}
		}
	case TypeMessage, TypeJSON:
		p.Data = json.RawMessage(data)
	case TypeError:
		p.Data = json.RawMessage(`"` + strings.ReplaceAll(data, `"`, `\"`) + `"`)
	default:
		return Packet{}, false
	}
	return p, true
}

// EventNames — bus event names (overleaf real-time Router.js, D28a map).
const (
	EvConnectionRejected     = "connectionRejected"
	EvJoinProjectResponse    = "joinProjectResponse"
	EvReconnectGracefully    = "reconnectGracefully"
	EvClientTrackingRefresh  = "clientTracking.refresh"
	EvClientTrackingUpdated  = "clientTracking.clientUpdated"
	EvClientTrackingDisc     = "clientTracking.clientDisconnected"
	EvClientTrackingGetUsers = "clientTracking.getConnectedUsers"
	EvClientTrackingUpdate   = "clientTracking.updatePosition"
	EvDebug                  = "debug"
	EvDebugGetHostname       = "debug.getHostname"
	EvServerPing             = "serverPing"
	EvClientPong             = "clientPong"
)
