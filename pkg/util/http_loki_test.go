package util

// This file is separated from `http_test.go` to differentiate from the original `http_test.go` file forked from Cortex.

import (
	"bytes"
	"testing"
)

func BenchmarkDecompressFromBuffer(b *testing.B) {
	buf := bytes.Buffer{}
	buf.Grow(100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decompressFromBuffer(&buf, 1000, RawSnappy, nil) //nolint:errcheck
	}
}
