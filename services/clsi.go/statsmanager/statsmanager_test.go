package statsmanager

import (
	"testing"
)

func TestSampleByHashZeroNegative(t *testing.T) {
	if SampleByHash("any-key", 0) {
		t.Error("pct 0 must sample no one")
	}
	if SampleByHash("any-key", -5) {
		t.Error("negative pct must sample no one")
	}
}

func TestSampleByHashPercent100And0(t *testing.T) {
	if !SampleByHash("some-user-id", 100) {
		t.Error("pct 100 must sample everyone")
	}
	// percentile is [0..99], so 0 samples no one.
	if SampleByHash("some-user-id", 0) {
		t.Error("pct 0 must sample no one")
	}
}

func TestSampleByHashConsistent(t *testing.T) {
	a := SampleByHash("user-123", 50)
	b := SampleByHash("user-123", 50)
	if a != b {
		t.Error("sampleByHash must be stable for the same key")
	}
}

func TestSampleRequestNoUser(t *testing.T) {
	if got := SampleRequest(RequestSample{MetricsPath: "/compile"}, 50); got != nil {
		t.Errorf("no user_id => %v, want nil", *got)
	}
}

func TestSampleRequestExcludedPath(t *testing.T) {
	for _, p := range []string{"clsi-perf", "health-check", "clsi-cache-template"} {
		if got := SampleRequest(RequestSample{UserID: "u1", MetricsPath: p}, 100); got != nil {
			t.Errorf("path %s must be excluded; got %v", p, *got)
		}
	}
}

func TestSampleRequestZeroPct(t *testing.T) {
	if got := SampleRequest(RequestSample{UserID: "u1", MetricsPath: "/compile"}, 0); got != nil {
		t.Errorf("pct 0 => nil, got %v", *got)
	}
}

func TestSampleRequestHappyPathMatchesHash(t *testing.T) {
	got := SampleRequest(RequestSample{UserID: "u1", MetricsPath: "/compile"}, 100)
	if got == nil || !*got {
		t.Errorf("pct 100 + normal path => true, got %v", got)
	}
}
