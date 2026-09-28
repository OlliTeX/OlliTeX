// C10 oracle — WebApiManager vendor contracts W1–W5 + C11 HA1.
package webapimanager

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

var cctx = context.Background()

func TestC10_GetHistoryIdCacheHit(t *testing.T) {
	var incs []string
	d := &Deps{
		GetCachedHistoryId: func(ctx context.Context, p string) (string, error) { return "cached-hid", nil },
		Inc:                func(name string) { incs = append(incs, name) },
	}
	got, err := d.GetHistoryId(cctx, "p")
	if err != nil || got != "cached-hid" {
		t.Fatalf("%q %v", got, err)
	}
	if len(incs) != 2 || incs[0] != "history_id_cache_requests_total" || incs[1] != "history_id_cache_hits_total" {
		t.Fatalf("metrics: %v", incs)
	}
}

func TestC10_GetHistoryIdMissCaches(t *testing.T) {
	set := 0
	d := &Deps{WebAPIURL: "http://web"}
	d.GetCachedHistoryId = func(ctx context.Context, p string) (string, error) { return "", nil }
	d.FetchJson = func(ctx context.Context, url string, timeout time.Duration) (map[string]any, error) {
		if url != "http://web/project/p/details" {
			t.Fatalf("url: %q", url)
		}
		if timeout != DetailsFetchTimeout {
			t.Fatalf("timeout: %v", timeout)
		}
		return map[string]any{"overleaf": map[string]any{"history": map[string]any{"id": "hid-1"}}}, nil
	}
	d.SetCachedHistoryId = func(ctx context.Context, p, h string) error {
		set++
		return nil
	}
	got, err := d.GetHistoryId(cctx, "p")
	if err != nil || got != "hid-1" {
		t.Fatalf("%q %v", got, err)
	}
	if set != 1 {
		t.Fatalf("set calls: %d", set)
	}
}

func TestC10_GetHistoryIdNoHistoryKey(t *testing.T) {
	d := &Deps{
		GetCachedHistoryId: func(ctx context.Context, p string) (string, error) { return "", nil },
		FetchJson: func(ctx context.Context, url string, timeout time.Duration) (map[string]any, error) {
			return map[string]any{"overleaf": map[string]any{}}, nil
		},
	}
	got, err := d.GetHistoryId(cctx, "p")
	if err != nil || got != "" {
		t.Fatalf("vendor: undefined historyId → %q %v", got, err)
	}
}

func TestC10_Details404Wrapped(t *testing.T) {
	d := &Deps{
		GetCachedHistoryId: func(ctx context.Context, p string) (string, error) { return "", nil },
		FetchJson: func(ctx context.Context, url string, timeout time.Duration) (map[string]any, error) {
			return nil, &RequestFailedError{Status: 404, Msg: "not found"}
		},
	}
	_, err := d.GetHistoryId(cctx, "p")
	nfe, ok := err.(*NotFoundError)
	if !ok || nfe.Msg != "got a 404 from web api" || nfe.Cause == nil {
		t.Fatalf("404 wrap: %v (%T)", err, err)
	}
}

func TestC10_DetailsRetryOnceThenThrow(t *testing.T) {
	var sleeps int
	tries := 0
	boom := errors.New("boom-500")
	d := &Deps{
		GetCachedHistoryId: func(ctx context.Context, p string) (string, error) { return "", nil },
		Sleep:              func(time.Duration) { sleeps++ },
		FetchJson: func(ctx context.Context, url string, timeout time.Duration) (map[string]any, error) {
			tries++
			return nil, boom
		},
	}
	defer SetRetryTimeoutMs(5000)
	_, err := d.GetHistoryId(cctx, "p")
	if !errors.Is(err, boom) {
		t.Fatalf("second failure must rethrow the ORIGINAL error: %v", err)
	}
	if tries != 2 || sleeps != 1 {
		t.Fatalf("tries=%d sleeps=%d (vendor: ONE retry after RETRY_TIMEOUT_MS)", tries, sleeps)
	}
}

func TestC10_ResyncBodyOnlyTruthyKeys(t *testing.T) {
	var gotBody map[string]any
	d := &Deps{WebAPIURL: "http://web"}
	d.FetchNothing = func(ctx context.Context, url string, body map[string]any, timeout time.Duration) (*Response, error) {
		gotBody = body
		if url != "http://web/project/p/history/resync" {
			t.Fatalf("url: %q", url)
		}
		if timeout != ResyncTimeout {
			t.Fatalf("timeout: %v", timeout)
		}
		return &Response{Status: 200}, nil
	}
	err := d.RequestResync(cctx, "p", map[string]any{
		"historyRangesMigration":     true,
		"resyncProjectStructureOnly": false, // falsy → excluded (vendor truthiness)
		"other":                      "x",   // unknown key → ignored
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, has := gotBody["resyncProjectStructureOnly"]; has {
		t.Fatalf("falsy key must be omitted: %#v", gotBody)
	}
	if _, has := gotBody["historyRangesMigration"]; !has {
		t.Fatalf("truthy key must be present: %#v", gotBody)
	}
}

func TestC10_Resync404Wrapped(t *testing.T) {
	d := &Deps{
		FetchNothing: func(ctx context.Context, url string, body map[string]any, timeout time.Duration) (*Response, error) {
			return &Response{Status: 404}, &RequestFailedError{Status: 404, Msg: "nf"}
		},
	}
	err := d.RequestResync(cctx, "p", nil)
	nfe, ok := err.(*NotFoundError)
	if !ok || nfe.Msg != "got a 404 from web api" {
		t.Fatalf("404 wrap: %v", err)
	}
}

func TestC10_ResyncOtherErrorRethrown(t *testing.T) {
	boom := &RequestFailedError{Status: 503, Msg: "unavailable"}
	d := &Deps{
		FetchNothing: func(ctx context.Context, url string, body map[string]any, timeout time.Duration) (*Response, error) {
			return nil, boom
		},
	}
	err := d.RequestResync(cctx, "p", nil)
	if err != boom || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("other errors rethrown untouched: %v", err)
	}
}

func TestC10_SetRetryTimeoutMs(t *testing.T) {
	defer SetRetryTimeoutMs(5000)
	SetRetryTimeoutMs(7)
	if RETRY_TIMEOUT_MS != 7*time.Millisecond {
		t.Fatalf("module state: %v", RETRY_TIMEOUT_MS)
	}
}
