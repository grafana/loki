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

	"github.com/grafana/loki/v3/pkg/dataobj/bucketstat"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/metadata"
	"github.com/grafana/loki/v3/pkg/util/rangeio"
	"github.com/grafana/loki/v3/pkg/xcap"
	"github.com/grafana/loki/v3/pkg/xcap/xcaptest"
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
		ctx, recorded := xcaptest.NewRegion(t)

		got, err := newTestMemorySectionReader().MetadataRanges(ctx, []RelativeByteRange{{Offset: 5, Length: 3}, {Offset: 0, Length: 4}, {Offset: 9, Length: 1}})

		require.NoError(t, err)
		require.Equal(t, [][]byte{[]byte("567"), []byte("0123"), []byte("9")}, got)
		require.Equal(t, metadataStats{}, recordedMetadataStats(recorded), "a reader over an io.ReaderAt sends no request")
	})

	t.Run("returns an empty slice for a range of zero length at the end of the region", func(t *testing.T) {
		ctx, recorded := xcaptest.NewRegion(t)

		got, err := newTestMemorySectionReader().MetadataRanges(ctx, []RelativeByteRange{{Offset: int64(len(testMetadataRegion)), Length: 0}})

		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Empty(t, got[0])
		require.Equal(t, metadataStats{}, recordedMetadataStats(recorded))
	})

	t.Run("records one merged request and its downloaded bytes for a partly prefetched range", func(t *testing.T) {
		sr := newTestBucketSectionReader(t)
		sr.rr = &prefetchedRangeReader{inner: sr.rr, ranges: metadata.PrefetchedRanges{{Offset: 14, Data: []byte("2345")}}}
		ctx, recorded := xcaptest.NewRegion(t)
		ctx = rangeio.WithConfig(ctx, &rangeio.Config{MaxParallelism: 1, CoalesceSize: 1 << 20, MaxRangeSize: 8 << 20, MinRangeSize: 1 << 20})

		got, err := sr.MetadataRanges(ctx, []RelativeByteRange{{Offset: 1, Length: 6}})

		require.NoError(t, err)
		require.Equal(t, [][]byte{[]byte("123456")}, got)
		require.Equal(t, metadataStats{fromStorage: 6, storageRequests: 1}, recordedMetadataStats(recorded), "the merged read downloads the prefetched bytes between the two gaps too")
	})

	t.Run("records every byte as storage and one request when no prefetched range wraps the bucket reader", func(t *testing.T) {
		ctx, recorded := xcaptest.NewRegion(t)
		xcap.RegionFromContext(ctx).Record(bucketstat.StatObjectRequestsGetRange.Observe(5))

		got, err := newTestBucketSectionReader(t).MetadataRanges(ctx, []RelativeByteRange{{Offset: 1, Length: 6}})

		require.NoError(t, err)
		require.Equal(t, [][]byte{[]byte("123456")}, got)
		require.Equal(t, metadataStats{fromStorage: 6, storageRequests: 1}, recordedMetadataStats(recorded), "the requests that other reads recorded in the same region do not count")
		require.Equal(t, int64(5+1), recorded(bucketstat.StatObjectRequestsGetRange), "the region of the context receives the object-store statistics of the reads")
		require.Equal(t, int64(6), recorded(bucketstat.StatObjectBytesDownloaded))
	})

	t.Run("records the request and no bytes when the read fails", func(t *testing.T) {
		ctx, recorded := xcaptest.NewRegion(t)
		sr := newTestMemorySectionReader()
		sr.rr = &bucketRangeReader{bucket: NewInstrumentedBucketReader(failingBucket{objstore.NewInMemBucket()}), path: "obj"}

		got, err := sr.MetadataRanges(ctx, []RelativeByteRange{{Offset: 1, Length: 6}})

		require.EqualError(t, err, "read failed")
		require.Nil(t, got)
		require.Equal(t, metadataStats{storageRequests: 1}, recordedMetadataStats(recorded))
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
				ctx, recorded := xcaptest.NewRegion(t)

				got, err := newTestMemorySectionReader().MetadataRanges(ctx, []RelativeByteRange{{Offset: 0, Length: 1}, tc.r})

				require.EqualError(t, err, tc.wantErr)
				require.Nil(t, got)
				require.Equal(t, metadataStats{}, recordedMetadataStats(recorded), "the reader checks every range before it reads any")
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
			{name: "the region offset and the start offset overflow int64", startOff: 10, region: &filemd.Region{Offset: math.MaxInt64 - 5}, wantErr: "section metadata region overflows: start offset=10 region offset=9223372036854775802 region length=0"},
			{name: "the region end overflows int64", region: &filemd.Region{Offset: math.MaxInt64 - 1, Length: 5}, wantErr: "section metadata region overflows: start offset=0 region offset=9223372036854775806 region length=5"},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				info := &filemd.SectionInfo{}
				if tc.region != nil {
					info.Layout = &filemd.SectionLayout{Metadata: tc.region}
				}

				// The section reader has a nil rangeReader, so a read before the validation panics and fails the test.
				sr := &sectionReader{sec: info, startOff: tc.startOff}

				ctx, recorded := xcaptest.NewRegion(t)

				got, err := sr.MetadataRanges(ctx, []RelativeByteRange{{Offset: 0, Length: 1}})

				require.EqualError(t, err, tc.wantErr)
				require.Nil(t, got)
				require.Equal(t, metadataStats{}, recordedMetadataStats(recorded))
			})
		}
	})
}

func TestSectionReader_DataRange(t *testing.T) {
	t.Run("returns the bytes of a range that starts after the start of the region", func(t *testing.T) {
		rc, err := newTestMemorySectionReader().DataRange(t.Context(), RelativeByteRange{Offset: 8, Length: 4})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, rc.Close()) })

		got, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.Equal(t, "data", string(got))
	})

	t.Run("returns the bytes of a range that ends at the end of the region", func(t *testing.T) {
		rc, err := newTestMemorySectionReader().DataRange(t.Context(), RelativeByteRange{Offset: 0, Length: int64(len(testDataRegion))})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, rc.Close()) })

		got, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.Equal(t, testDataRegion, string(got))
	})

	t.Run("returns an empty reader without a read for a range of zero length at the end of the region", func(t *testing.T) {
		// The section reader has a nil rangeReader, so a read panics and fails the test.
		sr := newTestMemorySectionReader()
		sr.rr = nil

		rc, err := sr.DataRange(t.Context(), RelativeByteRange{Offset: int64(len(testDataRegion)), Length: 0})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, rc.Close()) })

		got, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("returns an empty reader for a data region of zero length at the start of the object", func(t *testing.T) {
		sr := &sectionReader{sec: &filemd.SectionInfo{Layout: &filemd.SectionLayout{Data: &filemd.Region{}}}}

		rc, err := sr.DataRange(t.Context(), RelativeByteRange{Offset: 0, Length: 0})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, rc.Close()) })

		got, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("returns an error for a range that is not inside the data region", func(t *testing.T) {
		tests := []struct {
			name           string
			offset, length int64
			wantErr        string
		}{
			{name: "has one byte at the end of the region", offset: 12, length: 1, wantErr: "section data range is invalid: offset=12 length=1 region length=12"},
			{name: "ends one byte after the region", offset: 0, length: 13, wantErr: "section data range is invalid: offset=0 length=13 region length=12"},
			{name: "starts after the region", offset: 13, length: 0, wantErr: "section data range is invalid: offset=13 length=0 region length=12"},
			{name: "has a negative offset", offset: -1, length: 2, wantErr: "parameters must not be negative: offset=-1 length=2"},
			{name: "has a negative length", offset: 2, length: -1, wantErr: "parameters must not be negative: offset=2 length=-1"},
			{name: "has an end that overflows", offset: 5, length: math.MaxInt64, wantErr: "section data range is invalid: offset=5 length=9223372036854775807 region length=12"},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				rc, err := newTestMemorySectionReader().DataRange(t.Context(), RelativeByteRange{Offset: tc.offset, Length: tc.length})

				require.EqualError(t, err, tc.wantErr)
				require.Nil(t, rc)
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
			{name: "the section has no data region", wantErr: "section has no data"},
			{name: "the region offset is above MaxInt64", region: &filemd.Region{Offset: math.MaxInt64 + 1}, wantErr: "section data offset is too large"},
			{name: "the region length is above MaxInt64", region: &filemd.Region{Length: math.MaxInt64 + 1}, wantErr: "section data length is too large"},
			{name: "the region offset and the start offset overflow int64", startOff: 10, region: &filemd.Region{Offset: math.MaxInt64 - 5}, wantErr: "section data region overflows: start offset=10 region offset=9223372036854775802 region length=0"},
			{name: "the region end overflows int64", region: &filemd.Region{Offset: math.MaxInt64 - 1, Length: 5}, wantErr: "section data region overflows: start offset=0 region offset=9223372036854775806 region length=5"},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				info := &filemd.SectionInfo{}
				if tc.region != nil {
					info.Layout = &filemd.SectionLayout{Data: tc.region}
				}

				// The section reader has a nil rangeReader, so a read before the validation panics and fails the test.
				sr := &sectionReader{sec: info, startOff: tc.startOff}

				rc, err := sr.DataRange(t.Context(), RelativeByteRange{Offset: 0, Length: 0})

				require.EqualError(t, err, tc.wantErr)
				require.Nil(t, rc)
			})
		}
	})
}

// failingBucket fails every GetRange.
type failingBucket struct {
	objstore.Bucket
}

func (failingBucket) GetRange(context.Context, string, int64, int64) (io.ReadCloser, error) {
	return nil, errors.New("read failed")
}

// metadataStats holds the metadata statistics that a read recorded.
type metadataStats struct{ fromStorage, storageRequests int64 }

// recordedMetadataStats returns the metadata statistics that the function recorded holds. See
// [xcaptest.NewRegion].
func recordedMetadataStats(recorded func(xcap.Statistic) int64) metadataStats {
	return metadataStats{
		fromStorage:     recorded(metadata.StatMetadataBytesFromStorage),
		storageRequests: recorded(metadata.StatMetadataRequestsToStorage),
	}
}

// newTestBucketSectionReader returns the section reader of newTestMemorySectionReader, but over an
// in-memory bucket that records its requests, as FromBucket does.
func newTestBucketSectionReader(t *testing.T) *sectionReader {
	t.Helper()

	bucket := objstore.NewInMemBucket()
	require.NoError(t, bucket.Upload(t.Context(), "obj", bytes.NewReader([]byte(testObjectHeader+testMetadataPadding+testMetadataRegion+testDataRegion+testObjectTrailer))))

	sr := newTestMemorySectionReader()
	sr.rr = &bucketRangeReader{bucket: NewInstrumentedBucketReader(bucket), path: "obj"}
	return sr
}

// newTestMemorySectionReader returns a section reader that reads an object from memory, through an
// io.ReaderAt. The object has a header, a metadata region, a data region and a trailer, in this order.
func newTestMemorySectionReader() *sectionReader {
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
