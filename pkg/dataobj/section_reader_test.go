package dataobj

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/metadata"
	"github.com/grafana/loki/v3/pkg/util/rangeio"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// The test object starts with a header, so the section does not start at the start of the object.
// Padding comes before the metadata region and a trailer comes after the data region, so a read
// that leaves a region returns bytes instead of an error.
const (
	testObjectHeader    = "HEADER!!"
	testMetadataPadding = "abcd"
	testMetadataRegion  = "0123456789"
	testDataRegion      = "section data"
	testObjectTrailer   = "TRAILER"
)

func TestSectionReader_MetadataRanges(t *testing.T) {
	t.Run("returns the bytes of each range in the order of the ranges", func(t *testing.T) {
		got, err := newTestSectionReader().MetadataRanges(t.Context(), []RelativeByteRange{{Offset: 5, Length: 3}, {Offset: 0, Length: 4}, {Offset: 9, Length: 1}})

		require.NoError(t, err)
		require.Equal(t, [][]byte{[]byte("567"), []byte("0123"), []byte("9")}, got)
	})

	t.Run("returns an empty slice for a range of zero length at the end of the region", func(t *testing.T) {
		got, err := newTestSectionReader().MetadataRanges(t.Context(), []RelativeByteRange{{Offset: int64(len(testMetadataRegion)), Length: 0}})

		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Empty(t, got[0])
	})

	t.Run("records the bytes from memory and from storage and one merged request for a partly prefetched range", func(t *testing.T) {
		const region = "test"
		sr := newTestSectionReader()
		sr.rr = &prefetchedRangeReader{inner: sr.rr, ranges: metadata.PrefetchedRanges{{Offset: 14, Data: []byte("2345")}}}
		ctx := rangeio.WithConfig(t.Context(), &rangeio.Config{MaxParallelism: 1, CoalesceSize: 1 << 20, MaxRangeSize: 8 << 20, MinRangeSize: 1 << 20})
		ctx, capture := xcap.NewCapture(ctx, nil)
		ctx, _ = xcap.StartRegion(ctx, region)

		got, err := sr.MetadataRanges(ctx, []RelativeByteRange{{Offset: 1, Length: 6}})

		require.NoError(t, err)
		require.Equal(t, [][]byte{[]byte("123456")}, got)
		require.Equal(t, int64(4), xcap.ValueFromRegion[int64](capture, region, StatMetadataBytesFromMemory))
		require.Equal(t, int64(2), xcap.ValueFromRegion[int64](capture, region, StatMetadataBytesFromStorage))
		require.Equal(t, int64(1), xcap.ValueFromRegion[int64](capture, region, StatMetadataRequestsToStorage))
	})

	t.Run("records every byte as memory and no request when the reader reads from an io.ReaderAt", func(t *testing.T) {
		const region = "test"
		ctx, capture := xcap.NewCapture(t.Context(), nil)
		ctx, _ = xcap.StartRegion(ctx, region)

		got, err := newTestSectionReader().MetadataRanges(ctx, []RelativeByteRange{{Offset: 1, Length: 6}})

		require.NoError(t, err)
		require.Equal(t, [][]byte{[]byte("123456")}, got)
		require.Equal(t, int64(6), xcap.ValueFromRegion[int64](capture, region, StatMetadataBytesFromMemory))
		require.Zero(t, xcap.ValueFromRegion[int64](capture, region, StatMetadataBytesFromStorage))
		require.Zero(t, xcap.ValueFromRegion[int64](capture, region, StatMetadataRequestsToStorage))
	})

	t.Run("records every byte as storage and one request when no prefetched range wraps the bucket reader", func(t *testing.T) {
		const region = "test"
		bucket := objstore.NewInMemBucket()
		require.NoError(t, bucket.Upload(t.Context(), "obj", bytes.NewReader([]byte(testObjectHeader+testMetadataPadding+testMetadataRegion+testDataRegion+testObjectTrailer))))
		sr := newTestSectionReader()
		sr.rr = &bucketRangeReader{bucket: bucket, path: "obj"}
		ctx, capture := xcap.NewCapture(t.Context(), nil)
		ctx, _ = xcap.StartRegion(ctx, region)

		got, err := sr.MetadataRanges(ctx, []RelativeByteRange{{Offset: 1, Length: 6}})

		require.NoError(t, err)
		require.Equal(t, [][]byte{[]byte("123456")}, got)
		require.Zero(t, xcap.ValueFromRegion[int64](capture, region, StatMetadataBytesFromMemory))
		require.Equal(t, int64(6), xcap.ValueFromRegion[int64](capture, region, StatMetadataBytesFromStorage))
		require.Equal(t, int64(1), xcap.ValueFromRegion[int64](capture, region, StatMetadataRequestsToStorage))
	})

	t.Run("records the request of a failed read but no bytes", func(t *testing.T) {
		const region = "test"
		ctx, capture := xcap.NewCapture(t.Context(), nil)
		ctx, _ = xcap.StartRegion(ctx, region)
		sr := newTestSectionReader()
		sr.rr = hookedRangeReader{rangeReader: sr.rr, readRange: func(context.Context, int64, int64) (io.ReadCloser, error) {
			return nil, errors.New("read failed")
		}}

		got, err := sr.MetadataRanges(ctx, []RelativeByteRange{{Offset: 1, Length: 6}})

		require.EqualError(t, err, "read failed")
		require.Nil(t, got)
		require.Zero(t, xcap.ValueFromRegion[int64](capture, region, StatMetadataBytesFromMemory))
		require.Zero(t, xcap.ValueFromRegion[int64](capture, region, StatMetadataBytesFromStorage))
		require.Equal(t, int64(1), xcap.ValueFromRegion[int64](capture, region, StatMetadataRequestsToStorage))
	})

	t.Run("returns an error for a range that is not inside the metadata region", func(t *testing.T) {
		tests := []struct {
			name    string
			r       RelativeByteRange
			wantErr string
		}{
			{name: "has one byte at the end of the region", r: RelativeByteRange{Offset: 10, Length: 1}, wantErr: "section metadata range 1 is invalid: offset=10 length=1 region length=10"},
			{name: "ends one byte after the region", r: RelativeByteRange{Offset: 9, Length: 2}, wantErr: "section metadata range 1 is invalid: offset=9 length=2 region length=10"},
			{name: "starts after the region", r: RelativeByteRange{Offset: 11, Length: 0}, wantErr: "section metadata range 1 is invalid: offset=11 length=0 region length=10"},
			{name: "has a negative offset", r: RelativeByteRange{Offset: -1, Length: 2}, wantErr: "section metadata range 1 is invalid: offset=-1 length=2 region length=10"},
			{name: "has a negative length", r: RelativeByteRange{Offset: 2, Length: -1}, wantErr: "section metadata range 1 is invalid: offset=2 length=-1 region length=10"},
			{name: "has an end that overflows", r: RelativeByteRange{Offset: 1, Length: math.MaxInt64}, wantErr: "section metadata range 1 is invalid: offset=1 length=9223372036854775807 region length=10"},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				got, err := newTestSectionReader().MetadataRanges(t.Context(), []RelativeByteRange{{Offset: 0, Length: 1}, tc.r})

				require.EqualError(t, err, tc.wantErr)
				require.Nil(t, got)
			})
		}
	})

	t.Run("returns an error when the layout of the section is corrupt", func(t *testing.T) {
		tests := []struct {
			name     string
			startOff int64
			region   *filemd.Region
			wantErr  string
		}{
			{name: "the section has no metadata region", wantErr: "section has no metadata"},
			{name: "the region offset is above MaxInt64", region: &filemd.Region{Offset: math.MaxInt64 + 1}, wantErr: "section metadata region is too large"},
			{name: "the region length is above MaxInt64", region: &filemd.Region{Length: math.MaxInt64 + 1}, wantErr: "section metadata region is too large"},
			{name: "the region offset and the start offset overflow int64", startOff: 10, region: &filemd.Region{Offset: math.MaxInt64 - 5}, wantErr: "section metadata region overflows: offset=-9223372036854775804 length=0"},
			{name: "the region end overflows int64", region: &filemd.Region{Offset: math.MaxInt64 - 1, Length: 5}, wantErr: "section metadata region overflows: offset=9223372036854775806 length=5"},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				info := &filemd.SectionInfo{}
				if tc.region != nil {
					info.Layout = &filemd.SectionLayout{Metadata: tc.region}
				}

				// The section reader has a nil rangeReader, so a read before the validation panics and fails the test.
				sr := &sectionReader{sec: info, startOff: tc.startOff}

				got, err := sr.MetadataRanges(t.Context(), []RelativeByteRange{{Offset: 0, Length: 1}})

				require.EqualError(t, err, tc.wantErr)
				require.Nil(t, got)
			})
		}
	})
}

func TestSectionReader_DataRange(t *testing.T) {
	t.Run("returns the bytes of a range that starts after the start of the region", func(t *testing.T) {
		rc, err := newTestSectionReader().DataRange(t.Context(), RelativeByteRange{Offset: 8, Length: 4})
		require.NoError(t, err)
		defer rc.Close()

		got, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.Equal(t, "data", string(got))
	})

	t.Run("returns the bytes of a range that ends at the end of the region", func(t *testing.T) {
		rc, err := newTestSectionReader().DataRange(t.Context(), RelativeByteRange{Offset: 0, Length: int64(len(testDataRegion))})
		require.NoError(t, err)
		defer rc.Close()

		got, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.Equal(t, testDataRegion, string(got))
	})

	t.Run("returns an error for a range that is not inside the data region", func(t *testing.T) {
		tests := []struct {
			name           string
			offset, length int64
			wantErr        string
		}{
			{name: "has one byte at the end of the region", offset: 12, length: 1, wantErr: "section data range is invalid: offset=12 length=1 region length=12"},
			{name: "ends one byte after the region", offset: 0, length: 13, wantErr: "section data range is invalid: offset=0 length=13 region length=12"},
			{name: "has a negative offset", offset: -1, length: 2, wantErr: "parameters must not be negative: offset=-1 length=2"},
			{name: "has an end that overflows", offset: 5, length: math.MaxInt64, wantErr: "section data range is invalid: offset=5 length=9223372036854775807 region length=12"},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				rc, err := newTestSectionReader().DataRange(t.Context(), RelativeByteRange{Offset: tc.offset, Length: tc.length})

				require.EqualError(t, err, tc.wantErr)
				require.Nil(t, rc)
			})
		}
	})
}

// newTestSectionReader returns a section reader over an object with a header, a metadata region, a
// data region and a trailer, in this order.
func newTestSectionReader() *sectionReader {
	object := []byte(testObjectHeader + testMetadataPadding + testMetadataRegion + testDataRegion + testObjectTrailer)
	return &sectionReader{
		rr: &readerAtRangeReader{size: int64(len(object)), r: bytes.NewReader(object)},
		sec: &filemd.SectionInfo{Layout: &filemd.SectionLayout{
			Metadata: &filemd.Region{Offset: uint64(len(testMetadataPadding)), Length: uint64(len(testMetadataRegion))},
			Data:     &filemd.Region{Offset: uint64(len(testMetadataPadding) + len(testMetadataRegion)), Length: uint64(len(testDataRegion))},
		}},
		startOff: int64(len(testObjectHeader)),
	}
}
