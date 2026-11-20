package index

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
)

// OrderedChunkScan retains the checked record between looking up its
// fingerprint and decoding it. A streaming reader can therefore look ahead
// without rereading or decoding the next series.
type OrderedChunkScan struct {
	readRecord   func(storage.SeriesRef) ([]byte, error)
	close        func() error
	version      int
	decoder      *Decoder
	lookupSymbol func(uint32) (string, error)

	ref     storage.SeriesRef
	content []byte
}

func (s *OrderedChunkScan) record(id storage.SeriesRef) ([]byte, error) {
	if s.content != nil && s.ref == id {
		return s.content, nil
	}
	s.content = nil
	content, err := s.readRecord(id)
	if err != nil {
		return nil, err
	}
	s.ref, s.content = id, content
	return content, nil
}

// SeriesFingerprint returns the fingerprint without decoding labels or chunks.
func (s *OrderedChunkScan) SeriesFingerprint(id storage.SeriesRef) (uint64, error) {
	content, err := s.record(id)
	if err != nil {
		return 0, err
	}
	if len(content) < 8 {
		return 0, fmt.Errorf("read series fingerprint: %w", io.ErrUnexpectedEOF)
	}
	return binary.BigEndian.Uint64(content), nil
}

// Series decodes the labels and matching chunks for the given series offset.
// The caller can pass nil for lbls to decode only chunks.
func (s *OrderedChunkScan) Series(id storage.SeriesRef, from, through int64, lbls *labels.Labels, chks *[]ChunkMeta) (uint64, error) {
	content, err := s.record(id)
	if err != nil {
		return 0, err
	}
	return s.decoder.seriesWithSymbolLookup(s.version, content, id, from, through, lbls, chks, s.lookupSymbol)
}

// Close releases resources held by the scan.
func (s *OrderedChunkScan) Close() error {
	s.content = nil
	if s.close == nil {
		return nil
	}
	closeScan := s.close
	s.close = nil
	return closeScan()
}
