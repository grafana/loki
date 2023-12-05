package search

import (
	"bytes"

	"golang.org/x/sys/cpu"
)

func init() {
	if cpu.ARM64.HasASIMD {
		index = indexNeon
	} else {
		index = func(haystack []byte, needle []byte) int64 { return int64(bytes.Index(haystack, needle)) }
	}
}

