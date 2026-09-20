// Package statsmanager ports services/clsi/app/js/StatsManager.js.
//
// Node parity notes:
//
// sampleByHash(key, samplePercentage) returns false for percentage <= 0.
// Otherwise it MD5s the key, reads the first 4 bytes big-endian, maps to a
// percentile [0..99], and returns percentile < percentage (strict, matching
// upstream).
//
// "sampleRequest" ports the JS optional return value: nil when the request
// has no user_id, is excluded by Metrics.shouldSkipMetrics(path), or the
// sampling percentage is <= 0.
package statsmanager

import (
	"crypto/md5"
	"math"

	"clsi/metrics"
)

// SampleByHash ports sampleByHash: a stable sample over a keyspace with a
// given sample percentage (see the Node doc block).
func SampleByHash(key string, samplePercentage int) bool {
	if samplePercentage <= 0 {
		return false
	}
	sum := md5.Sum([]byte(key))
	// JS: hash.readUInt32BE(0) — first four bytes big-endian.
	hashValue := uint32(sum[0])<<24 | uint32(sum[1])<<16 | uint32(sum[2])<<8 | uint32(sum[3])
	// JS: Math.floor((hashValue / 0xffffffff) * 100) — float, then floor.
	percentile := int(math.Floor(float64(hashValue) / 0xFFFFFFFF * 100))
	return percentile < samplePercentage
}

// SampleRequestResult is the (optional) sampling decision for one request.
// nil => the request is NOT sampled (no user_id, excluded path, or pct <= 0).
// true/false => the stable sampleByHash result.
type SampleRequestResult = *bool

// RequestSample mirrors the JS request surface used by sampleRequest.
type RequestSample struct {
	UserID      string
	MetricsPath string
}

// SampleRequest ports sampleRequest(request, samplingPercentage).
// Returns nil when the request has no user_id, is excluded by
// Metrics.ShouldSkipMetrics(path), or the sampling percentage is <= 0.
func SampleRequest(req RequestSample, samplingPercentage int) SampleRequestResult {
	if req.UserID == "" {
		return nil
	}
	if metrics.ShouldSkipMetrics(req.MetricsPath) {
		return nil
	}
	if samplingPercentage <= 0 {
		return nil
	}
	res := SampleByHash(req.UserID, samplingPercentage)
	return &res
}
