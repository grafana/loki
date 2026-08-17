package index

import (
	"context"
	"fmt"
	"hash/crc32"
	"sort"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/streamenc"
)

// seriesOffset returns the absolute file offset of the series record for the
// given series ref.
// Series records are padded to a 16-byte alignment and a ref is the record's
// file offset divided by 16, which is what lets a 4-byte ref address a much
// larger file.
// See Creator.AddSeries.
func seriesOffset(id storage.SeriesRef) int {
	return int(id) * 16
}

// maxSeriesForwardSkip bounds how far a seriesScan will read-and-discard
// forward rather than repositioning the file.
// Discarding drains the read-ahead buffer and then refills it, one read syscall
// per bufferful, so past a buffer's worth of bytes a seek is the cheaper way to
// get there.
const maxSeriesForwardSkip = streamenc.ReaderBufferSize

// seriesScan reads series records for a single pass over a postings list.
// It is how StreamReader reads the postings list.
// A seriesScan is not safe for concurrent use.
type seriesScan struct {
	reader *StreamReader
	decbuf streamenc.Decbuf
	// scratch backs the content slice handed to the decoder.
	// The decoder never retains it, so it is reused across records rather
	// than allocated per readSeriesRecord.
	// It grows to the largest record the scan has seen.
	scratch []byte
	// reopen records that decbuf hit an error.
	// A Decbuf's error is sticky and would silently no-op every later read,
	// so the scan replaces it before the next readSeriesRecord rather than
	// failing the rest of the iteration.
	reopen bool
}

// NewSeriesScan implements Reader.
func (s *StreamReader) NewSeriesScan() SeriesScan {
	return &seriesScan{
		reader: s,
		decbuf: s.factory.NewRawDecbuf(context.Background()),
	}
}

func (sc *seriesScan) Close() error {
	return sc.decbuf.Close()
}

// seek positions the scan's reader at the given absolute file offset.
//
// A short forward step is served by discarding bytes, which will often come
// straight out of the read-ahead buffer and cost nothing; anything else falls
// back to repositioning the file.
// Backwards steps are always a reposition, so out-of-order refs stay correct
// and merely incur the cost of ResetAt.
func (sc *seriesScan) seek(offset int) {
	if distance := offset - sc.decbuf.Offset(); distance >= 0 && distance <= maxSeriesForwardSkip {
		sc.decbuf.Skip(distance)
		return
	}
	sc.decbuf.ResetAt(offset)
}

// readSeriesRecord reads the series record at the given absolute file offset
// and returns its content bytes, having validated the record's CRC32.
//
// On disk a series record is a uvarint content length, the content, then
// a CRC32 over the content alone.
// The uvarint length prefix is why this can't go through the streamenc
// factory: NewDecbufAtChecked assumes a 4-byte big-endian length, so we open
// a raw Decbuf over the whole file and decode the record ourselves.
// The returned bytes are only valid until the next call on this scan.
func (sc *seriesScan) readSeriesRecord(offset int) ([]byte, error) {
	if sc.reopen {
		_ = sc.decbuf.Close()
		sc.decbuf = sc.reader.factory.NewRawDecbuf(context.Background())
		sc.reopen = false
	}
	if err := sc.decbuf.Err(); err != nil {
		sc.reopen = true
		return nil, fmt.Errorf("open series decbuf: %w", err)
	}

	if sc.seek(offset); sc.decbuf.Err() != nil {
		sc.reopen = true
		return nil, fmt.Errorf("go to start of series record: %w", sc.decbuf.Err())
	}

	contentLen := sc.decbuf.Uvarint64()
	if err := sc.decbuf.Err(); err != nil {
		sc.reopen = true
		return nil, fmt.Errorf("read series record length: %w", err)
	}
	// Bound the claimed length against what is left in the file before
	// allocating, so a corrupt length prefix can't ask for an arbitrarily
	// large buffer.
	remaining := sc.decbuf.Len()
	if remaining < crc32.Size || contentLen > uint64(remaining-crc32.Size) {
		return nil, fmt.Errorf(
			"series record at %d claims %d content bytes but only %d remain: %w",
			offset, contentLen, remaining, streamenc.ErrInvalidSize,
		)
	}

	if uint64(cap(sc.scratch)) < contentLen {
		sc.scratch = make([]byte, contentLen)
	}
	content := sc.scratch[:contentLen]

	sc.decbuf.ReadInto(content)
	expectedCRC := sc.decbuf.Be32()
	if err := sc.decbuf.Err(); err != nil {
		sc.reopen = true
		return nil, fmt.Errorf("read series record: %w", err)
	}
	if crc32.Checksum(content, castagnoliTable) != expectedCRC {
		return nil, fmt.Errorf("series record at %d: %w", offset, streamenc.ErrInvalidChecksum)
	}
	return content, nil
}

func (sc *seriesScan) LabelValueFor(id storage.SeriesRef, label string) (string, error) {
	content, err := sc.readSeriesRecord(seriesOffset(id))
	if err != nil {
		return "", fmt.Errorf("label values for: %w", err)
	}

	value, err := sc.reader.decoder.LabelValueFor(content, label)
	if err != nil {
		return "", storage.ErrNotFound
	}
	if value == "" {
		return "", storage.ErrNotFound
	}
	return value, nil
}

func (sc *seriesScan) LabelNamesFor(ids ...storage.SeriesRef) ([]string, error) {
	// Gather the name offsets in the symbol table first, so each distinct name
	// is only resolved once no matter how many series carry it.
	offsetsMap := make(map[uint32]struct{})
	for _, id := range ids {
		content, err := sc.readSeriesRecord(seriesOffset(id))
		if err != nil {
			return nil, fmt.Errorf("get buffer for series: %w", err)
		}

		offsets, err := sc.reader.decoder.LabelNamesOffsetsFor(content)
		if err != nil {
			return nil, fmt.Errorf("get label name offsets: %w", err)
		}
		for _, offset := range offsets {
			offsetsMap[offset] = struct{}{}
		}
	}

	names := make([]string, 0, len(offsetsMap))
	for offset := range offsetsMap {
		name, err := sc.reader.symbols.Lookup(offset)
		if err != nil {
			return nil, fmt.Errorf("lookup symbol in LabelNamesFor: %w", err)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (sc *seriesScan) Series(id storage.SeriesRef, from int64, through int64, lbls *labels.Labels, chks *[]ChunkMeta) (uint64, error) {
	content, err := sc.readSeriesRecord(seriesOffset(id))
	if err != nil {
		return 0, err
	}

	fingerprint, err := sc.reader.decoder.Series(sc.reader.version, content, id, from, through, lbls, chks)
	if err != nil {
		return 0, fmt.Errorf("decode series: %w", err)
	}
	return fingerprint, nil
}

func (sc *seriesScan) ChunkStats(id storage.SeriesRef, from, through int64, lbls *labels.Labels, by map[string]struct{}) (uint64, ChunkStats, error) {
	content, err := sc.readSeriesRecord(seriesOffset(id))
	if err != nil {
		return 0, ChunkStats{}, err
	}

	return sc.reader.decoder.ChunkStats(sc.reader.version, content, id, from, through, lbls, by)
}
