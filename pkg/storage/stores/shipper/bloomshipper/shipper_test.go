package bloomshipper

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
)

func Test_Shipper_findBlocks(t *testing.T) {
	t.Run("expected block that are specified in tombstones to be filtered out", func(t *testing.T) {
		metas := []Meta{
			{
				Blocks: []BlockRef{
					//this blockRef is marked as deleted in the next meta
					createMatchingBlockRef(1),
					createMatchingBlockRef(2),
				},
			},
			{
				Blocks: []BlockRef{
					//this blockRef is marked as deleted in the next meta
					createMatchingBlockRef(3),
					createMatchingBlockRef(4),
				},
			},
			{
				Tombstones: []BlockRef{
					createMatchingBlockRef(1),
					createMatchingBlockRef(3),
				},
				Blocks: []BlockRef{
					createMatchingBlockRef(5),
				},
			},
		}

		ts := model.Now()

		interval := NewInterval(
			ts.Add(-2*time.Hour),
			ts.Add(-1*time.Hour),
		)
		blocks := BlocksForMetas(metas, interval, []v1.FingerprintBounds{{Min: 100, Max: 200}})

		expectedBlockRefs := []BlockRef{
			createMatchingBlockRef(2),
			createMatchingBlockRef(4),
			createMatchingBlockRef(5),
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
			ref := createBlockRef(data.minFingerprint, data.maxFingerprint, data.startTimestamp, data.endTimestamp)
			blocks := BlocksForMetas([]Meta{{Blocks: []BlockRef{ref}}}, NewInterval(300, 400), []v1.FingerprintBounds{{Min: 100, Max: 200}})
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
		b := createBlockRef(0, math.MaxUint64, startTs, endTs)
		isOutside := isOutsideRange(b, NewInterval(0, 900), []v1.FingerprintBounds{})
		require.True(t, isOutside)
	})

	t.Run("is outside if startTs == through ", func(t *testing.T) {
		b := createBlockRef(0, math.MaxUint64, startTs, endTs)
		isOutside := isOutsideRange(b, NewInterval(900, 1000), []v1.FingerprintBounds{})
		require.True(t, isOutside)
	})

	t.Run("is outside if endTs < from", func(t *testing.T) {
		b := createBlockRef(0, math.MaxUint64, startTs, endTs)
		isOutside := isOutsideRange(b, NewInterval(2100, 3000), []v1.FingerprintBounds{})
		require.True(t, isOutside)
	})

	t.Run("is outside if endFp < first fingerprint", func(t *testing.T) {
		b := createBlockRef(0, 90, startTs, endTs)
		isOutside := isOutsideRange(b, NewInterval(startTs, endTs), []v1.FingerprintBounds{{Min: 100, Max: 199}})
		require.True(t, isOutside)
	})

	t.Run("is outside if startFp > last fingerprint", func(t *testing.T) {
		b := createBlockRef(200, math.MaxUint64, startTs, endTs)
		isOutside := isOutsideRange(b, NewInterval(startTs, endTs), []v1.FingerprintBounds{{Min: 0, Max: 49}, {Min: 100, Max: 149}})
		require.True(t, isOutside)
	})

	t.Run("is outside if within gaps in fingerprints", func(t *testing.T) {
		b := createBlockRef(100, 199, startTs, endTs)
		isOutside := isOutsideRange(b, NewInterval(startTs, endTs), []v1.FingerprintBounds{{Min: 0, Max: 99}, {Min: 200, Max: 299}})
		require.True(t, isOutside)
	})

	t.Run("is not outside if within fingerprints 1", func(t *testing.T) {
		b := createBlockRef(10, 90, startTs, endTs)
		isOutside := isOutsideRange(b, NewInterval(startTs, endTs), []v1.FingerprintBounds{{Min: 0, Max: 99}, {Min: 200, Max: 299}})
		require.False(t, isOutside)
	})

	t.Run("is not outside if within fingerprints 2", func(t *testing.T) {
		b := createBlockRef(210, 290, startTs, endTs)
		isOutside := isOutsideRange(b, NewInterval(startTs, endTs), []v1.FingerprintBounds{{Min: 0, Max: 99}, {Min: 200, Max: 299}})
		require.False(t, isOutside)
	})

	t.Run("is not outside if spans across multiple fingerprint ranges", func(t *testing.T) {
		b := createBlockRef(50, 250, startTs, endTs)
		isOutside := isOutsideRange(b, NewInterval(startTs, endTs), []v1.FingerprintBounds{{Min: 0, Max: 99}, {Min: 200, Max: 299}})
		require.False(t, isOutside)
	})

	t.Run("is not outside if fingerprint range and time range are larger than block", func(t *testing.T) {
		b := createBlockRef(math.MaxUint64/3, math.MaxUint64/3*2, startTs, endTs)
		isOutside := isOutsideRange(b, NewInterval(0, 3000), []v1.FingerprintBounds{{Min: 0, Max: math.MaxUint64}})
		require.False(t, isOutside)
	})

	t.Run("is not outside if block fingerprint range is bigger that search keyspace", func(t *testing.T) {
		b := createBlockRef(0x0000, 0xffff, model.Earliest, model.Latest)
		isOutside := isOutsideRange(b, NewInterval(startTs, endTs), []v1.FingerprintBounds{{Min: 0x0100, Max: 0xff00}})
		require.False(t, isOutside)
	})
}

func createMatchingBlockRef(checksum uint32) BlockRef {
	block := createBlockRef(0, math.MaxUint64, model.Time(0), model.Time(math.MaxInt64))
	block.Checksum = checksum
	return block
}

func createBlockRef(
	minFingerprint, maxFingerprint uint64,
	startTimestamp, endTimestamp model.Time,
) BlockRef {
	day := startTimestamp.Unix() / int64(24*time.Hour/time.Second)
	return BlockRef{
		Ref: Ref{
			TenantID:       "fake",
			TableName:      fmt.Sprintf("%d", day),
			Bounds:         v1.NewBounds(model.Fingerprint(minFingerprint), model.Fingerprint(maxFingerprint)),
			StartTimestamp: startTimestamp,
			EndTimestamp:   endTimestamp,
			Checksum:       0,
		},
	}
}
