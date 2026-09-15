package projectlist

import (
	"strings"
	"testing"
)

func TestInvSubjectNodeParity(t *testing.T) {
	got := invSubjectLines(`"webgo-p4inv-b" — shared by e2e-user@e2e.test`)
	want := "Subject: =?UTF-8?Q?=22webgo-p4inv-b=22_=E2=80=94_shared_by_?=\r\n =?UTF-8?Q?e2e-user=40e2e=2Etest?="
	if got != want {
		t.Fatalf("subject mismatch:\n got=%q\nwant=%q", got, want)
	}
	// short subjects stay single-word
	got2 := invSubjectLines(`"ab" — shared by x@y.co`)
	t.Logf("short: %q", got2)
	if strings.Count(got2, "?=") < 1 {
		t.Fatal("expected an encoded word")
	}
}
