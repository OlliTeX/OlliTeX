package contentcacheworker

import (
	"testing"

	"clsi/contentcachemanager"
)

// TestWorkerUpdateForwards verifies the worker is a direct pass-through to
// contentcachemanager.Update (mirrors
// `workerpool.worker(ContentCacheManager.promises)` where the worker host
// invokes promises.update — here contentcachemanager.Update).
func TestWorkerUpdateForwards(t *testing.T) {
	w := Worker{}
	res, err := w.Update(contentcachemanager.UpdateArgs{
		// pdfSize below minChunkSize => early-return with empty ranges (no
		// dir required).
		PdfSize:               0,
		PdfCachingMinChunkSize: 1,
	})
	if err != nil {
		t.Fatalf("worker: %v", err)
	}
	if res == nil {
		t.Fatalf("result nil")
	}
	if len(res.ContentRanges) != 0 {
		t.Fatalf("ContentRanges: got %d want 0", len(res.ContentRanges))
	}
	if len(res.NewContentRanges) != 0 {
		t.Fatalf("NewContentRanges: got %d want 0", len(res.NewContentRanges))
	}
	if res.ReclaimedSpace != 0 {
		t.Fatalf("ReclaimedSpace: got %d want 0", res.ReclaimedSpace)
	}
}
