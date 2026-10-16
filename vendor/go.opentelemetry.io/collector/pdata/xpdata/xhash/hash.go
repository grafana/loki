// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package xhash // import "go.opentelemetry.io/collector/pdata/xpdata/xhash"

import (
	"encoding/binary"
	"math"
	"slices"
	"strings"
	"sync"

	"github.com/cespare/xxhash/v2"

	"go.opentelemetry.io/collector/pdata/pcommon"
)

var (
	extraByte       = []byte{'\xf3'}
	keyPrefix       = []byte{'\xf4'}
	valEmpty        = []byte{'\xf5'}
	valBytesPrefix  = []byte{'\xf6'}
	valStrPrefix    = []byte{'\xf7'}
	valBoolTrue     = []byte{'\xf8'}
	valBoolFalse    = []byte{'\xf9'}
	valIntPrefix    = []byte{'\xfa'}
	valDoublePrefix = []byte{'\xfb'}
	valMapPrefix    = []byte{'\xfc'}
	valMapSuffix    = []byte{'\xfd'}
	valSlicePrefix  = []byte{'\xfe'}
	valSliceSuffix  = []byte{'\xff'}

	emptyHash = [16]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
)

// HashOption is a function that sets an option on the hash calculation.
type HashOption func(*hashWriter)

// WithMap adds a map to the hash calculation.
func WithMap(m pcommon.Map) HashOption {
	return func(hw *hashWriter) {
		hw.writeMapHash(m)
	}
}

// WithValue adds a value to the hash calculation.
func WithValue(v pcommon.Value) HashOption {
	return func(hw *hashWriter) {
		hw.writeValueHash(v)
	}
}

// WithString adds a string to the hash calculation.
func WithString(s string) HashOption {
	return func(hw *hashWriter) {
		hw.byteBuf = append(hw.byteBuf, valStrPrefix...)
		hw.byteBuf = append(hw.byteBuf, s...)
	}
}

// mapEntry is a key and its value collected from a pcommon.Map so that entries can be
// sorted by key without looking each value up again.
type mapEntry struct {
	key string
	val pcommon.Value
}

type hashWriter struct {
	byteBuf    []byte
	entriesBuf []mapEntry
}

func newHashWriter() *hashWriter {
	return &hashWriter{
		byteBuf:    make([]byte, 0, 512),
		entriesBuf: make([]mapEntry, 0, 16),
	}
}

var hashWriterPool = &sync.Pool{
	New: func() any { return newHashWriter() },
}

// Hash generates a hash for the provided options and returns the computed hash as a [16]byte.
func Hash(opts ...HashOption) [16]byte {
	if len(opts) == 0 {
		return emptyHash
	}

	hw := hashWriterPool.Get().(*hashWriter)
	defer hashWriterPool.Put(hw)
	hw.byteBuf = hw.byteBuf[:0]

	for _, o := range opts {
		o(hw)
	}

	return hw.hashSum128()
}

// Hash64 generates a hash for the provided options and returns the computed hash as a uint64.
func Hash64(opts ...HashOption) uint64 {
	hash := Hash(opts...)
	return xxhash.Sum64(hash[:])
}

// MapHash return a hash for the provided map.
// Maps with the same underlying key/value pairs in different order produce the same deterministic hash value.
func MapHash(m pcommon.Map) [16]byte {
	if m.Len() == 0 {
		return emptyHash
	}

	hw := hashWriterPool.Get().(*hashWriter)
	defer hashWriterPool.Put(hw)
	hw.byteBuf = hw.byteBuf[:0]

	hw.writeMapHash(m)

	return hw.hashSum128()
}

// ValueHash return a hash for the provided pcommon.Value.
func ValueHash(v pcommon.Value) [16]byte {
	hw := hashWriterPool.Get().(*hashWriter)
	defer hashWriterPool.Put(hw)
	hw.byteBuf = hw.byteBuf[:0]

	hw.writeValueHash(v)

	return hw.hashSum128()
}

func (hw *hashWriter) writeMapHash(m pcommon.Map) {
	// For each recursive call into this function we want to preserve the previous buffer state
	// while also adding new entries to the buffer. nextIndex is the index of the first new entry
	// added to the buffer for this call of the function.
	// This also works for the first non-recursive call of this function because the buffer is always empty
	// on the first call due to it being cleared of any added entries at then end of the function.
	nextIndex := len(hw.entriesBuf)

	// Collect keys together with their values in a single pass. Looking each value up by key with
	// Map.Get afterwards is a linear scan per key, which makes hashing a wide map quadratic.
	for k, v := range m.All() {
		hw.entriesBuf = append(hw.entriesBuf, mapEntry{key: k, val: v})
	}

	// Get only the newly added entries from the buffer by slicing the buffer from nextIndex to the end
	workingEntries := hw.entriesBuf[nextIndex:]

	slices.SortFunc(workingEntries, func(a, b mapEntry) int { return strings.Compare(a.key, b.key) })
	for _, e := range workingEntries {
		hw.byteBuf = append(hw.byteBuf, keyPrefix...)
		hw.byteBuf = append(hw.byteBuf, e.key...)
		hw.writeValueHash(e.val)
	}

	// Remove all entries that were added to the buffer during this call of the function, and drop the
	// references they hold so the pooled buffer does not keep the map's values reachable.
	clear(hw.entriesBuf[nextIndex:])
	hw.entriesBuf = hw.entriesBuf[:nextIndex]
}

func (hw *hashWriter) writeValueHash(v pcommon.Value) {
	switch v.Type() {
	case pcommon.ValueTypeStr:
		hw.writeString(v.Str())
	case pcommon.ValueTypeBool:
		if v.Bool() {
			hw.byteBuf = append(hw.byteBuf, valBoolTrue...)
		} else {
			hw.byteBuf = append(hw.byteBuf, valBoolFalse...)
		}
	case pcommon.ValueTypeInt:
		hw.byteBuf = append(hw.byteBuf, valIntPrefix...)
		hw.byteBuf = binary.LittleEndian.AppendUint64(hw.byteBuf, uint64(v.Int()))
	case pcommon.ValueTypeDouble:
		hw.byteBuf = append(hw.byteBuf, valDoublePrefix...)
		hw.byteBuf = binary.LittleEndian.AppendUint64(hw.byteBuf, math.Float64bits(v.Double()))
	case pcommon.ValueTypeMap:
		hw.byteBuf = append(hw.byteBuf, valMapPrefix...)
		hw.writeMapHash(v.Map())
		hw.byteBuf = append(hw.byteBuf, valMapSuffix...)
	case pcommon.ValueTypeSlice:
		sl := v.Slice()
		hw.byteBuf = append(hw.byteBuf, valSlicePrefix...)
		for i := 0; i < sl.Len(); i++ {
			hw.writeValueHash(sl.At(i))
		}
		hw.byteBuf = append(hw.byteBuf, valSliceSuffix...)
	case pcommon.ValueTypeBytes:
		hw.byteBuf = append(hw.byteBuf, valBytesPrefix...)
		hw.byteBuf = append(hw.byteBuf, v.Bytes().AsRaw()...)
	case pcommon.ValueTypeEmpty:
		hw.byteBuf = append(hw.byteBuf, valEmpty...)
	}
}

func (hw *hashWriter) writeString(s string) {
	hw.byteBuf = append(hw.byteBuf, valStrPrefix...)
	hw.byteBuf = append(hw.byteBuf, s...)
}

// hashSum128 returns a [16]byte hash sum.
func (hw *hashWriter) hashSum128() [16]byte {
	r := [16]byte{}
	res := r[:]

	h := xxhash.Sum64(hw.byteBuf)
	res = binary.LittleEndian.AppendUint64(res[:0], h)

	// Append an extra byte to generate another part of the hash sum
	hw.byteBuf = append(hw.byteBuf, extraByte...)
	h = xxhash.Sum64(hw.byteBuf)
	_ = binary.LittleEndian.AppendUint64(res[8:], h)

	return r
}
