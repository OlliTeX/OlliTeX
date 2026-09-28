// C11 oracle — HistoryApiManager HA1.
package historyapimanager

import (
	"context"
	"testing"
	"time"

	"ollitex/go/services/project-history/internal/webapimanager"
)

var cctx = context.Background()

func TestC11_TrueWhenHistoryIdPresent(t *testing.T) {
	d := &Deps{
		WebApi: &webapimanager.Deps{
			GetCachedHistoryId: func(ctx context.Context, p string) (string, error) { return "hid", nil },
		},
	}
	got, err := d.ShouldUseProjectHistory(cctx, "p")
	if err != nil || got != true {
		t.Fatalf("%v %v", got, err)
	}
}

func TestC11_FalseWhenNoHistoryId(t *testing.T) {
	d := &Deps{
		WebApi: &webapimanager.Deps{
			GetCachedHistoryId: func(ctx context.Context, p string) (string, error) { return "", nil },
			FetchJson: func(ctx context.Context, url string, timeout time.Duration) (map[string]any, error) {
				return map[string]any{}, nil
			},
		},
	}
	got, err := d.ShouldUseProjectHistory(cctx, "p")
	if err != nil || got != false {
		t.Fatalf("%v %v", got, err)
	}
}

func TestC11_ErrorPropagates(t *testing.T) {
	boom := &webapimanager.NotFoundError{Msg: "got a 404 from web api"}
	d := &Deps{
		WebApi: &webapimanager.Deps{
			GetCachedHistoryId: func(ctx context.Context, p string) (string, error) { return "", nil },
			FetchJson: func(ctx context.Context, url string, timeout time.Duration) (map[string]any, error) {
				return nil, &webapimanager.RequestFailedError{Status: 404, Msg: "nf"}
			},
		},
	}
	got, err := d.ShouldUseProjectHistory(cctx, "p")
	if err == nil {
		t.Fatal("want error")
	}
	_, ok := err.(*webapimanager.NotFoundError)
	if !ok || got != false {
		t.Fatalf("%v %T", got, err)
	}
	_ = boom
}

func TestC11_Unwired(t *testing.T) {
	d := &Deps{}
	if _, err := d.ShouldUseProjectHistory(cctx, "p"); err == nil {
		t.Fatal("want seam error")
	}
}
