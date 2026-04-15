//go:build windows
// +build windows

package utils

import "strings"

func pathPartEqual(a, b string) bool {
	return strings.EqualFold(a, b)
}
