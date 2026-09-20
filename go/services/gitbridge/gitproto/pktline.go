// Package gitproto implements the git "smart" wire protocol (stateless RPC)
// over HTTP: upload-pack (fetch/clone) and receive-pack (push), plus the
// side-band framing used by git clients.
package gitproto

import (
	"errors"
	"fmt"
)

// Pkt is one packet line.
type Pkt struct {
	// Flush marks a 0000 flush packet.
	Flush bool
	// Terminate marks a 00ff terminator packet.
	Terminate bool
	// Data is the payload after the 4-byte length prefix.
	Data []byte
}

// encodePkt returns the wire encoding of data as one or more pkt lines.
func encodePkt(data []byte) []byte {
	var out []byte
	rest := data
	for len(rest) > 0 {
		chunk := rest
		if len(chunk) > 0xfffb {
			chunk = chunk[:0xfffb]
		}
		out = appendHexLen(out, len(chunk)+4)
		out = append(out, chunk...)
		rest = rest[len(chunk):]
	}
	return out
}

// EncodeFlush is the wire bytes for a flush packet.
func EncodeFlush() []byte { return []byte("0000") }

// EncodeTerminate is the wire bytes for a term packet.
func EncodeTerminate() []byte { return []byte("00ff") }

func appendHexLen(dst []byte, total int) []byte {
	out := make([]byte, 4)
	out[0] = hexDigit(total >> 12)
	out[1] = hexDigit(total >> 8)
	out[2] = hexDigit(total >> 4)
	out[3] = hexDigit(total)
	return append(dst, out...)
}

func hexDigit(v int) byte { return "0123456789abcdef"[v&0x0f] }

func hexValue(c byte) (int, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), true
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10, true
	default:
		return 0, false
	}
}

// DecodePkt decodes one packet line from buf, returning bytes consumed.
func DecodePkt(buf []byte) (Pkt, int, error) {
	if len(buf) < 4 {
		return Pkt{}, 0, errors.New("gitproto: incomplete length")
	}
	n := 0
	for _, c := range buf[0:4] {
		d, ok := hexValue(c)
		if !ok {
			return Pkt{}, 0, fmt.Errorf("gitproto: invalid length byte %q", c)
		}
		n = n*16 + d
	}
	switch n {
	case 0x0000:
		return Pkt{Flush: true}, 4, nil
	case 0x00ff:
		return Pkt{Terminate: true}, 4, nil
	default:
		if len(buf) < n {
			return Pkt{}, 0, errors.New("gitproto: incomplete pkt line")
		}
		data := make([]byte, n-4)
		copy(data, buf[4:n])
		return Pkt{Data: data}, n, nil
	}
}
