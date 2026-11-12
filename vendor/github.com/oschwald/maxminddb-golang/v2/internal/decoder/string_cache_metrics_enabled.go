//go:build cachemetrics

package decoder

import "sync/atomic"

var (
	stringCacheBenchHits       atomic.Uint64
	stringCacheBenchMisses     atomic.Uint64
	stringCacheBenchAdmissions atomic.Uint64
)

func recordStringCacheHit()       { stringCacheBenchHits.Add(1) }
func recordStringCacheMiss()      { stringCacheBenchMisses.Add(1) }
func recordStringCacheAdmission() { stringCacheBenchAdmissions.Add(1) }

// StringCacheBenchmarkStats counts eligible hits and misses across the process.
// Only cachemetrics builds include these counters.
func StringCacheBenchmarkStats() (hits, misses uint64) {
	return stringCacheBenchHits.Load(), stringCacheBenchMisses.Load()
}
