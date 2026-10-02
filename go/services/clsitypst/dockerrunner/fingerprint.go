package dockerrunner

import (
	"crypto/md5" //nolint:gosec — non-security fingerprint
	"encoding/json"
	"fmt"
)

// fingerprint mirrors Node `DockerRunner._fingerprintContainer`:
//   - Node: JSON.stringify(options) in JS key insertion order, then MD5 hex.
//   - Go: json.Marshal (deterministic for this struct: fixed field order,
//     no maps; see package doc divergence 1). Names are only compared
//     within this Go deployment.
func fingerprint(opts CreateOpts) string {
	j, _ := json.Marshal(opts)
	sum := md5.Sum(j)
	return fmt.Sprintf("%x", sum[:])
}
