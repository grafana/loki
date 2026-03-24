//go:build windows || plan9 || js

package tsdb

// madviseWillNeed is a no-op on platforms without madvise support.
func madviseWillNeed(_ []byte) error {
	return nil
}
