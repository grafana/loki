package variant

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"unicode/utf8"
)

// Metadata holds the decoded metadata dictionary for a variant value.
type Metadata struct {
	Strings []string
	Sorted  bool
}

// DecodeMetadata decodes a variant metadata binary blob.
//
// Format: header(1) | dictionary_size(uint) | offsets(dictionary_size+1) | string_data
//
// Header bits (per the spec's metadata encoding grammar,
// header = version | sorted_strings << 4 | offset_size_minus_one << 6):
//
//	0-3: version (must be 1)
//	4:   sorted_strings
//	6-7: offset_size_minus_one
//
// Like Decode, DecodeMetadata rejects what the spec declares invalid
// (non-UTF-8 dictionary strings, sizes exceeding the data) and does not
// validate the unused header bit, per VariantEncoding.md: "Bit 5 (marked R)
// is reserved; it must be ignored by readers."
func DecodeMetadata(data []byte) (Metadata, error) {
	if len(data) == 0 {
		return Metadata{}, errors.New("variant metadata: empty data")
	}

	header := data[0]
	version := header & 0x0F
	if version != 1 {
		return Metadata{}, fmt.Errorf("variant metadata: unsupported version %d", version)
	}

	sorted := (header>>4)&1 == 1
	offsetSz := offsetSize((header >> 6) & 0x03)

	pos := 1
	dictSize, n, err := readUint(data[pos:], offsetSz)
	if err != nil {
		return Metadata{}, fmt.Errorf("variant metadata: reading dictionary_size: %w", err)
	}
	pos += n

	// The metadata must contain at least (dictSize+1) offsets; validate
	// against the input size before allocating to reject corrupt data that
	// declares an absurdly large dictionary. dictSize < 0 happens only on
	// 32-bit platforms, where a 4-byte size above math.MaxInt32 overflows
	// int; the comparison is phrased as <= dictSize rather than
	// < dictSize+1 so the addition cannot overflow the same way.
	if remaining := len(data) - pos; dictSize < 0 || remaining/offsetSz <= dictSize {
		return Metadata{}, fmt.Errorf("variant metadata: dictionary size %d exceeds data", dictSize)
	}

	// Read (dictSize+1) offsets
	offsets := make([]int, dictSize+1)
	for i := range offsets {
		v, n, err := readUint(data[pos:], offsetSz)
		if err != nil {
			return Metadata{}, fmt.Errorf("variant metadata: reading offset %d: %w", i, err)
		}
		offsets[i] = v
		pos += n
	}

	stringData := data[pos:]

	strings := make([]string, dictSize)
	for i := range dictSize {
		start := offsets[i]
		end := offsets[i+1]
		// start < 0 happens only on 32-bit platforms, where a 4-byte
		// offset above math.MaxInt32 overflows int; without the check the
		// slice below would panic.
		if start < 0 || start > end || end > len(stringData) {
			return Metadata{}, fmt.Errorf("variant metadata: invalid string offset [%d, %d) in data of length %d", start, end, len(stringData))
		}
		if !utf8.Valid(stringData[start:end]) {
			return Metadata{}, fmt.Errorf("variant metadata: dictionary string %d is not valid UTF-8", i)
		}
		strings[i] = string(stringData[start:end])
	}

	return Metadata{Strings: strings, Sorted: sorted}, nil
}

// Lookup returns the string at the given dictionary index.
func (m Metadata) Lookup(id int) (string, error) {
	if id < 0 || id >= len(m.Strings) {
		return "", fmt.Errorf("variant metadata: index %d out of range [0, %d)", id, len(m.Strings))
	}
	return m.Strings[id], nil
}

// MetadataBuilder builds a variant metadata dictionary.
type MetadataBuilder struct {
	strings []string
	index   map[string]int
}

// Add adds a string to the dictionary and returns its index.
// If the string already exists, the existing index is returned.
func (b *MetadataBuilder) Add(s string) int {
	if b.index == nil {
		b.index = make(map[string]int)
	}
	if idx, ok := b.index[s]; ok {
		return idx
	}
	idx := len(b.strings)
	b.strings = append(b.strings, s)
	b.index[s] = idx
	return idx
}

// Build returns the decoded Metadata and the encoded binary representation.
// The dictionary indices in the output match those returned by Add, so
// encoded values referencing those indices remain valid.
func (b *MetadataBuilder) Build() (Metadata, []byte) {
	n := len(b.strings)

	// Check if the strings happen to be sorted
	sorted := sort.StringsAreSorted(b.strings)

	// Compute string data and offsets
	offsets := make([]int, n+1)
	totalLen := 0
	for i, s := range b.strings {
		offsets[i] = totalLen
		totalLen += len(s)
	}
	offsets[n] = totalLen

	// Determine offset size
	maxOffset := max(totalLen, n)
	osc := offsetSizeCode(maxOffset)
	offsetSz := offsetSize(osc)

	// Build the binary blob
	// header(1) + dict_size(offsetSz) + offsets((n+1)*offsetSz) + string_data(totalLen)
	size := 1 + offsetSz + (n+1)*offsetSz + totalLen
	buf := make([]byte, size)

	// Header: version=1, sorted_strings flag at bit 4, offset_size_minus_one
	// at bits 6-7 (per the spec's metadata encoding grammar).
	sortedBit := byte(0)
	if sorted {
		sortedBit = 1
	}
	buf[0] = 1 | (sortedBit << 4) | (osc << 6)

	pos := 1
	writeUint(buf[pos:], n, offsetSz)
	pos += offsetSz

	for _, off := range offsets {
		writeUint(buf[pos:], off, offsetSz)
		pos += offsetSz
	}

	for _, s := range b.strings {
		copy(buf[pos:], s)
		pos += len(s)
	}

	return Metadata{Strings: b.strings, Sorted: sorted}, buf
}

// Reset clears the builder for reuse.
func (b *MetadataBuilder) Reset() {
	b.strings = b.strings[:0]
	for k := range b.index {
		delete(b.index, k)
	}
}

// readUint reads an unsigned integer of the given byte width from data. Note
// that on 32-bit platforms a 4-byte value above math.MaxInt32 overflows int
// and is returned negative; callers validating sizes and offsets against the
// input must guard against negative results before using them.
func readUint(data []byte, size int) (int, int, error) {
	if len(data) < size {
		return 0, 0, errors.New("not enough data")
	}
	switch size {
	case 1:
		return int(data[0]), 1, nil
	case 2:
		return int(binary.LittleEndian.Uint16(data[:2])), 2, nil
	case 3:
		return int(data[0]) | int(data[1])<<8 | int(data[2])<<16, 3, nil
	case 4:
		return int(binary.LittleEndian.Uint32(data[:4])), 4, nil
	default:
		return 0, 0, fmt.Errorf("invalid offset size %d", size)
	}
}

// writeUint writes an unsigned integer of the given byte width to buf.
func writeUint(buf []byte, v int, size int) {
	switch size {
	case 1:
		buf[0] = byte(v)
	case 2:
		binary.LittleEndian.PutUint16(buf[:2], uint16(v))
	case 3:
		buf[0] = byte(v)
		buf[1] = byte(v >> 8)
		buf[2] = byte(v >> 16)
	case 4:
		binary.LittleEndian.PutUint32(buf[:4], uint32(v))
	}
}
