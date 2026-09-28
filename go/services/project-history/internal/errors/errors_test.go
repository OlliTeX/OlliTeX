package errors

import "testing"

// Covers all 9 constructor sites (Node call sites verified 2026-09-19:
// SyncManager.js L142/L939, UpdatesProcessor.js L433, HttpController usage).

func TestConstructors(t *testing.T) {
	cases := []struct {
		e    *Error
		kind Kind
		msg  string
	}{
		{NotFound("missing"), KindNotFound, "missing"},
		{BadRequest("bad"), KindBadRequest, "bad"},
		{Sync("boom"), KindSync, "boom"},
		{SyncOngoing(SyncOngoingErrorMessage), KindSyncOngoing, "sync ongoing"},
		{OpsOutOfOrder("doc version out of order"), KindOpsOutOfOrder, "doc version out of order"},
		{InconsistentChunk("chunk"), KindInconsistentChunk, "chunk"},
		{UpdateWithUnknownFormat("fmt"), KindUpdateUnknownFormat, "fmt"},
		{UnexpectedOpType("op"), KindUnexpectedOpType, "op"},
		{TooManyRequests("slow"), KindTooManyRequests, "slow"},
		{NeedFullProjectStructureResync("v12"), KindNeedFullResync, "v12"},
		{FileContentEmpty("empty"), KindFileContentEmpty, "empty"},
		{LockTimeout("ph:lock:1"), Kind("Timeout"), "Timeout"},
	}
	for _, c := range cases {
		if c.e.Kind != c.kind {
			t.Errorf("kind: got %s want %s", c.e.Kind, c.kind)
		}
		if c.e.Error() != c.msg {
			t.Errorf("msg: got %q want %q", c.e.Error(), c.msg)
		}
	}
	if !LockTimeout("k").Timeout || LockTimeout("k").Key != "k" {
		t.Error("LockTimeout must carry Timeout flag + Key")
	}
	if SyncOngoingErrorMessage != "sync ongoing" {
		t.Error(" SYNC_ONGOING_ERROR_MESSAGE drift")
	}
}
