package apps

import (
	"net/http/httptest"
	"testing"
)

func TestProbeStatusPing(t *testing.T) {
	a := &App{}
	w := httptest.NewRecorder()
	a.statusPing(w, nil)
	b := w.Body.Bytes()
	t.Logf("body bytes (%d): %X", len(b), b)
}
