package search

import (
	"bytes"

	"golang.org/x/sys/cpu"
)

func init() {
	if cpu.X86.HasAVX512 {
		index = indexAvx512
	} else {
		index = func(haystack []byte, needle []byte) int64 { return int64(bytes.Index(haystack, needle)) }
	}
}

