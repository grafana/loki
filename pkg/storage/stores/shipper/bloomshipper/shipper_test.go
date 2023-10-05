package bloomshipper

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper/bloomshipperconfig"
)

func Test_Shipper_filterOutOutdated(t *testing.T) {
	tests := map[string]struct {
		metas             []Meta
		expectedBlockRefs []BlockRef
	}{
		"expected block that are specified in tombstones to be filtered out": {
			metas: []Meta{
				{
					Blocks: []BlockRef{
						//this blockRef is marked as deleted in the next meta
						createBlockRef("block1"),
						createBlockRef("block2"),
					},
				},
				{
					Blocks: []BlockRef{
						//this blockRef is marked as deleted in the next meta
						createBlockRef("block3"),
						createBlockRef("block4"),
					},
				},
				{
					Tombstones: []BlockRef{
						createBlockRef("block1"),
						createBlockRef("block3"),
					},
					Blocks: []BlockRef{
						createBlockRef("block2"),
						createBlockRef("block4"),
						createBlockRef("block5"),
					},
				},
			},
			expectedBlockRefs: []BlockRef{
				createBlockRef("block2"),
				createBlockRef("block4"),
				createBlockRef("block5"),
			},
		},
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			shipper := &Shipper{}
			blocks := shipper.filterOutOutdated(data.metas)

			require.ElementsMatch(t, data.expectedBlockRefs, blocks)
		})
	}
}

func createBlockRef(blockPath string) BlockRef {
	return BlockRef{
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
	blockFilePath := filepath.Join(dir, "test-block.zip")
	file, err := os.OpenFile(blockFilePath, os.O_CREATE|os.O_RDWR, 0700)
	require.NoError(t, err)
	writer := zip.NewWriter(file)

	bloomFileWriter, err := writer.Create(bloomFileName)
	require.NoError(t, err)
	bloomFileContent := uuid.NewString()
	_, err = io.Copy(bloomFileWriter, bytes.NewReader([]byte(bloomFileContent)))
	require.NoError(t, err)
	seriesFileWriter, err := writer.Create(seriesFileName)
	require.NoError(t, err)
	seriesFileContent := uuid.NewString()
	_, err = io.Copy(seriesFileWriter, bytes.NewReader([]byte(seriesFileContent)))
	require.NoError(t, err)
	err = writer.Close()
	require.NoError(t, err)
	err = file.Close()
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
