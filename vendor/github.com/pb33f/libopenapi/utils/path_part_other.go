//go:build !windows
// +build !windows

package utils

func pathPartEqual(a, b string) bool {
	return a == b
}
