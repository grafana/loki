package bloomshipper

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper/config"
)

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
					createMatchingBlockRef("block2"),
					createMatchingBlockRef("block4"),
					createMatchingBlockRef("block5"),
				},
			},
		}

		shipper := &Shipper{}
		blocks := shipper.findBlocks(metas, 300, 400, []uint64{100, 200})

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
		startTimestamp int64
		endTimestamp   int64
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
			blocks := shipper.findBlocks([]Meta{{Blocks: []BlockRef{ref}}}, 300, 400, []uint64{100, 110, 120, 130, 140, 150, 160, 170, 180, 190, 200})
			if data.filtered {
				require.Empty(t, blocks)
				return
			}
			require.Len(t, blocks, 1)
			require.Equal(t, ref, blocks[0])
		})
	}
}

func TestGetPosition(t *testing.T) {
	for i, tc := range []struct {
		s   []int
		v   int
		exp int
	}{
		{s: []int{}, v: 1, exp: 0},
		{s: []int{1, 2, 3}, v: 0, exp: 0},
		{s: []int{1, 2, 3}, v: 2, exp: 1},
		{s: []int{1, 2, 3}, v: 4, exp: 3},
		{s: []int{1, 2, 4, 5}, v: 3, exp: 2},
	} {
		tc := tc
		name := fmt.Sprintf("case-%d", i)
		t.Run(name, func(t *testing.T) {
			got := getPosition[[]int](tc.s, tc.v)
			require.Equal(t, tc.exp, got)
		})
	}
}

func TestIsOutsideRange(t *testing.T) {
	t.Run("is outside if startTs > through", func(t *testing.T) {
		b := createBlockRef("block", 0, math.MaxUint64, 100, 200)
		isOutside := isOutsideRange(&b, 0, 90, []uint64{})
		require.True(t, isOutside)
	})

	t.Run("is outside if endTs < from", func(t *testing.T) {
		b := createBlockRef("block", 0, math.MaxUint64, 100, 200)
		isOutside := isOutsideRange(&b, 210, 300, []uint64{})
		require.True(t, isOutside)
	})

	t.Run("is outside if endFp < first fingerprint", func(t *testing.T) {
		b := createBlockRef("block", 0, 90, 100, 200)
		isOutside := isOutsideRange(&b, 100, 200, []uint64{100, 200})
		require.True(t, isOutside)
	})

	t.Run("is outside if startFp > last fingerprint", func(t *testing.T) {
		b := createBlockRef("block", 210, math.MaxUint64, 100, 200)
		isOutside := isOutsideRange(&b, 100, 200, []uint64{100, 200})
		require.True(t, isOutside)
	})

	t.Run("is outside if within gaps in fingerprints", func(t *testing.T) {
		b := createBlockRef("block", 100, 200, 100, 200)
		isOutside := isOutsideRange(&b, 100, 200, []uint64{0, 99, 201, 300})
		require.True(t, isOutside)
	})

	t.Run("is not outside if within fingerprints 1", func(t *testing.T) {
		b := createBlockRef("block", 100, 200, 100, 200)
		isOutside := isOutsideRange(&b, 100, 200, []uint64{0, 100, 200, 300})
		require.False(t, isOutside)
	})

	t.Run("is not outside if within fingerprints 2", func(t *testing.T) {
		b := createBlockRef("block", 100, 150, 100, 200)
		isOutside := isOutsideRange(&b, 100, 200, []uint64{0, 100, 200, 300})
		require.False(t, isOutside)
	})

	t.Run("is not outside if within fingerprints 3", func(t *testing.T) {
		b := createBlockRef("block", 150, 200, 100, 200)
		isOutside := isOutsideRange(&b, 100, 200, []uint64{0, 100, 200, 300})
		require.False(t, isOutside)
	})
}

func createMatchingBlockRef(blockPath string) BlockRef {
	return createBlockRef(blockPath, 0, uint64(math.MaxUint64), 0, math.MaxInt)
}

func createBlockRef(
	blockPath string,
	minFingerprint, maxFingerprint uint64,
	startTimestamp, endTimestamp int64,
) BlockRef {
	return BlockRef{
		Ref: Ref{
			TenantID:       "fake",
			TableName:      "16600",
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

const (
	bloomFileName  = "bloom"
	seriesFileName = "series"
)

func Test_Shipper_extractBlock(t *testing.T) {
	dir := t.TempDir()

	mockBlockDir := filepath.Join(dir, "mock-block-dir")
	err := os.MkdirAll(mockBlockDir, 0777)
	require.NoError(t, err)
	bloomFile, err := os.Create(filepath.Join(mockBlockDir, bloomFileName))
	require.NoError(t, err)
	bloomFileContent := uuid.NewString()
	_, err = io.Copy(bloomFile, bytes.NewReader([]byte(bloomFileContent)))
	require.NoError(t, err)

	seriesFile, err := os.Create(filepath.Join(mockBlockDir, seriesFileName))
	require.NoError(t, err)
	seriesFileContent := uuid.NewString()
	_, err = io.Copy(seriesFile, bytes.NewReader([]byte(seriesFileContent)))
	require.NoError(t, err)

	blockFilePath := filepath.Join(dir, "test-block-archive")
	file, err := os.OpenFile(blockFilePath, os.O_CREATE|os.O_RDWR, 0700)
	require.NoError(t, err)
	err = v1.TarGz(file, v1.NewDirectoryBlockReader(mockBlockDir))
	require.NoError(t, err)

	blockFile, err := os.OpenFile(blockFilePath, os.O_RDONLY, 0700)
	require.NoError(t, err)

	workingDir := t.TempDir()
	shipper := Shipper{config: config.Config{WorkingDirectory: workingDir}}
	ts := time.Now().UTC()
	block := Block{
		BlockRef: BlockRef{BlockPath: "first-period-19621/tenantA/metas/ff-fff-1695272400-1695276000-aaa"},
		Data:     blockFile,
	}

	actualPath, err := shipper.extractBlock(&block, ts)

	require.NoError(t, err)
	expectedPath := filepath.Join(workingDir, block.BlockPath, strconv.FormatInt(ts.UnixMilli(), 10))
	require.Equal(t, expectedPath, actualPath,
		"expected archive to be extracted to working directory under the same path as blockPath and with timestamp suffix")
	require.FileExists(t, filepath.Join(expectedPath, bloomFileName))
	require.FileExists(t, filepath.Join(expectedPath, seriesFileName))

	actualBloomFileContent, err := os.ReadFile(filepath.Join(expectedPath, bloomFileName))
	require.NoError(t, err)
	require.Equal(t, bloomFileContent, string(actualBloomFileContent))

	actualSeriesFileContent, err := os.ReadFile(filepath.Join(expectedPath, seriesFileName))
	require.NoError(t, err)
	require.Equal(t, seriesFileContent, string(actualSeriesFileContent))
}
