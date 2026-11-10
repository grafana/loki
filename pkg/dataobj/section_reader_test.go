package dataobj

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/util/rangeio"
)

func Test_sectionReader_Ranges(t *testing.T) {
	t.Run("reads the ranges of the data region at their offsets in the file", func(t *testing.T) {
		rr := &recordingRangeReader{}
		sr := newTestSectionReader(rr)

		require.NoError(t, sr.DataRanges(t.Context(), []rangeio.Range{{Offset: 5, Data: make([]byte, 10)}, {Offset: 30, Data: make([]byte, 20)}}))

		require.Equal(t, []int64{1105, 1130}, rr.offsets)
	})

	t.Run("reads the ranges of the metadata region at their offsets in the file", func(t *testing.T) {
		rr := &recordingRangeReader{}
		sr := newTestSectionReader(rr)

		require.NoError(t, sr.MetadataRanges(t.Context(), []rangeio.Range{{Offset: 5, Data: make([]byte, 10)}}))

		require.Equal(t, []int64{1015}, rr.offsets)
	})

	t.Run("reads a range that ends at the end of the region", func(t *testing.T) {
		rr := &recordingRangeReader{}
		sr := newTestSectionReader(rr)

		require.NoError(t, sr.DataRanges(t.Context(), []rangeio.Range{{Offset: 40, Data: make([]byte, 10)}}))

		require.Equal(t, []int64{1140}, rr.offsets)
	})

	t.Run("rejects a range past the end of the region without a read", func(t *testing.T) {
		rr := &recordingRangeReader{}
		sr := newTestSectionReader(rr)

		err := sr.DataRanges(t.Context(), []rangeio.Range{{Offset: 5, Data: make([]byte, 10)}, {Offset: 45, Data: make([]byte, 20)}})

		require.ErrorContains(t, err, "section data is invalid")
		require.Empty(t, rr.offsets)
	})

	t.Run("rejects a metadata range past the end of the region without a read", func(t *testing.T) {
		rr := &recordingRangeReader{}
		sr := newTestSectionReader(rr)

		err := sr.MetadataRanges(t.Context(), []rangeio.Range{{Offset: 15, Data: make([]byte, 10)}})

		require.ErrorContains(t, err, "section data is invalid")
		require.Empty(t, rr.offsets)
	})

	t.Run("rejects a range with a negative offset without a read", func(t *testing.T) {
		rr := &recordingRangeReader{}
		sr := newTestSectionReader(rr)

		err := sr.DataRanges(t.Context(), []rangeio.Range{{Offset: -1, Data: make([]byte, 10)}})

		require.ErrorContains(t, err, "must not be negative")
		require.Empty(t, rr.offsets)
	})

	t.Run("returns an unexpected EOF, not an EOF, when the reader returns an EOF", func(t *testing.T) {
		sr := newTestSectionReader(&recordingRangeReader{err: io.EOF})

		err := sr.DataRanges(t.Context(), []rangeio.Range{{Offset: 5, Data: make([]byte, 10)}})

		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		require.NotErrorIs(t, err, io.EOF)
	})
}

// newTestSectionReader returns a section reader with the data region [100, 150) and the metadata region [10, 30)
// of a section that starts at offset 1000 of the file.
func newTestSectionReader(rr rangeReader) *sectionReader {
	return &sectionReader{
		rr:       rr,
		startOff: 1000,
		sec: &filemd.SectionInfo{Layout: &filemd.SectionLayout{
			Data:     &filemd.Region{Offset: 100, Length: 50},
			Metadata: &filemd.Region{Offset: 10, Length: 20},
		}},
	}
}

// recordingRangeReader records the offsets of the ranges that it reads, and returns err.
type recordingRangeReader struct {
	rangeReader

	offsets []int64
	err     error
}

func (r *recordingRangeReader) ReadRanges(_ context.Context, ranges []rangeio.Range) error {
	for _, rg := range ranges {
		r.offsets = append(r.offsets, rg.Offset)
	}
	return r.err
}
