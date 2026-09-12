package filestore

import (
	"time"
)

// --- ImageOptimiser (1:1 with app/js/ImageOptimiser.js) --------------------

func fseCompressPng(localPath string) {
	// 1:1: optipng with a 30s SIGKILL timeout; a timeout/absence is a warning,
	// and a failure never aborts the request in the local-CE build.
	_, _ = fseExec([]string{"optipng", localPath}, 30*time.Second)
}
