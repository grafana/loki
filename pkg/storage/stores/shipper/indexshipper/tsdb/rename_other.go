//go:build !windows

package tsdb

// isTransientRenameErr is only meaningful on Windows, where open handles
// block renames. Elsewhere a failed rename is never retried.
func isTransientRenameErr(error) bool { return false }
