package toolkit

import (
	"strings"
	"testing"
)

func TestDemuxFrame(t *testing.T) {
	// one full frame: header(8) + "hello"(5)
	var sb strings.Builder
	frame := []byte{0, 0, 0, 0, 0, 0, 0, 5, 'h', 'e', 'l', 'l', 'o'}
	got, complete := demuxFrame(frame, &sb)
	if !complete || got != 13 || sb.String() != "hello" {
		t.Fatalf("full frame: got=%d complete=%v out=%q", got, complete, sb.String())
	}
	// partial frame (header says 5, only 2 bytes present) → not complete, nothing written
	sb.Reset()
	partial := []byte{0, 0, 0, 0, 0, 0, 0, 5, 'h', 'i'}
	if got, complete := demuxFrame(partial, &sb); complete || got != 0 || sb.Len() != 0 {
		t.Fatalf("partial frame: got=%d complete=%v out=%q", got, complete, sb.String())
	}
	// two frames concatenated in one buffer
	sb.Reset()
	double := append(append([]byte{}, frame...), frame...)
	if got, complete := demuxFrame(double, &sb); !complete || got != 26 || sb.String() != "hellohello" {
		t.Fatalf("two frames: got=%d complete=%v out=%q", got, complete, sb.String())
	}
	// raw passthrough (< 8 bytes)
	sb.Reset()
	if _, ok := demuxFrame([]byte("abc"), &sb); !ok || sb.String() != "abc" {
		t.Fatalf("raw passthrough: %q ok=%v", sb.String(), ok)
	}
}
