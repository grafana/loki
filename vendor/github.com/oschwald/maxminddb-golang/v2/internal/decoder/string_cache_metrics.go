//go:build !cachemetrics

package decoder

// Empty hooks inline away; cachemetrics builds replace them with counters.
func recordStringCacheHit()       {}
func recordStringCacheMiss()      {}
func recordStringCacheAdmission() {}
