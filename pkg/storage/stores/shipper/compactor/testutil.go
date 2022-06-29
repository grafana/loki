package compactor

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/klauspost/compress/gzip"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor/retention"
)

const (
	multiTenantIndexPrefix = "mti"
	sharedIndexPrefix      = "si"
)

type IndexFileConfig struct {
	CompressFile bool
}

type IndexRecords struct {
	Start, NumRecords int
}

func compressFile(t *testing.T, filepath string) {
	t.Helper()
	uncompressedFile, err := os.Open(filepath)
	require.NoError(t, err)

	compressedFile, err := os.Create(fmt.Sprintf("%s.gz", filepath))
	require.NoError(t, err)

	compressedWriter := gzip.NewWriter(compressedFile)

	_, err = io.Copy(compressedWriter, uncompressedFile)
	require.NoError(t, err)

	require.NoError(t, compressedWriter.Close())
	require.NoError(t, uncompressedFile.Close())
	require.NoError(t, compressedFile.Close())
	require.NoError(t, os.Remove(filepath))
}

type IndexesConfig struct {
	NumUnCompactedFiles, NumCompactedFiles int
}

func (c IndexesConfig) String() string {
	return fmt.Sprintf("Common Indexes - UCIFs: %d, CIFs: %d", c.NumUnCompactedFiles, c.NumCompactedFiles)
}

type PerUserIndexesConfig struct {
	IndexesConfig
	NumUsers int
}

func (c PerUserIndexesConfig) String() string {
	return fmt.Sprintf("Per User Indexes - UCIFs: %d, CIFs: %d, Users: %d", c.NumUnCompactedFiles, c.NumCompactedFiles, c.NumUsers)
}

func SetupTable(t *testing.T, path string, commonDBsConfig IndexesConfig, perUserDBsConfig PerUserIndexesConfig) {
	require.NoError(t, util.EnsureDirectory(path))
	// filename -> content
	commonIndexes := map[string]string{}
	// userID -> filename -> content
	perUserIndexes := map[string]map[string]string{}

	for i := 0; i < commonDBsConfig.NumUnCompactedFiles; i++ {
		commonIndexes[fmt.Sprintf("%s-%d", sharedIndexPrefix, i)] = fmt.Sprint(i)
	}

	for i := 0; i < commonDBsConfig.NumCompactedFiles; i++ {
		commonIndexes[fmt.Sprintf("%s-compactor-%d", sharedIndexPrefix, i)] = fmt.Sprint(i)
	}

	for i := 0; i < perUserDBsConfig.NumUnCompactedFiles; i++ {
		dbName := fmt.Sprintf("%s-%d", multiTenantIndexPrefix, i)
		commonIndexes[dbName] = ""
		for j := 0; j < perUserDBsConfig.NumUsers; j = j + 1 {
			commonIndexes[dbName] = commonIndexes[dbName] + BuildUserID(j) + "\n"
		}
	}

	for i := 0; i < perUserDBsConfig.NumCompactedFiles; i++ {
		for j := 0; j < perUserDBsConfig.NumUsers; j++ {
			userID := BuildUserID(j)
			if i == 0 {
				perUserIndexes[userID] = map[string]string{}
			}
			perUserIndexes[userID][fmt.Sprintf("compactor-%d", i)] = fmt.Sprint(i)
		}
	}

	idx := 0
	for filename, content := range commonIndexes {
		filePath := filepath.Join(path, filename)
		require.NoError(t, ioutil.WriteFile(filePath, []byte(content), 0777))
		if idx%2 == 0 {
			compressFile(t, filePath)
		}
		idx++
	}

	for userID, files := range perUserIndexes {
		require.NoError(t, util.EnsureDirectory(filepath.Join(path, userID)))
		for filename, content := range files {
			filePath := filepath.Join(path, userID, filename)
			require.NoError(t, ioutil.WriteFile(filePath, []byte(content), 0777))
			compressFile(t, filePath)
		}
	}
}

func BuildUserID(id int) string {
	return fmt.Sprintf("user-%d", id)
}

type compactedIndex struct {
	indexFile *os.File
}

func openCompactedIndex(path string) (*compactedIndex, error) {
	idxFile, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0755)
	if err != nil {
		return nil, err
	}

	return &compactedIndex{indexFile: idxFile}, nil
}

func (c compactedIndex) ForEachChunk(_ retention.ChunkEntryCallback) error {
	return nil
}

func (c compactedIndex) IndexChunk(_ chunk.Chunk) (bool, error) {
	return true, nil
}

func (c compactedIndex) CleanupSeries(_ []byte, _ labels.Labels) error {
	return nil
}

func (c compactedIndex) Cleanup() {
	_ = c.indexFile.Close()
}

func (c compactedIndex) ToIndexFile() (index.Index, error) {
	return c, nil
}

func (c compactedIndex) Name() string {
	return fmt.Sprintf("compactor-%d", time.Now().Unix())
}

func (c compactedIndex) Path() string {
	return c.indexFile.Name()
}

func (c compactedIndex) Close() error {
	return c.indexFile.Close()
}

func (c compactedIndex) Reader() (io.ReadSeeker, error) {
	_, err := c.indexFile.Seek(0, 0)
	if err != nil {
		return nil, err
	}
	return c.indexFile, nil
}

type testIndexCompactor struct{}

func newTestIndexCompactor() testIndexCompactor {
	return testIndexCompactor{}
}

func (i testIndexCompactor) NewTableCompactor(ctx context.Context, commonIndexSet IndexSet, existingUserIndexSet map[string]IndexSet, makeEmptyUserIndexSetFunc MakeEmptyUserIndexSetFunc, periodConfig config.PeriodConfig) TableCompactor {
	return newTestTableCompactor(ctx, commonIndexSet, existingUserIndexSet, makeEmptyUserIndexSetFunc, periodConfig)
}

type tableCompactor struct {
	ctx                       context.Context
	commonIndexSet            IndexSet
	existingUserIndexSet      map[string]IndexSet
	makeEmptyUserIndexSetFunc MakeEmptyUserIndexSetFunc
	periodConfig              config.PeriodConfig
}

func newTestTableCompactor(ctx context.Context, commonIndexSet IndexSet, existingUserIndexSet map[string]IndexSet, makeEmptyUserIndexSetFunc MakeEmptyUserIndexSetFunc, periodConfig config.PeriodConfig) tableCompactor {
	return tableCompactor{
		ctx:                       ctx,
		commonIndexSet:            commonIndexSet,
		existingUserIndexSet:      existingUserIndexSet,
		makeEmptyUserIndexSetFunc: makeEmptyUserIndexSetFunc,
		periodConfig:              periodConfig,
	}
}

func (t tableCompactor) CompactTable() error {
	sourceFiles := t.commonIndexSet.ListSourceFiles()
	perUserIndexes := map[string]CompactedIndex{}

	var commonCompactedIndex CompactedIndex

	if len(sourceFiles) > 1 || (len(sourceFiles) == 1 && !strings.Contains(sourceFiles[0].Name, "compactor")) {
		multiTenantIndexFilesCount := 0

		for _, sourceIndex := range t.commonIndexSet.ListSourceFiles() {
			if strings.HasPrefix(sourceIndex.Name, multiTenantIndexPrefix) {
				multiTenantIndexFilesCount++
			}

			srcFilePath, err := t.commonIndexSet.GetSourceFile(sourceIndex)
			if err != nil {
				return err
			}

			if strings.HasPrefix(sourceIndex.Name, multiTenantIndexPrefix) {
				srcFile, err := os.Open(srcFilePath)
				if err != nil {
					return err
				}

				scanner := bufio.NewScanner(srcFile)
				for scanner.Scan() {
					userID := scanner.Text()
					userIndex, ok := perUserIndexes[userID]
					if ok {
						_, err := userIndex.(*compactedIndex).indexFile.WriteString(sourceIndex.Name)
						if err != nil {
							return err
						}
						continue
					}

					userIdxSet, ok := t.existingUserIndexSet[userID]
					if !ok {
						userIdxSet, err = t.makeEmptyUserIndexSetFunc(userID)
						if err != nil {
							return err
						}
						t.existingUserIndexSet[userID] = userIdxSet
					}

					userIndex, err = openCompactedIndex(filepath.Join(userIdxSet.GetWorkingDir(), fmt.Sprintf("compactor-%d", time.Now().Unix())))
					if err != nil {
						return err
					}

					perUserIndexes[userID] = userIndex

					for _, idx := range userIdxSet.ListSourceFiles() {
						_, err := userIndex.(*compactedIndex).indexFile.WriteString(idx.Name)
						if err != nil {
							return err
						}
					}

					_, err := userIndex.(*compactedIndex).indexFile.WriteString(sourceIndex.Name)
					if err != nil {
						return err
					}
				}

				if err := srcFile.Close(); err != nil {
					return err
				}
			} else {
				if commonCompactedIndex == nil {
					commonCompactedIndex, err = openCompactedIndex(filepath.Join(t.commonIndexSet.GetWorkingDir(), fmt.Sprintf("compactor-%d", time.Now().Unix())))
					if err != nil {
						return err
					}
				}
				_, err := commonCompactedIndex.(*compactedIndex).indexFile.WriteString(sourceIndex.Name)
				if err != nil {
					return err
				}
			}
		}

		if err := t.commonIndexSet.SetCompactedIndex(commonCompactedIndex, true); err != nil {
			return err
		}
	}

	for userID, idxSet := range t.existingUserIndexSet {
		if _, ok := perUserIndexes[userID]; ok || len(idxSet.ListSourceFiles()) <= 1 {
			continue
		}

		var err error
		perUserIndexes[userID], err = openCompactedIndex(filepath.Join(idxSet.GetWorkingDir(), fmt.Sprintf("compactor-%d", time.Now().Unix())))
		if err != nil {
			return err
		}
		for _, srcFile := range idxSet.ListSourceFiles() {
			_, err := perUserIndexes[userID].(*compactedIndex).indexFile.WriteString(srcFile.Name)
			if err != nil {
				return err
			}
		}
	}

	for userID, userIndex := range perUserIndexes {
		if err := t.existingUserIndexSet[userID].SetCompactedIndex(userIndex, true); err != nil {
			return err
		}
	}

	return nil
}

func (i testIndexCompactor) OpenCompactedIndexFile(_ context.Context, path, _, _, _ string, _ config.PeriodConfig, _ log.Logger) (CompactedIndex, error) {
	return openCompactedIndex(path)
}
