package index

import (
	"io"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
)

// StreamReader is the file-streaming counterpart to ByteSliceReader.
//
// Currently, this is just scaffolding: StreamReader delegates all calls to a *ByteSliceReader.
// Follow-up changes will progressively replace embedded calls with streaming implementations
// backed by a file-handle pool.
type StreamReader struct {
	mmapReader *ByteSliceReader
}

// NewStreamFileReader constructs a StreamReader against the given index file.
func NewStreamFileReader(path string) (*StreamReader, error) {
	mmapReader, err := NewMmapFileReader(path)
	if err != nil {
		return nil, err
	}
	return &StreamReader{mmapReader: mmapReader}, nil
}

func (s StreamReader) Version() int {
	return s.mmapReader.Version()
}

func (s StreamReader) RawFileReader() (io.ReadSeeker, error) {
	return s.mmapReader.RawFileReader()
}

func (s StreamReader) PostingsRanges() (map[labels.Label]Range, error) {
	return s.mmapReader.PostingsRanges()
}

func (s StreamReader) Bounds() (int64, int64) {
	return s.mmapReader.Bounds()
}

func (s StreamReader) Checksum() uint32 {
	return s.mmapReader.Checksum()
}

func (s StreamReader) Symbols() StringIter {
	return s.mmapReader.Symbols()
}

func (s StreamReader) SymbolTableSize() uint64 {
	return s.mmapReader.SymbolTableSize()
}

func (s StreamReader) LabelValues(name string, matchers ...*labels.Matcher) ([]string, error) {
	return s.mmapReader.LabelValues(name, matchers...)
}

func (s StreamReader) LabelNames(matchers ...*labels.Matcher) ([]string, error) {
	return s.mmapReader.LabelNames(matchers...)
}

func (s StreamReader) LabelValueFor(id storage.SeriesRef, label string) (string, error) {
	return s.mmapReader.LabelValueFor(id, label)
}

func (s StreamReader) LabelNamesFor(ids ...storage.SeriesRef) ([]string, error) {
	return s.mmapReader.LabelNamesFor(ids...)
}

func (s StreamReader) Series(id storage.SeriesRef, from int64, through int64, lbls *labels.Labels, chks *[]ChunkMeta) (uint64, error) {
	return s.mmapReader.Series(id, from, through, lbls, chks)
}

func (s StreamReader) ChunkStats(id storage.SeriesRef, from, through int64, lbls *labels.Labels, by map[string]struct{}) (uint64, ChunkStats, error) {
	return s.mmapReader.ChunkStats(id, from, through, lbls, by)
}

func (s StreamReader) Postings(name string, fpFilter FingerprintFilter, values ...string) (Postings, error) {
	return s.mmapReader.Postings(name, fpFilter, values...)
}

func (s StreamReader) Size() int64 {
	return s.mmapReader.Size()
}

func (s StreamReader) Close() error {
	return s.mmapReader.Close()
}
