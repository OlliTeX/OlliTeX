package gitproto

import "testing"

func TestEncodePkt(t *testing.T) {
	if got, want := string(encodePkt([]byte("data"))), "0008data"; got != want {
		t.Fatalf("encodePkt=%q want %q", got, want)
	}
	if got, want := string(EncodeFlush()), "0000"; got != want {
		t.Fatalf("flush=%q want %q", got, want)
	}
	if got, want := string(EncodeTerminate()), "00ff"; got != want {
		t.Fatalf("term=%q want %q", got, want)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	cases := []string{"hello\n", "0008data", "done", "NAK"}
	for _, c := range cases {
		enc := encodePkt([]byte(c))
		p, n, err := DecodePkt(enc)
		if err != nil {
			t.Fatalf("case %q: %v", c, err)
		}
		if n != len(enc) {
			t.Fatalf("case %q: consumed %d want %d", c, n, len(enc))
		}
		if string(p.Data) != c {
			t.Fatalf("case %q: data=%q want %q", c, p.Data, c)
		}
	}
}

func TestDecodeFlushTerm(t *testing.T) {
	p, n, err := DecodePkt([]byte("0000"))
	if err != nil || !p.Flush || n != 4 {
		t.Fatalf("flush: %v %v", p, err)
	}
	p, n, _ = DecodePkt([]byte("00ff"))
	if !p.Terminate || n != 4 {
		t.Fatalf("term: %v %d", p, n)
	}
}

func TestEncodeLargeSplit(t *testing.T) {
	big := make([]byte, 0x20000)
	enc := encodePkt(big)
	if len(enc) < 0x10001 {
		t.Fatalf("split not applied: %d", len(enc))
	}
	var got []byte
	rest := enc
	for len(rest) > 0 {
		pkt, n2, err := DecodePkt(rest)
		if err != nil {
			t.Fatalf("mid decode: %v", err)
		}
		got = append(got, pkt.Data...)
		rest = rest[n2:]
		if len(rest) == 0 {
			break
		}
	}
	if len(got) != len(big) {
		t.Fatalf("reassembled %d want %d", len(got), len(big))
	}
}
