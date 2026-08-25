package util //nolint:revive

import (
	"github.com/cespare/xxhash/v2"
)

// sampleHashBufferSize is the largest labels+line pair that SampleHasher copies into its inline
// buffer. Below it, hashing one contiguous slice beats feeding xxhash in three writes. Above it
// the copy costs more than it saves and the streaming path is faster.
//
// BenchmarkSampleHashThreshold measures the two against each other, so the value can be checked
// again on other hardware. The crossover moves with the CPU, so this is one machine's answer
// rather than a universal one: it sat near 800 bytes on an Apple M3 Pro. Being off by a little
// only picks the slower of two paths that are within a few percent of each other there, so the
// value does not need to be exact.
const sampleHashBufferSize = 768

// SampleHasher computes sample deduplication hashes without allocating.
// The zero value is ready to use. A SampleHasher is not safe for concurrent use.
type SampleHasher struct {
	// Because the streaming path handles everything larger, the buffer never has to grow: a
	// SampleHasher keeps a fixed size no matter how long the lines are.
	buf    [sampleHashBufferSize]byte
	digest xxhash.Digest
}

// Hash returns the deduplication hash of the sample that lblString and line identify.
func (h *SampleHasher) Hash(lblString string, line []byte) uint64 {
	if len(lblString)+1+len(line) <= len(h.buf) {
		b := append(h.buf[:0], lblString...)
		b = append(b, ':')
		b = append(b, line...)
		return xxhash.Sum64(b)
	}

	h.digest.Reset()
	_, _ = h.digest.WriteString(lblString)
	_, _ = h.digest.WriteString(":")
	_, _ = h.digest.Write(line)
	return h.digest.Sum64()
}

// UniqueSampleHash returns the deduplication hash of the sample that lblString and line
// identify. Callers on a hot path should hold a SampleHasher and reuse it.
func UniqueSampleHash(lblString string, line []byte) uint64 {
	var h SampleHasher
	return h.Hash(lblString, line)
}
