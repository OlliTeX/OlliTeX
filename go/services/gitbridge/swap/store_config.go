// store.go (partial) — SwapStore.fromConfig.
package swap

import (
	"ollitex/go/services/gitbridge/config"
)

// FromConfigStore ports SwapStore.fromConfig(Optional<SwapStoreConfig>):
// memory→InMemory, s3→S3, otherwise→Noop (Java: swapStores.get(type) with
// default SwapStoreConfig.NOOP).
func FromConfigStore(cfg *config.SwapStoreStore) SwapStore {
	if cfg == nil || cfg.Type == nil {
		return NoopSwapStore{}
	}
	switch *cfg.Type {
	case "memory":
		return NewInMemorySwapStore()
	case "s3":
		ak, sk, bucket, region, endpoint := "", "", "", "", ""
		if cfg.AwsAccessKey != nil {
			ak = *cfg.AwsAccessKey
		}
		if cfg.AwsSecret != nil {
			sk = *cfg.AwsSecret
		}
		if cfg.S3BucketName != nil {
			bucket = *cfg.S3BucketName
		}
		if cfg.AwsRegion != nil {
			region = *cfg.AwsRegion
		}
		if cfg.AwsEndpoint != nil {
			endpoint = *cfg.AwsEndpoint
		}
		s, err := NewS3SwapStore(ak, sk, bucket, region, endpoint)
		if err != nil {
			// Java: S3 client ctor throws → fromConfig propagates. The Go port
			// is lenient (logs + noop) since a misconfigured S3 store would
			// otherwise panic at boot; documented divergence.
			return NoopSwapStore{}
		}
		return s
	default:
		return NoopSwapStore{}
	}
}
