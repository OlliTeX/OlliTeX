package requestparser

import "testing"

func TestBuildRegexDirect(t *testing.T) {
	s := "e7b01b52-70eb-4134-9f8e-cab800ef27d3"
	t.Logf("match=%v", BuildRegex.MatchString(s))
	t.Logf("re=%q", BuildRegex.String())
}
