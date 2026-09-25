// Package otcbridge pins the module wiring to the shared Go libraries
// (module `ollitex`, replace'd to the repo root in go.mod). Consumers of
// `ollitex/go/libraries/otc` (the Node `overleaf-editor-core` port) keep the
// bridge thin: this package exists so the module path, replace directive, and
// the otc symbols this service relies on stay a single greppable seam.
package otcbridge

import otc "ollitex/go/libraries/otc"

// Symbol pins — a compile error here means upstream `go/libraries` moved the
// seam this service already depends on (Phase A blob utils, safe-pathname,
// length limits).
var (
	otcBlobHash         = otc.BlobHashFromBuffer
	otcBlobHashString   = otc.BlobHashFromString
	otcEmptyHash        = otc.EmptyHash
	otcHexHashPattern   = otc.HexHashRxString
	otcStrLenOfBuffer   = otc.GetStringLengthOfBuffer
	otcContainsNonBmp   = otc.ContainsNonBmpChars
	otcSafePathClean    = otc.Clean
	otcSafePathIsClean  = otc.IsClean
	otcSafePathIsCleanD = otc.IsCleanDebug
	otcMaxStrLen        = otc.MaxStringLength
)

var _ = otcBlobHash
var _ = otcBlobHashString
var _ = otcEmptyHash
var _ = otcHexHashPattern
var _ = otcStrLenOfBuffer
var _ = otcContainsNonBmp
var _ = otcSafePathClean
var _ = otcSafePathIsClean
var _ = otcSafePathIsCleanD
var _ = otcMaxStrLen
