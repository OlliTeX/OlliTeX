package updatemanager

import (
	"errors"
	"reflect"
	"testing"
)

// --- lockUpdatesAndDo (oracle group: successful / fetch error / method error)

type lockTest struct {
	order       []string
	fetchErr    error
	methodErr   error
	methodRes   any
	methodCalls []string
	incCalls    []string
}

// manager wires the oracle's ProjectLockManager + fetchAndApply stubs onto a
// Manager; FetchAndApply is the real method (it drains via the GetUpdates
// seam; fetch errors propagate as-is, mirroring the oracle rejects stub).
func (lt *lockTest) manager() *Manager {
	return &Manager{
		GetLock: func(projectID string) (any, error) {
			lt.order = append(lt.order, "getLock:"+projectID)
			return "mock-project-lock-value", nil
		},
		Extend: func(projectID string, token any) error {
			lt.order = append(lt.order, "extend:"+projectID)
			return nil
		},
		Release: func(projectID string, token any) error {
			lt.order = append(lt.order, "release:"+projectID)
			return nil
		},
		GetUpdates: func(projectID string) ([]*Update, error) {
			lt.order = append(lt.order, "getUpdates:"+projectID)
			if lt.fetchErr != nil {
				return nil, lt.fetchErr
			}
			return nil, nil
		},
		GetProjectUpdatesLength: func(projectID string) (int, error) {
			lt.order = append(lt.order, "length:"+projectID)
			return 0, nil
		},
	}
}

func TestLockUpdatesAndDoSuccess(t *testing.T) {
	lt := &lockTest{methodRes: "method result"}
	m := lt.manager()
	method := func(projectID, docID string, args ...any) (any, error) {
		lt.methodCalls = append(lt.methodCalls, projectID+"/"+docID)
		for _, a := range args {
			lt.methodCalls = append(lt.methodCalls, toStr(a))
		}
		return lt.methodRes, nil
	}

	res, err := m.LockUpdatesAndDo(method, umProjectID, umDocID, "argument 1")
	if err != nil {
		t.Fatalf("LockUpdatesAndDo: %v", err)
	}
	if res != "method result" {
		t.Fatalf("result = %v, want the method response", res)
	}
	if !reflect.DeepEqual(lt.methodCalls, []string{umProjectID + "/" + umDocID, "argument 1"}) {
		t.Fatalf("method args = %v, want %v", lt.methodCalls, []string{umProjectID + "/" + umDocID, "argument 1"})
	}
	want := []string{
		"getLock:" + umProjectID,
		"getUpdates:" + umProjectID,
		"extend:" + umProjectID,
		"release:" + umProjectID,
		"length:" + umProjectID, // the fire-and-forget continue
	}
	if !reflect.DeepEqual(lt.order, want) {
		t.Fatalf("order = %v, want %v", lt.order, want)
	}
}

func TestLockUpdatesAndDoFetchError(t *testing.T) {
	fetchErr := errors.New("Something went wrong")
	lt := &lockTest{fetchErr: fetchErr}
	m := lt.manager()

	_, err := m.LockUpdatesAndDo(methodReturning(lt, nil, errors.New("method error")), umProjectID, umDocID, "argument 1")
	if err == nil || err.Error() != fetchErr.Error() {
		t.Fatalf("err = %v, want the fetch error", err)
	}
	// The lock is freed (vendor finally) and — because the lock update is
	// rejected — the background continue does not run.
	want := []string{
		"getLock:" + umProjectID,
		"getUpdates:" + umProjectID,
		"release:" + umProjectID,
	}
	if !reflect.DeepEqual(lt.order, want) {
		t.Fatalf("order = %v, want %v", lt.order, want)
	}
}

func TestLockUpdatesAndDoMethodError(t *testing.T) {
	lt := &lockTest{}
	m := lt.manager()
	methodErr := errors.New("something went wrong")

	_, err := m.LockUpdatesAndDo(methodReturning(lt, nil, methodErr), umProjectID, umDocID, "argument 1")
	if err == nil || err.Error() != methodErr.Error() {
		t.Fatalf("err = %v, want the method error", err)
	}
	// Release still runs for a failed method; the continue is skipped.
	want := []string{
		"getLock:" + umProjectID,
		"getUpdates:" + umProjectID,
		"extend:" + umProjectID,
		"release:" + umProjectID,
	}
	if !reflect.DeepEqual(lt.order, want) {
		t.Fatalf("order = %v, want %v", lt.order, want)
	}
}

// methodReturning builds the oracle's `sinon.stub().resolves/rejects` method.
func methodReturning(lt *lockTest, res any, err error) Method {
	return func(projectID, docID string, args ...any) (any, error) {
		lt.methodCalls = append(lt.methodCalls, projectID+"/"+docID)
		return res, err
	}
}
