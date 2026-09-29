package otc

import "ollitex/go/libraries/otpure"

// safe_pathname.go — the public surface (Clean/CleanDebug/IsClean/IsCleanDebug)
// and its machinery moved to ollitex/go/libraries/otpure/pathname.go
// (d5dd23dd S1a). Users: otpure.Clean et al.

// Re-exports of the otpure implementation, preserving the documented public
// surface (Clean/CleanDebug/IsClean/IsCleanDebug) for consumers that import
// otc directly (e.g. the safepathname oracle suite).
func CleanDebug(pathname string) (string, string) { return otpure.CleanDebug(pathname) }

func IsCleanDebug(pathname string) (bool, string) { return otpure.IsCleanDebug(pathname) }
