package core

import otc "ollitex/go/libraries/otc"

// The safe-pathname algorithm (ports safe_pathname.js: clean/cleanDebug,
// isClean/isCleanDebug, BAD_CHAR_RX, BAD_FILE_RX, BLOCKED_FILE_RX,
// MAX_PATH=1024, path.normalize) is reused from the shared `go/libraries/otc`
// package so both sides maintain one implementation. This file is the small
// in-package surface the engine (FileMap.checkPathname) calls.
//
// Note: upstream's char test rejects supplementary (surrogate-pair, > 0xFFFF)
// code points and passes the replacement character U+FFFD through; Go strings
// receive JSON-decoded surrogates as U+FFFD, so replacement chars are accepted
// here — a deliberate upstream behavior change versus the previous local
// approximation, recorded so the accepted/dirty set is auditable.

// Clean — safePathname.clean.
func Clean(pathname string) string { return otc.Clean(pathname) }

// IsClean — safePathname.isClean.
func IsClean(pathname string) bool { return otc.IsClean(pathname) }

// IsCleanDebug — (isClean, reason).
func IsCleanDebug(pathname string) (bool, string) { return otc.IsCleanDebug(pathname) }
