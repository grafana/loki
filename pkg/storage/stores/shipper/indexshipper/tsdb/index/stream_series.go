// SPDX-License-Identifier: AGPL-3.0-only
//
// Streaming implementations of Series and ChunkStats. Series records use a
// uvarint length prefix rather than the uint32 length prefix that the
// streamenc factory's NewDecbufAtChecked assumes, so we open a raw Decbuf
// and read the length ourselves.

package index

import (
	"context"
	"hash/crc32"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/streamenc"
)

// readUvarintSection reads a section at the given file offset whose layout is
//
//	[uvarint content_length][content...][crc32 uint32]
//
// It validates the CRC over content and returns the content bytes.
func (r *StreamReader) readUvarintSection(ctx context.Context, off int) ([]byte, error) {
	d := r.factory.NewRawDecbuf(ctx)
	if err := d.Err(); err != nil {
		return nil, err
	}
	defer func() { _ = d.Close() }()

	d.ResetAt(off)
	if err := d.Err(); err != nil {
		return nil, err
	}

	l := d.Uvarint64()
	if err := d.Err(); err != nil {
		return nil, err
	}
	content := make([]byte, l)
	d.ReadInto(content)
	if err := d.Err(); err != nil {
		return nil, err
	}
	expCRC := d.Be32()
	if err := d.Err(); err != nil {
		return nil, err
	}
	if crc32Castagnoli(content) != expCRC {
		return nil, errors.Wrap(streamenc.ErrInvalidChecksum, "series record")
	}
	return content, nil
}

// Series populates lbls and chks for the given series ref, matching
// Reader.Series semantics.
func (r *StreamReader) Series(id storage.SeriesRef, from int64, through int64, lbls *labels.Labels, chks *[]ChunkMeta) (uint64, error) {
	offset := id
	if r.version >= FormatV2 {
		offset = id * 16
	}
	content, err := r.readUvarintSection(context.Background(), int(offset))
	if err != nil {
		return 0, err
	}

	// r.decoder is package-private on Reader; we recreate a Decoder here.
	// Decoder is stateless once constructed; sharing across StreamReader
	// calls is safe.
	dec := r.decoder()
	fprint, err := dec.Series(r.version, content, id, from, through, lbls, chks)
	if err != nil {
		return 0, errors.Wrap(err, "read series")
	}
	return fprint, nil
}

// ChunkStats mirrors Reader.ChunkStats.
func (r *StreamReader) ChunkStats(id storage.SeriesRef, from, through int64, lbls *labels.Labels, by map[string]struct{}) (uint64, ChunkStats, error) {
	offset := id
	if r.version >= FormatV2 {
		offset = id * 16
	}
	content, err := r.readUvarintSection(context.Background(), int(offset))
	if err != nil {
		return 0, ChunkStats{}, err
	}

	dec := r.decoder()
	return dec.ChunkStats(r.version, content, id, from, through, lbls, by)
}

// decoder constructs a Decoder wired to this StreamReader's symbol lookup.
// We build a fresh instance per call because Decoder does not have a Reset
// method; storing one on the reader would require careful audit of its
// state to confirm reuse is safe. Series decoding is not currently
// allocation-bound, so this is fine for the first cut.
func (r *StreamReader) decoder() *Decoder {
	return newDecoder(r.lookupSymbol, DefaultMaxChunksToBypassMarkerLookup)
}

// crc32Castagnoli imported from stream_reader.go — keep this here so the
// file is self-documenting when read alone.
var _ = crc32.Castagnoli
