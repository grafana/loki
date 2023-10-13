package bloomshipper

import (
	"bytes"
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
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper/bloomshipperconfig"
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
		blocks := shipper.findBlocks(metas, 100, 200, 300, 400)

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
			blocks := shipper.findBlocks([]Meta{{Blocks: []BlockRef{ref}}}, 100, 200, 300, 400)
			if data.filtered {
				require.Empty(t, blocks)
				return
			}
			require.Len(t, blocks, 1)
			require.Equal(t, ref, blocks[0])
		})
	}
}

func createMatchingBlockRef(blockPath string) BlockRef {
	return createBlockRef(blockPath, 0, uint64(math.MaxUint64), 0, math.MaxInt)
}

func createBlockRef(
	blockPath string,
	minFingerprint uint64,
	maxFingerprint uint64,
	startTimestamp int64,
	endTimestamp int64,
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
	shipper := Shipper{config: bloomshipperconfig.Config{WorkingDirectory: workingDir}}
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
