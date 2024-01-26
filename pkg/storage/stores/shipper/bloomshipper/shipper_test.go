package bloomshipper

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func interval(start, end model.Time) Interval {
	return Interval{Start: start, End: end}
}

func Test_Shipper_findBlocks(t *testing.T) {
	t.Run("expected block that are specified in tombstones to be filtered out", func(t *testing.T) {
		metas := []Meta{
			{
				Blocks: []BlockRef{
					//this blockRef is marked as deleted in the next meta
					createMatchingBlockRef("block1"),
					createMatchingBlockRef("block2"),
				},
			},
			{
				Blocks: []BlockRef{
					//this blockRef is marked as deleted in the next meta
					createMatchingBlockRef("block3"),
					createMatchingBlockRef("block4"),
				},
			},
			{
				Tombstones: []BlockRef{
					createMatchingBlockRef("block1"),
					createMatchingBlockRef("block3"),
				},
				Blocks: []BlockRef{
					createMatchingBlockRef("block5"),
				},
			},
		}

		ts := model.Now()

		shipper := &Shipper{}
		interval := Interval{
			Start: ts.Add(-2 * time.Hour),
			End:   ts.Add(-1 * time.Hour),
		}
		blocks := shipper.findBlocks(metas, interval, []fpRange{{100, 200}})

		expectedBlockRefs := []BlockRef{
			createMatchingBlockRef("block2"),
			createMatchingBlockRef("block4"),
			createMatchingBlockRef("block5"),
		}
		require.ElementsMatch(t, expectedBlockRefs, blocks)
	})

	tests := map[string]struct {
		minFingerprint uint64
		maxFingerprint uint64
		startTimestamp model.Time
		endTimestamp   model.Time
		filtered       bool
	}{
		"expected block not to be filtered out if minFingerprint and startTimestamp are within range": {
			filtered: false,

			minFingerprint: 100,
			maxFingerprint: 220, // outside range
			startTimestamp: 300,
			endTimestamp:   401, // outside range
		},
		"expected block not to be filtered out if maxFingerprint and endTimestamp are within range": {
			filtered: false,

			minFingerprint: 50, // outside range
			maxFingerprint: 200,
			startTimestamp: 250, // outside range
			endTimestamp:   400,
		},
		"expected block to be filtered out if fingerprints are outside range": {
			filtered: true,

			minFingerprint: 50, // outside range
			maxFingerprint: 60, // outside range
			startTimestamp: 300,
			endTimestamp:   400,
		},
		"expected block to be filtered out if timestamps are outside range": {
			filtered: true,

			minFingerprint: 200,
			maxFingerprint: 100,
			startTimestamp: 401, // outside range
			endTimestamp:   500, // outside range
		},
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			shipper := &Shipper{}
			ref := createBlockRef("fake-block", data.minFingerprint, data.maxFingerprint, data.startTimestamp, data.endTimestamp)
			blocks := shipper.findBlocks([]Meta{{Blocks: []BlockRef{ref}}}, interval(300, 400), []fpRange{{100, 200}})
			if data.filtered {
				require.Empty(t, blocks)
				return
			}
			require.Len(t, blocks, 1)
			require.Equal(t, ref, blocks[0])
		})
	}
}

func TestIsOutsideRange(t *testing.T) {
	startTs := model.Time(1000)
	endTs := model.Time(2000)

	t.Run("is outside if startTs > through", func(t *testing.T) {
		b := createBlockRef("block", 0, math.MaxUint64, startTs, endTs)
		isOutside := isOutsideRange(b, interval(0, 900), []fpRange{})
		require.True(t, isOutside)
	})

	t.Run("is outside if startTs == through ", func(t *testing.T) {
		b := createBlockRef("block", 0, math.MaxUint64, startTs, endTs)
		isOutside := isOutsideRange(b, interval(900, 1000), []fpRange{})
		require.True(t, isOutside)
	})

	t.Run("is outside if endTs < from", func(t *testing.T) {
		b := createBlockRef("block", 0, math.MaxUint64, startTs, endTs)
		isOutside := isOutsideRange(b, interval(2100, 3000), []fpRange{})
		require.True(t, isOutside)
	})

	t.Run("is outside if endFp < first fingerprint", func(t *testing.T) {
		b := createBlockRef("block", 0, 90, startTs, endTs)
		isOutside := isOutsideRange(b, interval(startTs, endTs), []fpRange{{100, 199}})
		require.True(t, isOutside)
	})

	t.Run("is outside if startFp > last fingerprint", func(t *testing.T) {
		b := createBlockRef("block", 200, math.MaxUint64, startTs, endTs)
		isOutside := isOutsideRange(b, interval(startTs, endTs), []fpRange{{0, 49}, {100, 149}})
		require.True(t, isOutside)
	})

	t.Run("is outside if within gaps in fingerprints", func(t *testing.T) {
		b := createBlockRef("block", 100, 199, startTs, endTs)
		isOutside := isOutsideRange(b, interval(startTs, endTs), []fpRange{{0, 99}, {200, 299}})
		require.True(t, isOutside)
	})

	t.Run("is not outside if within fingerprints 1", func(t *testing.T) {
		b := createBlockRef("block", 10, 90, startTs, endTs)
		isOutside := isOutsideRange(b, interval(startTs, endTs), []fpRange{{0, 99}, {200, 299}})
		require.False(t, isOutside)
	})

	t.Run("is not outside if within fingerprints 2", func(t *testing.T) {
		b := createBlockRef("block", 210, 290, startTs, endTs)
		isOutside := isOutsideRange(b, interval(startTs, endTs), []fpRange{{0, 99}, {200, 299}})
		require.False(t, isOutside)
	})

	t.Run("is not outside if spans across multiple fingerprint ranges", func(t *testing.T) {
		b := createBlockRef("block", 50, 250, startTs, endTs)
		isOutside := isOutsideRange(b, interval(startTs, endTs), []fpRange{{0, 99}, {200, 299}})
		require.False(t, isOutside)
	})

	t.Run("is not outside if fingerprint range and time range are larger than block", func(t *testing.T) {
		b := createBlockRef("block", math.MaxUint64/3, math.MaxUint64/3*2, startTs, endTs)
		isOutside := isOutsideRange(b, interval(0, 3000), []fpRange{{0, math.MaxUint64}})
		require.False(t, isOutside)
	})

	t.Run("is not outside if block fingerprint range is bigger that search keyspace", func(t *testing.T) {
		b := createBlockRef("block", 0x0000, 0xffff, model.Earliest, model.Latest)
		isOutside := isOutsideRange(b, interval(startTs, endTs), []fpRange{{0x0100, 0xff00}})
		require.False(t, isOutside)
	})
}

func createMatchingBlockRef(blockPath string) BlockRef {
	return createBlockRef(blockPath, 0, math.MaxUint64, model.Time(0), model.Time(math.MaxInt64))
}

func createBlockRef(
	blockPath string,
	minFingerprint, maxFingerprint uint64,
	startTimestamp, endTimestamp model.Time,
) BlockRef {
	day := startTimestamp.Unix() / int64(24*time.Hour/time.Second)
	return BlockRef{
		Ref: Ref{
			TenantID:       "fake",
			TableName:      fmt.Sprintf("%d", day),
			MinFingerprint: minFingerprint,
			MaxFingerprint: maxFingerprint,
			StartTimestamp: startTimestamp,
			EndTimestamp:   endTimestamp,
			Checksum:       0,
		},
		// block path is unique, and it's used to distinguish the blocks so the rest of the fields might be skipped in this test
		BlockPath: blockPath,
	}
}
