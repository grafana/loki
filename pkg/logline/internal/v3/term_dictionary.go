package v3

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/klauspost/compress/zstd"
)

// Term dictionary using front-coded compression with zstd.
// Based on the front_coded_zstd_2.go reference implementation.
//
// Structure:
// - Terms are grouped into blocks of BlockSize (64)
// - Each block stores the first term as a header
// - Subsequent block headers are XOR-delta encoded from the previous header
// - Within a block, terms are prefix-compressed (store prefix length + suffix)
// - The entire payload is zstd compressed

const (
	// TermDictBlockSize is the number of terms per block.
	// Targeting 2MB compressed blocks that can hold 100-500K terms.
	// At ~7 bytes per term uncompressed (6 byte term + ~1 byte metadata),
	// 131072 terms = ~900KB uncompressed, which compresses to ~200-400KB with zstd.
	TermDictBlockSize = 131072

	// NgramLength is the length of each ngram term
	NgramLength = 6
)

// TermDictionaryPayload is the uncompressed data structure
type TermDictionaryPayload struct {
	FirstHeader  [NgramLength]byte
	HeaderDeltas []byte // (numBlocks - 1) * NgramLength bytes, XOR encoded
	EntryData    []byte // packed prefix_len/suffix_len
	Suffixes     []byte
}

// TermDictionary is a compressed term dictionary that maps terms to positions.
type TermDictionary struct {
	Compressed []byte
	NumBlocks  int
	Count      int
}

// BuildTermDictionary creates a TermDictionary from sorted ngrams.
// The ngrams must be sorted lexicographically and each must be exactly NgramLength bytes.
func BuildTermDictionary(ngrams [][NgramLength]byte) *TermDictionary {
	if len(ngrams) == 0 {
		return &TermDictionary{
			Compressed: []byte{},
			NumBlocks:  0,
			Count:      0,
		}
	}

	var firstHeader [NgramLength]byte
	copy(firstHeader[:], ngrams[0][:])

	var headerDeltas []byte
	var entryData []byte
	var suffixes []byte
	numBlocks := 0
	prevHeader := firstHeader

	for i := 0; i < len(ngrams); i += TermDictBlockSize {
		end := min(i+TermDictBlockSize, len(ngrams))
		chunk := ngrams[i:end]

		var headerBytes [NgramLength]byte
		copy(headerBytes[:], chunk[0][:])

		if numBlocks > 0 {
			// Store XOR delta from previous header
			delta := xorNgramBytes(prevHeader[:], headerBytes[:])
			headerDeltas = append(headerDeltas, delta[:]...)
		}
		prevHeader = headerBytes
		numBlocks++

		// Encode entries within block
		prev := chunk[0][:]
		for _, s := range chunk[1:] {
			sBytes := s[:]
			prefixLen := commonPrefixLength(prev, sBytes)
			suffixLen := NgramLength - prefixLen
			suffix := sBytes[prefixLen:]

			// Pack prefix_len and suffix_len if both fit in nibbles (0-15)
			if prefixLen <= 15 && suffixLen <= 15 {
				entryData = append(entryData, byte((prefixLen<<4)|suffixLen))
			} else {
				entryData = append(entryData, 0xFF)
				entryData = append(entryData, byte(prefixLen))
				entryData = append(entryData, byte(suffixLen))
			}

			suffixes = append(suffixes, suffix...)
			prev = sBytes
		}
	}

	// Serialize payload
	payload := serializeTermDictPayload(firstHeader, headerDeltas, entryData, suffixes)

	// Compress with zstd level 3 (good balance of speed and compression)
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedBetterCompression))
	if err != nil {
		panic(err) // static options only — programming error if this fails
	}
	compressed := encoder.EncodeAll(payload, nil)
	_ = encoder.Close()

	return &TermDictionary{
		Compressed: compressed,
		NumBlocks:  numBlocks,
		Count:      len(ngrams),
	}
}

// BuildTermDictionaryFromStrings creates a TermDictionary from sorted ngram strings.
func BuildTermDictionaryFromStrings(ngrams []string) *TermDictionary {
	ngramBytes := make([][NgramLength]byte, len(ngrams))
	for i, s := range ngrams {
		copy(ngramBytes[i][:], s)
	}
	return BuildTermDictionary(ngramBytes)
}

func xorNgramBytes(a, b []byte) [NgramLength]byte {
	var result [NgramLength]byte
	for i := range NgramLength {
		result[i] = a[i] ^ b[i]
	}
	return result
}

func commonPrefixLength(a, b []byte) int {
	n := min(len(b), len(a))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

func decodeTermDictHeader(first [NgramLength]byte, deltas []byte, blockIdx int) [NgramLength]byte {
	if blockIdx == 0 {
		return first
	}

	current := first
	for i := range blockIdx {
		deltaStart := i * NgramLength
		for j := range NgramLength {
			current[j] ^= deltas[deltaStart+j]
		}
	}
	return current
}

func serializeTermDictPayload(firstHeader [NgramLength]byte, headerDeltas, entryData, suffixes []byte) []byte {
	// Calculate total size: 6 + 3*(8 + data) for header + 3 sections
	totalSize := NgramLength + 8 + len(headerDeltas) + 8 + len(entryData) + 8 + len(suffixes)
	buf := bytes.NewBuffer(make([]byte, 0, totalSize))

	// Write firstHeader (6 bytes)
	buf.Write(firstHeader[:])

	// Write headerDeltas length (8 bytes) + data
	_ = binary.Write(buf, binary.LittleEndian, uint64(len(headerDeltas)))
	buf.Write(headerDeltas)

	// Write entryData length (8 bytes) + data
	_ = binary.Write(buf, binary.LittleEndian, uint64(len(entryData)))
	buf.Write(entryData)

	// Write suffixes length (8 bytes) + data
	_ = binary.Write(buf, binary.LittleEndian, uint64(len(suffixes)))
	buf.Write(suffixes)

	return buf.Bytes()
}

func deserializeTermDictPayload(data []byte) *TermDictionaryPayload {
	r := bytes.NewReader(data)

	var firstHeader [NgramLength]byte
	_, _ = r.Read(firstHeader[:])

	var headerDeltasLen uint64
	_ = binary.Read(r, binary.LittleEndian, &headerDeltasLen)
	headerDeltas := make([]byte, headerDeltasLen)
	_, _ = r.Read(headerDeltas)

	var entryDataLen uint64
	_ = binary.Read(r, binary.LittleEndian, &entryDataLen)
	entryData := make([]byte, entryDataLen)
	_, _ = r.Read(entryData)

	var suffixesLen uint64
	_ = binary.Read(r, binary.LittleEndian, &suffixesLen)
	suffixes := make([]byte, suffixesLen)
	_, _ = r.Read(suffixes)

	return &TermDictionaryPayload{
		FirstHeader:  firstHeader,
		HeaderDeltas: headerDeltas,
		EntryData:    entryData,
		Suffixes:     suffixes,
	}
}

func (td *TermDictionary) decompress() (*TermDictionaryPayload, error) {
	decoder, err := zstd.NewReader(nil)
	if err != nil {
		panic(err) // static options only — programming error
	}
	defer decoder.Close()

	decompressed, err := decoder.DecodeAll(td.Compressed, nil)
	if err != nil {
		return nil, fmt.Errorf("decompress term dictionary: %w", err)
	}
	return deserializeTermDictPayload(decompressed), nil
}

func decodeTermDictBlock(payload *TermDictionaryPayload, blockIdx, count, numBlocks int) [][NgramLength]byte {
	result := make([][NgramLength]byte, 0, TermDictBlockSize)

	// First entry is the header
	header := decodeTermDictHeader(payload.FirstHeader, payload.HeaderDeltas, blockIdx)
	result = append(result, header)

	entriesInBlock := TermDictBlockSize - 1
	if blockIdx == numBlocks-1 {
		remaining := count - blockIdx*TermDictBlockSize
		if remaining > 1 {
			entriesInBlock = remaining - 1
		} else {
			entriesInBlock = 0
		}
	}

	if entriesInBlock == 0 {
		return result
	}

	// Find where this block's entries start in entryData
	entriesBefore := blockIdx * (TermDictBlockSize - 1)

	// Scan entryData to find our starting position
	entryDataPos := 0
	suffixPos := 0

	for range entriesBefore {
		if entryDataPos >= len(payload.EntryData) {
			break
		}
		b := payload.EntryData[entryDataPos]
		var suffixLen int
		if b == 0xFF {
			entryDataPos++
			entryDataPos++ // skip prefix_len
			suffixLen = int(payload.EntryData[entryDataPos])
			entryDataPos++
		} else {
			entryDataPos++
			suffixLen = int(b & 0x0F)
		}
		suffixPos += suffixLen
	}

	// Decode this block's entries
	current := header
	for i := 0; i < entriesInBlock; i++ {
		if entryDataPos >= len(payload.EntryData) {
			break
		}

		b := payload.EntryData[entryDataPos]
		var prefixLen, suffixLen int
		if b == 0xFF {
			entryDataPos++
			prefixLen = int(payload.EntryData[entryDataPos])
			entryDataPos++
			suffixLen = int(payload.EntryData[entryDataPos])
			entryDataPos++
		} else {
			entryDataPos++
			prefixLen = int(b >> 4)
			suffixLen = int(b & 0x0F)
		}

		suffix := payload.Suffixes[suffixPos : suffixPos+suffixLen]
		suffixPos += suffixLen

		var next [NgramLength]byte
		copy(next[:prefixLen], current[:prefixLen])
		copy(next[prefixLen:prefixLen+suffixLen], suffix)
		current = next
		result = append(result, current)
	}

	return result
}

// Query looks up an ngram and returns its position, or -1 if not found.
func (td *TermDictionary) Query(ngram [NgramLength]byte) (int, error) {
	if td.Count == 0 {
		return -1, nil
	}

	payload, err := td.decompress()
	if err != nil {
		return 0, err
	}

	// Binary search on block headers
	lo, hi := 0, td.NumBlocks
	for lo < hi {
		mid := lo + (hi-lo)/2
		header := decodeTermDictHeader(payload.FirstHeader, payload.HeaderDeltas, mid)

		cmp := bytes.Compare(header[:], ngram[:])
		if cmp == 0 {
			return mid * TermDictBlockSize, nil
		} else if cmp < 0 {
			lo = mid + 1
		} else {
			hi = mid
		}
	}

	// Check the block before where we'd insert
	blockIdx := max(lo-1, 0)
	if blockIdx >= td.NumBlocks {
		return -1, nil
	}

	decoded := decodeTermDictBlock(payload, blockIdx, td.Count, td.NumBlocks)
	for i, entry := range decoded {
		if bytes.Equal(entry[:], ngram[:]) {
			return blockIdx*TermDictBlockSize + i, nil
		}
	}

	// Also check the next block's header
	if blockIdx+1 < td.NumBlocks {
		nextHeader := decodeTermDictHeader(payload.FirstHeader, payload.HeaderDeltas, blockIdx+1)
		if bytes.Equal(nextHeader[:], ngram[:]) {
			return (blockIdx + 1) * TermDictBlockSize, nil
		}
	}

	return -1, nil
}

// QueryString looks up an ngram string and returns its position, or -1 if not found.
func (td *TermDictionary) QueryString(ngram string) (int, error) {
	var key [NgramLength]byte
	copy(key[:], ngram)
	return td.Query(key)
}

// ToBytes serializes the term dictionary.
func (td *TermDictionary) ToBytes() []byte {
	buf := bytes.NewBuffer(make([]byte, 0, 24+len(td.Compressed)))
	_ = binary.Write(buf, binary.LittleEndian, uint64(len(td.Compressed)))
	buf.Write(td.Compressed)
	_ = binary.Write(buf, binary.LittleEndian, uint64(td.NumBlocks))
	_ = binary.Write(buf, binary.LittleEndian, uint64(td.Count))
	return buf.Bytes()
}

// TermDictionaryFromBytes deserializes a term dictionary.
func TermDictionaryFromBytes(data []byte) (*TermDictionary, error) {
	r := bytes.NewReader(data)

	var compressedLen uint64
	if err := binary.Read(r, binary.LittleEndian, &compressedLen); err != nil {
		return nil, fmt.Errorf("failed to read compressed length: %w", err)
	}

	compressed := make([]byte, compressedLen)
	if _, err := r.Read(compressed); err != nil {
		return nil, fmt.Errorf("failed to read compressed data: %w", err)
	}

	var numBlocks uint64
	if err := binary.Read(r, binary.LittleEndian, &numBlocks); err != nil {
		return nil, fmt.Errorf("failed to read numBlocks: %w", err)
	}
	var count uint64
	if err := binary.Read(r, binary.LittleEndian, &count); err != nil {
		return nil, fmt.Errorf("failed to read count: %w", err)
	}

	return &TermDictionary{
		Compressed: compressed,
		NumBlocks:  int(numBlocks),
		Count:      int(count),
	}, nil
}

// WriteTo writes the term dictionary to a writer.
func (td *TermDictionary) WriteTo(w io.Writer) (int64, error) {
	data := td.ToBytes()
	n, err := w.Write(data)
	return int64(n), err
}

// ReadTermDictionaryFrom reads a term dictionary from a reader.
func ReadTermDictionaryFrom(r io.Reader) (*TermDictionary, error) {
	var compressedLen uint64
	if err := binary.Read(r, binary.LittleEndian, &compressedLen); err != nil {
		return nil, fmt.Errorf("failed to read compressed length: %w", err)
	}

	compressed := make([]byte, compressedLen)
	if _, err := io.ReadFull(r, compressed); err != nil {
		return nil, fmt.Errorf("failed to read compressed data: %w", err)
	}

	var numBlocks uint64
	if err := binary.Read(r, binary.LittleEndian, &numBlocks); err != nil {
		return nil, fmt.Errorf("failed to read numBlocks: %w", err)
	}
	var count uint64
	if err := binary.Read(r, binary.LittleEndian, &count); err != nil {
		return nil, fmt.Errorf("failed to read count: %w", err)
	}

	return &TermDictionary{
		Compressed: compressed,
		NumBlocks:  int(numBlocks),
		Count:      int(count),
	}, nil
}

// GetAllTerms returns all terms in the dictionary in sorted order.
// This is primarily for debugging and testing.
func (td *TermDictionary) GetAllTerms() ([][NgramLength]byte, error) {
	if td.Count == 0 {
		return nil, nil
	}

	payload, err := td.decompress()
	if err != nil {
		return nil, err
	}
	result := make([][NgramLength]byte, 0, td.Count)

	for blockIdx := 0; blockIdx < td.NumBlocks; blockIdx++ {
		decoded := decodeTermDictBlock(payload, blockIdx, td.Count, td.NumBlocks)
		result = append(result, decoded...)
	}

	return result, nil
}

// TermCount returns the number of terms in the dictionary.
func (td *TermDictionary) TermCount() int {
	return td.Count
}

// CompressedSize returns the size of the compressed data in bytes.
func (td *TermDictionary) CompressedSize() int {
	return len(td.Compressed)
}
