package updatemanager

import (
	"errors"
	"reflect"
	"testing"

	"document-updater/internal/rangesmanager"
)

// ---------------------------------------------------------------------------
// Mirrors Node test/unit/js/UpdateManager/UpdateManagerTests.js 1:1 (the
// 990-LOC oracle). Every pinned value below is an oracle value.
//
// Seam recording, not stubbing: the oracle's sinon stubs (getLock /
// getPendingProjectUpdates / ...) map to the exported func seams on
// Manager; each test records calls (order + args) and asserts on the
// record. The oracle's "calledWith X" pins become "captured == X";
// "calledAfter/calledBefore" becomes an ordering check on the record.
// ---------------------------------------------------------------------------

const (
	umProjectID = "project-id-123"
	umDocID     = "document-id-123"
	umPathname  = "/a/b/c.tex"
)

var umHistoryID = "history-id-123"

func us(v string) *string { return &v }

func iv(v int) *int { return &v }

// umUpdate mirrors the oracle applyUpdate fixture
// `{op: [{p: 42, i: 'foo'}], meta: {user_id: 'last-author-fake-id'}}`.
func umUpdate() *Update {
	return &Update{
		Doc:  umDocID,
		Op:   []map[string]any{{"p": 42, "i": "foo"}},
		Meta: map[string]any{"user_id": "last-author-fake-id"},
	}
}

// umDocInfo mirrors the oracle getDoc fixture
// (lines ['original','lines'], version 34, pathname, historyRangesSupport).
func umDocInfo(histRS bool) DocInfo {
	return DocInfo{
		Lines:                []string{"original", "lines"},
		Version:              34,
		Ranges:               &rangesmanager.Ranges{},
		Pathname:             umPathname,
		ProjectHistoryID:     &umHistoryID,
		HistoryRangesSupport: histRS,
		Type:                 "sharejs-text-ot",
	}
}

// --- processOutstandingUpdatesWithLock --------------------------------------

func TestProcessSuccess(t *testing.T) {
	var order []string
	m := &Manager{
		GetLock: func(projectID string) (any, error) {
			order = append(order, "getLock:"+projectID)
			return "mock-project-lock-value", nil
		},
		GetUpdates: func(projectID string) ([]*Update, error) {
			order = append(order, "getUpdates:"+projectID)
			return nil, nil
		},
		Release: func(projectID string, token any) error {
			order = append(order, "release:"+projectID+":"+toStr(token))
			return nil
		},
		GetProjectUpdatesLength: func(projectID string) (int, error) {
			order = append(order, "length:"+projectID)
			return 0, nil
		},
	}

	if err := m.Process(umProjectID); err != nil {
		t.Fatalf("Process: %v", err)
	}

	want := []string{
		"getLock:" + umProjectID,                              // acquire the project lock
		"getUpdates:" + umProjectID,                           // drain the per-project queue
		"release:" + umProjectID + ":mock-project-lock-value", // free it
		"length:" + umProjectID,                               // continue processing
	}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

func TestProcessFetchError(t *testing.T) {
	const errMsg = "Something went wrong"
	fetchErr := errors.New(errMsg)
	var order []string
	m := &Manager{
		GetLock: func(projectID string) (any, error) {
			order = append(order, "getLock:"+projectID)
			return "mock-project-lock-value", nil
		},
		GetUpdates: func(projectID string) ([]*Update, error) {
			order = append(order, "getUpdates:"+projectID)
			return nil, fetchErr
		},
		Release: func(projectID string, token any) error {
			order = append(order, "release:"+projectID)
			return nil
		},
	}

	err := m.Process(umProjectID)
	if err == nil || err.Error() != errMsg {
		t.Fatalf("Process err = %v, want the fetch error", err)
	}
	// The lock is released even when the drain fails (vendor finally),
	// and the error propagates before any continue.
	want := []string{
		"getLock:" + umProjectID,
		"getUpdates:" + umProjectID,
		"release:" + umProjectID,
	}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

func TestProcessReleaseErrorBeatsFetchError(t *testing.T) {
	fetchErr := errors.New("fetch error")
	releaseErr := errors.New("release error")
	m := &Manager{
		GetLock: func(projectID string) (any, error) { return "tok", nil },
		GetUpdates: func(projectID string) ([]*Update, error) {
			return nil, fetchErr
		},
		Release: func(projectID string, token any) error { return releaseErr },
	}

	if err := m.Process(umProjectID); !errors.Is(err, releaseErr) {
		t.Fatalf("Process err = %v, want the release error", err)
	}
}

func TestProcessReleaseErrorPropagates(t *testing.T) {
	releaseErr := errors.New("release error")
	m := &Manager{
		GetLock:    func(projectID string) (any, error) { return "tok", nil },
		GetUpdates: func(projectID string) ([]*Update, error) { return nil, nil },
		Release:    func(projectID string, token any) error { return releaseErr },
	}

	if err := m.Process(umProjectID); !errors.Is(err, releaseErr) {
		t.Fatalf("Process err = %v, want the release error", err)
	}
}

// --- fetchAndApplyProjectUpdates --------------------------------------------

func TestFetchAndApplyUpdatesCarryingDocIDs(t *testing.T) {
	var applied []string
	m := &Manager{
		GetUpdates: func(projectID string) ([]*Update, error) {
			return []*Update{
				{Doc: "doc-1", Op: []map[string]any{{"p": 0, "i": "a"}}},
				{Doc: "doc-2", Op: []map[string]any{{"p": 1, "i": "b"}}},
			}, nil
		},
		GetDoc: func(projectID, docID string) (DocInfo, error) {
			applied = append(applied, projectID+"/"+docID)
			return DocInfo{Lines: []string{"original", "lines"}, Version: 34, Pathname: umPathname, Type: "sharejs-text-ot"}, nil
		},
		IsHistoryOT: func(u *Update) bool { return false },
	}

	if err := m.FetchAndApply(umProjectID); err != nil {
		t.Fatalf("FetchAndApply: %v", err)
	}
	// Each update is applied to the doc it carries, in queue order.
	want := []string{umProjectID + "/doc-1", umProjectID + "/doc-2"}
	if !reflect.DeepEqual(applied, want) {
		t.Fatalf("applied = %v, want %v", applied, want)
	}
}

func TestFetchAndApplyHistoryOTUpdate(t *testing.T) {
	var historyApplied []string
	shareJSCalled := false
	// the oracle routes via isHistoryOTEditOperationUpdate(...).returns(true)
	isHistoryOT := func(u *Update) bool { return u != nil && u.Doc == "doc-1" }
	m := &Manager{
		GetUpdates:  func(projectID string) ([]*Update, error) { return []*Update{{Doc: "doc-1"}}, nil },
		IsHistoryOT: isHistoryOT,
		HistoryOTApply: func(projectID, docID string, u *Update) error {
			historyApplied = append(historyApplied, projectID+"/"+docID)
			return nil
		},
		ShareJsApply: func(projectID, docID string, u *Update, lines []string, version int) ([]string, int, []any, error) {
			shareJSCalled = true
			return nil, 0, nil, nil
		},
	}

	if err := m.FetchAndApply(umProjectID); err != nil {
		t.Fatalf("FetchAndApply: %v", err)
	}
	want := []string{umProjectID + "/doc-1"}
	if !reflect.DeepEqual(historyApplied, want) {
		t.Fatalf("history-OT applied = %v, want %v", historyApplied, want)
	}
	if shareJSCalled {
		t.Fatal("the ShareJS path must not be taken for a history-OT update")
	}
}

func TestFetchAndApplyNoUpdates(t *testing.T) {
	anyApply := false
	m := &Manager{
		GetUpdates: func(projectID string) ([]*Update, error) { return nil, nil },
		ShareJsApply: func(projectID, docID string, u *Update, lines []string, version int) ([]string, int, []any, error) {
			anyApply = true
			return nil, 0, nil, nil
		},
		HistoryOTApply: func(projectID, docID string, u *Update) error {
			anyApply = true
			return nil
		},
	}

	if err := m.FetchAndApply(umProjectID); err != nil {
		t.Fatalf("FetchAndApply: %v", err)
	}
	if anyApply {
		t.Fatal("no update must be applied when the queue is empty")
	}
}

// --- continueProcessingUpdatesWithLock ---------------------------------------

func TestContinueWithOutstandingUpdates(t *testing.T) {
	var order []string
	call := 0
	m := &Manager{
		GetProjectUpdatesLength: func(projectID string) (int, error) {
			call++
			order = append(order, "length:"+projectID+":"+"3"+"#"+string(rune('0'+call)))
			if call == 1 {
				return 3, nil
			}
			return 0, nil
		},
		GetLock: func(projectID string) (any, error) {
			order = append(order, "getLock:"+projectID)
			return "tok", nil
		},
		GetUpdates: func(projectID string) ([]*Update, error) {
			order = append(order, "getUpdates:"+projectID)
			return nil, nil
		},
		Release: func(projectID string, token any) error {
			order = append(order, "release:"+projectID)
			return nil
		},
	}

	if err := m.Continue(umProjectID); err != nil {
		t.Fatalf("Continue: %v", err)
	}
	// length > 0 re-enters Process (re-lock, drain, release); the
	// inner continue then length=0 stops.
	want := []string{
		"length:" + umProjectID + ":3#1",
		"getLock:" + umProjectID,
		"getUpdates:" + umProjectID,
		"release:" + umProjectID,
		"length:" + umProjectID + ":3#2",
	}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
}

func TestContinueNoOutstandingUpdates(t *testing.T) {
	m := &Manager{
		GetProjectUpdatesLength: func(projectID string) (int, error) {
			if projectID != umProjectID {
				t.Fatalf("length project = %q, want %q", projectID, umProjectID)
			}
			return 0, nil
		},
		GetLock: func(projectID string) (any, error) {
			t.Fatal("Process must not re-enter when nothing is outstanding")
			return nil, nil
		},
	}

	if err := m.Continue(umProjectID); err != nil {
		t.Fatalf("Continue: %v", err)
	}
}

func toStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return "nil"
}
