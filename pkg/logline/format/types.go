// Package format defines the shared vocabulary of the on-disk index format:
// the term iterator contract, postings bitmaps, per-document metadata, the
// postings encodings, and the JSON-friendly header summary.
//
// Both the version shim and the versioned format implementations import this
// package, which keeps them free of import cycles.
package format

import (
	"time"

	"github.com/RoaringBitmap/roaring"
)

// TermIterator iterates over all terms in an index.
type TermIterator interface {
	Next() bool
	Term() [8]byte
	Bitmap() Bitmap
	Err() error
}

// TermBitmapPostings represents a term and its roaring bitmap.
// Not an on-disk type, but shared between the accumulator (pkg/logline)
// and the encoder (v1) as a handoff contract.
type TermBitmapPostings struct {
	Term   [8]byte
	Bitmap *roaring.Bitmap
}

// Bitmap wraps a roaring bitmap with an optional MatchesAll sentinel.
// MatchesAll indicates a density-filtered term that matches all documents.
// When MatchesAll is true, Roaring is nil.
type Bitmap struct {
	Roaring    *roaring.Bitmap
	MatchesAll bool
}

// And intersects this bitmap with another. Sentinel (MatchesAll) bitmaps
// are identity elements: AND(all, x) = x, AND(x, all) = x, AND(all, all) = all.
func (r Bitmap) And(other Bitmap) Bitmap {
	if r.MatchesAll {
		return other
	}
	if other.MatchesAll {
		return r
	}
	r.Roaring.And(other.Roaring)
	return r
}

// Or unions this bitmap with another. Sentinel (MatchesAll) bitmaps
// are absorbing: OR(all, x) = all, OR(x, all) = all, OR(all, all) = all.
// A zero-value Bitmap (nil Roaring, MatchesAll=false) is identity for Or.
func (r Bitmap) Or(other Bitmap) Bitmap {
	if r.MatchesAll || other.MatchesAll {
		return Bitmap{MatchesAll: true}
	}
	if r.Roaring == nil {
		return other
	}
	if other.Roaring == nil {
		return r
	}
	r.Roaring.Or(other.Roaring)
	return r
}

// IsEmpty returns true if the bitmap has no documents.
// A MatchesAll bitmap is never empty.
func (r Bitmap) IsEmpty() bool {
	if r.MatchesAll {
		return false
	}
	return r.Roaring == nil || r.Roaring.IsEmpty()
}

// DocumentMetadata contains metadata about a document for storage in the index header.
// Timestamps are stored as milliseconds since Unix epoch for millisecond precision.
type DocumentMetadata struct {
	ID          uint32
	MinTimeUnix int64 // milliseconds since Unix epoch
	MaxTimeUnix int64 // milliseconds since Unix epoch
}

// QueryMultipleTerminationReason indicates why QueryMultiple stopped.
type QueryMultipleTerminationReason uint8

const (
	QueryMultipleReasonComplete QueryMultipleTerminationReason = iota
	QueryMultipleReasonTermMiss
	QueryMultipleReasonEmptyAnd
)

// QueryMultipleResult is the result of a multi-term query against an index.
type QueryMultipleResult struct {
	Documents            []DocumentMetadata
	TermBatchesProcessed int
	Reason               QueryMultipleTerminationReason
}

// PostingsEncoding selects how per-term postings are encoded in the postings section.
type PostingsEncoding uint32

const (
	// PostingsEncodingBIDX stores one bitmap per term using BitmapIndexV2.
	PostingsEncodingBIDX PostingsEncoding = iota
	// PostingsEncodingFastUint32Blocked stores docIDs as blocked []uint32 payloads.
	PostingsEncodingFastUint32Blocked
	// PostingsEncodingFastRoaringBlocked stores blocked serialized roaring payloads.
	PostingsEncodingFastRoaringBlocked
	// PostingsEncodingBIDXPacked stores N terms per packed BIDX bitmap.
	PostingsEncodingBIDXPacked
	// PostingsEncodingFastDeltaVarIntBlocked stores delta+varint encoded docIDs in blocks.
	PostingsEncodingFastDeltaVarIntBlocked
)

// MaxQueryRequestBytes enforces the object-storage request contract.
const MaxQueryRequestBytes = 8 * 1024 * 1024

// ReadSection classifies a byte range within an index file.
type ReadSection uint8

const (
	ReadSectionUnknown  ReadSection = iota
	ReadSectionHeader               // header (v1) or footer (v2)
	ReadSectionPostings             // postings data (bitmaps)
	ReadSectionTermDict             // term dictionary
	ReadSectionMetadata             // doc metadata + block directories
)

const (
	flagEncodingMask uint32 = 0x0F
	flagPackingMask  uint32 = 0xF0
	flagPackingShift        = 4
)

// ParseFlags extracts the postings encoding and packing factor from header flags.
func ParseFlags(flags uint32) (PostingsEncoding, uint8) {
	encoding := PostingsEncoding(flags & flagEncodingMask)
	packing := uint8((flags & flagPackingMask) >> flagPackingShift)
	if packing == 0 {
		packing = 1
	}
	return encoding, packing
}

// WriterConfig allows overriding per-writer settings beyond the format defaults.
// A nil *WriterConfig means use version defaults for all settings.
// Zero-valued fields use the version default for that field.
type WriterConfig struct {
	// Encoding overrides the postings encoding. Zero value uses the format default.
	Encoding PostingsEncoding

	// DensityThreshold overrides the density threshold.
	// 0 uses the format default; positive values enable at that threshold;
	// negative values disable filtering.
	// The threshold is applied against the total documents in a full day
	// (24h / DocumentInterval), not the current index's document count.
	DensityThreshold float32

	// DocumentInterval is the time range each document ID covers. Used with
	// DensityThreshold to compute the day-based sentinel cutoff:
	// sentinelCutoff = (24h / DocumentInterval) * DensityThreshold.
	// Zero disables the density filter.
	DocumentInterval time.Duration
}

// HeaderInfo is a JSON-friendly summary of a binary index header.
// Stored in meta.json alongside each index file.
type HeaderInfo struct {
	Version              uint32 `json:"version"`
	Flags                uint32 `json:"flags"`
	DocumentCount        uint32 `json:"document_count"`
	TermBlockCount       uint32 `json:"term_block_count"`
	PostingsBlockCount   uint32 `json:"postings_block_count"`
	PostingsCompression  uint32 `json:"postings_compression"`
	TermCount            uint64 `json:"term_count"`
	PostingsDataSize     uint64 `json:"postings_data_size"`
	TermDataSize         uint64 `json:"term_data_size"`
	DocMetadataSize      uint64 `json:"doc_metadata_size"`
	TermBlockDirSize     uint64 `json:"term_block_dir_size"`
	PostingsBlockDirSize uint64 `json:"postings_block_dir_size"`
}
