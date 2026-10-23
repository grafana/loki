package dataobj

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
)

func TestDecoder_ExtendedMetadataRegionEnd(t *testing.T) {
	d := &decoder{}
	const startOff = int64(100)

	// testMaxBytes stands in for whatever maxBytes a caller passes in production (the cache backend's
	// own MaxItemBytes); extendedMetadataRegionEnd treats it as an opaque bound, so its exact value does not
	// matter here.
	const testMaxBytes = int64(64 << 20)

	// testSectionSpec is one section to build into a *filemd.Metadata via newTestMD.
	type testSectionSpec struct {
		isLogs         bool
		offset, length uint64
	}

	// newTestMD builds a minimal, valid *filemd.Metadata with one section per spec, in order, for
	// testing extendedMetadataRegionEnd's boundary, overflow, and section-selection logic directly.
	newTestMD := func(specs ...testSectionSpec) *filemd.Metadata {
		md := &filemd.Metadata{
			Dictionary: []string{"", "github.com/grafana/loki", "pointers", "logs"},
			Types: []*filemd.SectionType{
				nil,
				{NameRef: &filemd.SectionType_NameRef{NamespaceRef: 1, KindRef: 2}}, // 1: pointers
				{NameRef: &filemd.SectionType_NameRef{NamespaceRef: 1, KindRef: 3}}, // 2: logs
			},
		}
		for _, s := range specs {
			typeRef := uint32(1)
			if s.isLogs {
				typeRef = 2
			}
			md.Sections = append(md.Sections, &filemd.SectionInfo{
				TypeRef: typeRef,
				Layout:  &filemd.SectionLayout{Metadata: &filemd.Region{Offset: s.offset, Length: s.length}},
			})
		}
		return md
	}

	newTestSectionMD := func(offset, length uint64) *filemd.Metadata {
		return newTestMD(testSectionSpec{offset: offset, length: length})
	}

	t.Run("a region ending exactly at maxBytes is accepted but one byte more clamps to the file metadata only", func(t *testing.T) {
		atLimit := newTestSectionMD(0, uint64(testMaxBytes-startOff))
		end, err := d.extendedMetadataRegionEnd(atLimit, startOff, testMaxBytes)
		require.NoError(t, err)
		require.Equal(t, testMaxBytes, end)

		overLimit := newTestSectionMD(0, uint64(testMaxBytes-startOff)+1)
		end, err = d.extendedMetadataRegionEnd(overLimit, startOff, testMaxBytes)
		require.NoError(t, err)
		require.Equal(t, startOff, end, "one byte over maxBytes must still cache the file metadata, just not the section's")
	})

	t.Run("a smaller, later-listed section is still kept when a bigger, earlier-listed section does not fit", func(t *testing.T) {
		md := newTestMD(
			testSectionSpec{offset: 0, length: uint64(testMaxBytes) * 2}, // listed first, does not fit
			testSectionSpec{offset: 0, length: 1024},                     // listed second, fits comfortably
		)
		end, err := d.extendedMetadataRegionEnd(md, startOff, testMaxBytes)
		require.NoError(t, err)
		require.Equal(t, startOff+1024, end, "the smaller section must be kept even though it is listed after the one that does not fit")
	})

	t.Run("a logs section with an invalid layout does not block caching a valid non-logs section", func(t *testing.T) {
		md := newTestMD(
			testSectionSpec{isLogs: true, offset: uint64(math.MaxInt64) + 1, length: 0},
			testSectionSpec{offset: 0, length: 1024},
		)
		end, err := d.extendedMetadataRegionEnd(md, startOff, testMaxBytes)
		require.NoError(t, err)
		require.Equal(t, startOff+1024, end, "a logs section's layout is never validated, since its contribution is never used")
	})

	t.Run("the file metadata alone exceeding maxBytes is rejected outright since nothing would be left to cache", func(t *testing.T) {
		_, err := d.extendedMetadataRegionEnd(newTestSectionMD(0, 0), testMaxBytes+1, testMaxBytes)
		require.ErrorIs(t, err, errCannotCacheMetadata)
	})

	t.Run("a different maxBytes value is honored using the same boundary rule", func(t *testing.T) {
		const smallMax = int64(200)

		atLimit := newTestSectionMD(0, uint64(smallMax-startOff))
		end, err := d.extendedMetadataRegionEnd(atLimit, startOff, smallMax)
		require.NoError(t, err)
		require.Equal(t, smallMax, end)

		overLimit := newTestSectionMD(0, uint64(smallMax-startOff)+1)
		end, err = d.extendedMetadataRegionEnd(overLimit, startOff, smallMax)
		require.NoError(t, err)
		require.Equal(t, startOff, end, "exceeding a smaller maxBytes clamps to the file metadata only, same as exceeding a larger one")
	})

	t.Run("an offset or length above int64 max is rejected before any int64 conversion", func(t *testing.T) {
		_, err := d.extendedMetadataRegionEnd(newTestSectionMD(uint64(math.MaxInt64)+1, 0), startOff, testMaxBytes)
		require.ErrorIs(t, err, errCannotCacheMetadata)

		_, err = d.extendedMetadataRegionEnd(newTestSectionMD(0, uint64(math.MaxInt64)+1), startOff, testMaxBytes)
		require.ErrorIs(t, err, errCannotCacheMetadata)
	})

	t.Run("an offset and length that individually fit but overflow int64 when summed are rejected", func(t *testing.T) {
		// The sum, added to startOff as int64 arithmetic, wraps past math.MaxInt64 and back around to
		// a value smaller than startOff. The end < startOff check must catch this wraparound directly,
		// rather than relying on some other guard to happen to catch it.
		md := newTestSectionMD(uint64(math.MaxInt64), uint64(math.MaxInt64))
		_, err := d.extendedMetadataRegionEnd(md, startOff, testMaxBytes)
		require.ErrorIs(t, err, errCannotCacheMetadata)
	})
}
