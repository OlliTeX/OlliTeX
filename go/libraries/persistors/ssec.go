package persistors

import (
	"crypto/md5"
	"encoding/base64"
)

// SSECOptions mirrors S3Persistor.js `class SSECOptions` — customer-supplied
// encryption key + its MD5 + the AES256 algorithm, with getPutOptions /
// getGetOptions / getCopyOptions returning the AWS parameter maps. Node
// keeps the key in a private field (not serialized/printed); Go hides it the
// same way (lower-case accessors only via these methods).
type SSECOptions struct {
	Key       []byte
	keyMD5    string
	algorithm string
}

// NewSSECOptions mirrors `new SSECOptions(keyAsBuffer)`.
func NewSSECOptions(key []byte) *SSECOptions {
	sum := md5.Sum(key)
	return &SSECOptions{
		Key:       key,
		keyMD5:    base64.StdEncoding.EncodeToString(sum[:]),
		algorithm: "AES256",
	}
}

// GetKey returns the raw key bytes (Node: SSECustomerKey value).
func (s *SSECOptions) GetKey() []byte { return s.Key }

// GetMD5 returns the base64 MD5 digest (Node: SSECustomerKeyMD5 value).
func (s *SSECOptions) GetMD5() string { return s.keyMD5 }

// Put mirrors `getPutOptions()` (upload parameters).
func (s *SSECOptions) Put() map[string]any {
	return map[string]any{
		"SSECustomerKey":       s.Key,
		"SSECustomerKeyMD5":    s.keyMD5,
		"SSECustomerAlgorithm": s.algorithm,
	}
}

// Get mirrors `getGetOptions()`.
func (s *SSECOptions) Get() map[string]any {
	return map[string]any{
		"SSECustomerKey":       s.Key,
		"SSECustomerKeyMD5":    s.keyMD5,
		"SSECustomerAlgorithm": s.algorithm,
	}
}

// Copy mirrors `getCopyOptions()` (copy-source SSE parameters).
func (s *SSECOptions) Copy() map[string]any {
	return map[string]any{
		"CopySourceSSECustomerKey":       s.Key,
		"CopySourceSSECustomerKeyMD5":    s.keyMD5,
		"CopySourceSSECustomerAlgorithm": s.algorithm,
	}
}
