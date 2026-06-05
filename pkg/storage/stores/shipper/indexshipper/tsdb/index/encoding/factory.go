// SPDX-License-Identifier: AGPL-3.0-only
// Copied from: https://github.com/grafana/mimir/blob/main/pkg/storage/indexheader/encoding/factory.go

package encoding

import (
	"hash/crc32"
)

const (
	numLenBytes = 4 // Index sections use a 4-byte big-endian uint32 to mark the byte length of the section.
)

type DecbufFactory interface {
	// NewDecbufAtChecked returns a new binary decoding reader positioned at offset + 4 bytes.
	// It expects the first 4 bytes after offset to hold the big-endian-encoded content length,
	// followed by the contents and the expected checksum.
	// This method MUST check the CRC of the content and return an errored Decbuf if validation fails.
	NewDecbufAtChecked(offset int, table *crc32.Table) Decbuf

	// NewDecbufAtUnchecked returns a new binary decoding reader positioned at offset + 4 bytes.
	// It expects the first 4 bytes after offset to hold the big endian encoded content length,
	// followed by the contents and the expected checksum.
	// This method MUST NOT validate or compute the CRC of the content.
	// To check the CRC of the content, use NewDecbufAtChecked.
	NewDecbufAtUnchecked(offset int) Decbuf

	// NewRawDecbuf returns a new binary decoding reader positioned at the beginning of the underlying data,
	// and spanning the entire length of the data segment.
	// It MUST NOT make any assumptions about the layout of the underlying data w.r.t checksums, TOC, etc.
	// and it MUST NOT validate or compute the CRC of the content.
	// To create a binary decoding reader for some subset of the data or to perform integrity checks,
	// use NewDecbufAtUnchecked or NewDecbufAtChecked.
	NewRawDecbuf() Decbuf

	// NewDecbufInSection returns a new binary decoding reader positioned at tableOffset + sectionStartOffset,
	// spanning to tableOffset+sectionEndOffset or the end of the table, whichever comes first.
	// Reading from a section of the table rather than the whole table allows the reader to leverage
	// shorter read operations, which can improve read latency and cache efficiency (if caching these reads).
	// It expects the first 4 bytes after tableOffset to hold the big-endian encoded content length.
	// This method MUST NOT validate or compute the CRC of the content.
	NewDecbufInSection(tableOffset, sectionStartOffset, sectionEndOffset int) Decbuf

	// NewDecbufUvarintAt returns a new binary decoding reader for a section whose byte length is
	// encoded as a uvarint at the given offset, followed by the contents and a CRC32 checksum.
	// The returned Decbuf is positioned at the first content byte (after the uvarint prefix) and
	// its Len() reflects the content length only, excluding the CRC bytes.
	// If table is non-nil, this method MUST verify the CRC and return an errored Decbuf on mismatch.
	NewDecbufUvarintAt(offset int, table *crc32.Table) Decbuf

	Close() error
}
