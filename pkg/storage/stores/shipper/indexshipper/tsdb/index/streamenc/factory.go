// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/grafana/mimir/blob/main/pkg/storage/indexheader/encoding/factory.go
// Provenance-includes-license: AGPL-3.0-only
// Provenance-includes-copyright: The Grafana Mimir Authors.

package streamenc

import (
	"context"
	"hash/crc32"
)

const (
	// numLenBytes is the size of the 4-byte big-endian uint32 length prefix
	// used to mark the byte length of index sections.
	numLenBytes = 4
)

type DecbufFactory interface {
	// NewDecbufAtChecked returns a new binary decoding reader positioned at offset + 4 bytes.
	// It expects the first 4 bytes after offset to hold the big-endian-encoded content length,
	// followed by the contents and the expected checksum.
	// This method MUST check the CRC of the content and return an errored Decbuf if validation fails.
	NewDecbufAtChecked(ctx context.Context, offset int, table *crc32.Table) Decbuf

	// NewDecbufAtUnchecked returns a new binary decoding reader positioned at offset + 4 bytes.
	// It expects the first 4 bytes after offset to hold the big endian encoded content length,
	// followed by the contents and the expected checksum.
	// This method MUST NOT validate or compute the CRC of the content.
	// To check the CRC of the content, use NewDecbufAtChecked.
	NewDecbufAtUnchecked(ctx context.Context, offset int) Decbuf

	// NewRawDecbuf returns a new binary decoding reader positioned at the beginning of the underlying data,
	// and spanning the entire length of the data segment.
	// It MUST NOT make any assumptions about the layout of the underlying data w.r.t checksums, TOC, etc.
	// and it MUST NOT validate or compute the CRC of the content.
	// To create a binary decoding reader for some subset of the data or to perform integrity checks,
	// use NewDecbufAtUnchecked or NewDecbufAtChecked.
	NewRawDecbuf(ctx context.Context) Decbuf

	Close() error
}
