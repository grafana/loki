package bloomshipper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	aws_io "github.com/aws/smithy-go/io"
	"github.com/google/uuid"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/config"
)

const (
	day = 24 * time.Hour
)

var (
	// table 19627
	fixedDay = model.TimeFromUnix(time.Date(2023, time.September, 27, 0, 0, 0, 0, time.UTC).Unix())
)

func Test_BloomClient_GetMetas(t *testing.T) {
	shipper := createShipper(t)

	var expected []Meta
	folder1 := shipper.storageConfig.NamedStores.Filesystem["folder-1"].Directory
	// must not be present in results because it is outside of time range
	createMetaInStorage(t, folder1, "first-period-19621", "tenantA", 0, 100, fixedDay.Add(-7*day))
	// must be present in the results
	expected = append(expected, createMetaInStorage(t, folder1, "first-period-19621", "tenantA", 0, 100, fixedDay.Add(-6*day)))
	// must not be present in results because it belongs to another tenant
	createMetaInStorage(t, folder1, "first-period-19621", "tenantB", 0, 100, fixedDay.Add(-6*day))
	// must be present in the results
	expected = append(expected, createMetaInStorage(t, folder1, "first-period-19621", "tenantA", 101, 200, fixedDay.Add(-6*day)))

	folder2 := shipper.storageConfig.NamedStores.Filesystem["folder-2"].Directory
	// must not be present in results because it's out of the time range
	createMetaInStorage(t, folder2, "second-period-19626", "tenantA", 0, 100, fixedDay.Add(-1*day))
	// must be present in the results
	expected = append(expected, createMetaInStorage(t, folder2, "second-period-19625", "tenantA", 0, 100, fixedDay.Add(-2*day)))
	// must not be present in results because it belongs to another tenant
	createMetaInStorage(t, folder2, "second-period-19624", "tenantB", 0, 100, fixedDay.Add(-3*day))

	actual, err := shipper.GetMetas(context.Background(), MetaSearchParams{
		TenantID:       "tenantA",
		MinFingerprint: 50,
		MaxFingerprint: 150,
		StartTimestamp: fixedDay.Add(-6 * day).Unix(),
		EndTimestamp:   fixedDay.Add(-1*day - 1*time.Hour).Unix(),
	})
	require.NoError(t, err)
	require.ElementsMatch(t, expected, actual)
}

func Test_BloomClient_PutMeta(t *testing.T) {
	tests := map[string]struct {
		source           Meta
		expectedFilePath string
		expectedStorage  string
	}{
		"expected meta to be uploaded to the first folder": {
			source: createMetaEntity("tenantA",
				"first-period-19621",
				0xff,
				0xfff,
				time.Date(2023, time.September, 21, 5, 0, 0, 0, time.UTC).Unix(),
				time.Date(2023, time.September, 21, 6, 0, 0, 0, time.UTC).Unix(),
				0xaaa,
				"ignored-file-path-during-uploading",
			),
			expectedStorage:  "folder-1",
			expectedFilePath: "bloom/first-period-19621/tenantA/metas/ff-fff-1695272400-1695276000-aaa",
		},
		"expected meta to be uploaded to the second folder": {
			source: createMetaEntity("tenantA",
				"second-period-19625",
				200,
				300,
				time.Date(2023, time.September, 25, 0, 0, 0, 0, time.UTC).Unix(),
				time.Date(2023, time.September, 25, 1, 0, 0, 0, time.UTC).Unix(),
				0xbbb,
				"ignored-file-path-during-uploading",
			),
			expectedStorage:  "folder-2",
			expectedFilePath: "bloom/second-period-19625/tenantA/metas/c8-12c-1695600000-1695603600-bbb",
		},
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			shipper := createShipper(t)

			err := shipper.PutMeta(context.Background(), data.source)
			require.NoError(t, err)

			directory := shipper.storageConfig.NamedStores.Filesystem[data.expectedStorage].Directory
			filePath := filepath.Join(directory, data.expectedFilePath)
			require.FileExists(t, filePath)
			content, err := os.ReadFile(filePath)
			require.NoError(t, err)
			result := Meta{}
			err = json.Unmarshal(content, &result)
			require.NoError(t, err)

			require.Equal(t, data.source.Blocks, result.Blocks)
			require.Equal(t, data.source.Tombstones, result.Tombstones)
		})
	}

}

func Test_BloomClient_DeleteMeta(t *testing.T) {
	tests := map[string]struct {
		source           Meta
		expectedFilePath string
		expectedStorage  string
	}{
		"expected meta to be deleted from the first folder": {
			source: createMetaEntity("tenantA",
				"first-period-19621",
				0xff,
				0xfff,
				time.Date(2023, time.September, 21, 5, 0, 0, 0, time.UTC).Unix(),
				time.Date(2023, time.September, 21, 6, 0, 0, 0, time.UTC).Unix(),
				0xaaa,
				"ignored-file-path-during-uploading",
			),
			expectedStorage:  "folder-1",
			expectedFilePath: "bloom/first-period-19621/tenantA/metas/ff-fff-1695272400-1695276000-aaa",
		},
		"expected meta to be delete from the second folder": {
			source: createMetaEntity("tenantA",
				"second-period-19625",
				200,
				300,
				time.Date(2023, time.September, 25, 0, 0, 0, 0, time.UTC).Unix(),
				time.Date(2023, time.September, 25, 1, 0, 0, 0, time.UTC).Unix(),
				0xbbb,
				"ignored-file-path-during-uploading",
			),
			expectedStorage:  "folder-2",
			expectedFilePath: "bloom/second-period-19625/tenantA/metas/c8-12c-1695600000-1695603600-bbb",
		},
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			shipper := createShipper(t)
			directory := shipper.storageConfig.NamedStores.Filesystem[data.expectedStorage].Directory
			file := filepath.Join(directory, data.expectedFilePath)
			err := os.MkdirAll(file[:strings.LastIndex(file, delimiter)], 0755)
			require.NoError(t, err)
			err = os.WriteFile(file, []byte("dummy content"), 0700)
			require.NoError(t, err)

			err = shipper.DeleteMeta(context.Background(), data.source)
			require.NoError(t, err)

			require.NoFileExists(t, file)
		})
	}

}

func Test_BloomClient_GetBlocks(t *testing.T) {
	shipper := createShipper(t)
	fsNamedStores := shipper.storageConfig.NamedStores.Filesystem
	firstBlockPath := "bloom/first-period-19621/tenantA/blooms/eeee-ffff/1695272400-1695276000-1"
	firstBlockFullPath := filepath.Join(fsNamedStores["folder-1"].Directory, firstBlockPath)
	firstBlockData := createBlockFile(t, firstBlockFullPath)
	secondBlockPath := "bloom/second-period-19624/tenantA/blooms/aaaa-bbbb/1695531600-1695535200-2"
	secondBlockFullPath := filepath.Join(fsNamedStores["folder-2"].Directory, secondBlockPath)
	secondBlockData := createBlockFile(t, secondBlockFullPath)
	require.FileExists(t, firstBlockFullPath)
	require.FileExists(t, secondBlockFullPath)

	firstBlockRef := BlockRef{
		Ref: Ref{
			TenantID:       "tenantA",
			TableName:      "first-period-19621",
			MinFingerprint: 0xeeee,
			MaxFingerprint: 0xffff,
			StartTimestamp: time.Date(2023, time.September, 21, 5, 0, 0, 0, time.UTC).Unix(),
			EndTimestamp:   time.Date(2023, time.September, 21, 6, 0, 0, 0, time.UTC).Unix(),
			Checksum:       1,
		},
		BlockPath: firstBlockPath,
	}
	secondBlockRef := BlockRef{
		Ref: Ref{
			TenantID:       "tenantA",
			TableName:      "second-period-19624",
			MinFingerprint: 0xaaaa,
			MaxFingerprint: 0xbbbb,
			StartTimestamp: time.Date(2023, time.September, 24, 5, 0, 0, 0, time.UTC).Unix(),
			EndTimestamp:   time.Date(2023, time.September, 24, 6, 0, 0, 0, time.UTC).Unix(),
			Checksum:       2,
		},
		BlockPath: secondBlockPath,
	}

	blocksToDownload := []BlockRef{firstBlockRef, secondBlockRef}

	blocksCh, errorsCh := shipper.GetBlocks(context.Background(), blocksToDownload)
	blocks := make(map[string]string)
	func() {
		timout := time.After(5 * time.Second)
		for {
			select {
			case <-timout:
				t.Fatalf("the test had to be completed before the timeout")
				return
			case err := <-errorsCh:
				require.NoError(t, err)
			case block, ok := <-blocksCh:
				if !ok {
					return
				}
				blockData, err := io.ReadAll(block.Data)
				require.NoError(t, err)
				blocks[block.BlockRef.BlockPath] = string(blockData)

			}
		}
	}()

	firstBlockActualData, exists := blocks[firstBlockRef.BlockPath]
	require.Truef(t, exists, "data for the first block must be present in the results: %+v", blocks)
	require.Equal(t, firstBlockData, firstBlockActualData)

	secondBlockActualData, exists := blocks[secondBlockRef.BlockPath]
	require.True(t, exists, "data for the second block must be present in the results: %+v", blocks)
	require.Equal(t, secondBlockData, secondBlockActualData)

	require.Len(t, blocks, 2)
}

func Test_BloomClient_PutBlocks(t *testing.T) {
	shipper := createShipper(t)
	blockForFirstFolderData := "data1"
	blockForFirstFolder := Block{
		BlockRef: BlockRef{
			Ref: Ref{
				TenantID:       "tenantA",
				TableName:      "first-period-19621",
				MinFingerprint: 0xeeee,
				MaxFingerprint: 0xffff,
				StartTimestamp: time.Date(2023, time.September, 21, 5, 0, 0, 0, time.UTC).Unix(),
				EndTimestamp:   time.Date(2023, time.September, 21, 6, 0, 0, 0, time.UTC).Unix(),
				Checksum:       1,
			},
			IndexPath: uuid.New().String(),
		},
		Data: aws_io.ReadSeekNopCloser{ReadSeeker: bytes.NewReader([]byte(blockForFirstFolderData))},
	}

	blockForSecondFolderData := "data2"
	blockForSecondFolder := Block{
		BlockRef: BlockRef{
			Ref: Ref{
				TenantID:       "tenantA",
				TableName:      "second-period-19624",
				MinFingerprint: 0xaaaa,
				MaxFingerprint: 0xbbbb,
				StartTimestamp: time.Date(2023, time.September, 24, 5, 0, 0, 0, time.UTC).Unix(),
				EndTimestamp:   time.Date(2023, time.September, 24, 6, 0, 0, 0, time.UTC).Unix(),
				Checksum:       2,
			},
			IndexPath: uuid.New().String(),
		},
		Data: aws_io.ReadSeekNopCloser{ReadSeeker: bytes.NewReader([]byte(blockForSecondFolderData))},
	}

	results, err := shipper.PutBlocks(context.Background(), []Block{blockForFirstFolder, blockForSecondFolder})
	require.NoError(t, err)
	require.Len(t, results, 2)
	firstResultBlock := results[0]
	path := firstResultBlock.BlockPath
	require.Equal(t, "bloom/first-period-19621/tenantA/blooms/eeee-ffff/1695272400-1695276000-1", path)
	require.Equal(t, blockForFirstFolder.TenantID, firstResultBlock.TenantID)
	require.Equal(t, blockForFirstFolder.TableName, firstResultBlock.TableName)
	require.Equal(t, blockForFirstFolder.MinFingerprint, firstResultBlock.MinFingerprint)
	require.Equal(t, blockForFirstFolder.MaxFingerprint, firstResultBlock.MaxFingerprint)
	require.Equal(t, blockForFirstFolder.StartTimestamp, firstResultBlock.StartTimestamp)
	require.Equal(t, blockForFirstFolder.EndTimestamp, firstResultBlock.EndTimestamp)
	require.Equal(t, blockForFirstFolder.Checksum, firstResultBlock.Checksum)
	require.Equal(t, blockForFirstFolder.IndexPath, firstResultBlock.IndexPath)
	folder1 := shipper.storageConfig.NamedStores.Filesystem["folder-1"].Directory
	savedFilePath := filepath.Join(folder1, path)
	require.FileExists(t, savedFilePath)
	savedData, err := os.ReadFile(savedFilePath)
	require.NoError(t, err)
	require.Equal(t, blockForFirstFolderData, string(savedData))

	secondResultBlock := results[1]
	path = secondResultBlock.BlockPath
	require.Equal(t, "bloom/second-period-19624/tenantA/blooms/aaaa-bbbb/1695531600-1695535200-2", path)
	require.Equal(t, blockForSecondFolder.TenantID, secondResultBlock.TenantID)
	require.Equal(t, blockForSecondFolder.TableName, secondResultBlock.TableName)
	require.Equal(t, blockForSecondFolder.MinFingerprint, secondResultBlock.MinFingerprint)
	require.Equal(t, blockForSecondFolder.MaxFingerprint, secondResultBlock.MaxFingerprint)
	require.Equal(t, blockForSecondFolder.StartTimestamp, secondResultBlock.StartTimestamp)
	require.Equal(t, blockForSecondFolder.EndTimestamp, secondResultBlock.EndTimestamp)
	require.Equal(t, blockForSecondFolder.Checksum, secondResultBlock.Checksum)
	require.Equal(t, blockForSecondFolder.IndexPath, secondResultBlock.IndexPath)
	folder2 := shipper.storageConfig.NamedStores.Filesystem["folder-2"].Directory

	savedFilePath = filepath.Join(folder2, path)
	require.FileExists(t, savedFilePath)
	savedData, err = os.ReadFile(savedFilePath)
	require.NoError(t, err)
	require.Equal(t, blockForSecondFolderData, string(savedData))
}

func Test_BloomClient_DeleteBlocks(t *testing.T) {
	shipper := createShipper(t)
	fsNamedStores := shipper.storageConfig.NamedStores.Filesystem
	block1Path := filepath.Join(fsNamedStores["folder-1"].Directory, "bloom/first-period-19621/tenantA/blooms/eeee-ffff/1695272400-1695276000-1")
	createBlockFile(t, block1Path)
	block2Path := filepath.Join(fsNamedStores["folder-2"].Directory, "bloom/second-period-19624/tenantA/blooms/aaaa-bbbb/1695531600-1695535200-2")
	createBlockFile(t, block2Path)
	require.FileExists(t, block1Path)
	require.FileExists(t, block2Path)

	blocksToDelete := []BlockRef{
		{
			Ref: Ref{
				TenantID:       "tenantA",
				TableName:      "second-period-19624",
				MinFingerprint: 0xaaaa,
				MaxFingerprint: 0xbbbb,
				StartTimestamp: time.Date(2023, time.September, 24, 5, 0, 0, 0, time.UTC).Unix(),
				EndTimestamp:   time.Date(2023, time.September, 24, 6, 0, 0, 0, time.UTC).Unix(),
				Checksum:       2,
			},
			IndexPath: uuid.New().String(),
		},
		{
			Ref: Ref{
				TenantID:       "tenantA",
				TableName:      "first-period-19621",
				MinFingerprint: 0xeeee,
				MaxFingerprint: 0xffff,
				StartTimestamp: time.Date(2023, time.September, 21, 5, 0, 0, 0, time.UTC).Unix(),
				EndTimestamp:   time.Date(2023, time.September, 21, 6, 0, 0, 0, time.UTC).Unix(),
				Checksum:       1,
			},
			IndexPath: uuid.New().String(),
		},
	}
	err := shipper.DeleteBlocks(context.Background(), blocksToDelete)
	require.NoError(t, err)
	require.NoFileExists(t, block1Path)
	require.NoFileExists(t, block2Path)
}

func createBlockFile(t *testing.T, path string) string {
	err := os.MkdirAll(path[:strings.LastIndex(path, "/")], 0755)
	require.NoError(t, err)
	fileContent := uuid.NewString()
	err = os.WriteFile(path, []byte(fileContent), 0700)
	require.NoError(t, err)
	return fileContent
}

func Test_TablesByPeriod(t *testing.T) {
	configs := createPeriodConfigs()
	firstPeriodFrom := configs[0].From
	secondPeriodFrom := configs[1].From
	tests := map[string]struct {
		from, to               model.Time
		expectedTablesByPeriod map[config.DayTime][]string
	}{
		"expected 1 table": {
			from: model.TimeFromUnix(time.Date(2023, time.September, 20, 0, 0, 0, 0, time.UTC).Unix()),
			to:   model.TimeFromUnix(time.Date(2023, time.September, 20, 23, 59, 59, 0, time.UTC).Unix()),
			expectedTablesByPeriod: map[config.DayTime][]string{
				firstPeriodFrom: {"first-period-19620"}},
		},
		"expected tables for both periods": {
			from: model.TimeFromUnix(time.Date(2023, time.September, 21, 0, 0, 0, 0, time.UTC).Unix()),
			to:   model.TimeFromUnix(time.Date(2023, time.September, 25, 23, 59, 59, 0, time.UTC).Unix()),
			expectedTablesByPeriod: map[config.DayTime][]string{
				firstPeriodFrom:  {"first-period-19621", "first-period-19622", "first-period-19623"},
				secondPeriodFrom: {"second-period-19624", "second-period-19625"},
			},
		},
		"expected tables for the second period": {
			from: model.TimeFromUnix(time.Date(2023, time.September, 24, 0, 0, 0, 0, time.UTC).Unix()),
			to:   model.TimeFromUnix(time.Date(2023, time.September, 25, 1, 0, 0, 0, time.UTC).Unix()),
			expectedTablesByPeriod: map[config.DayTime][]string{
				secondPeriodFrom: {"second-period-19624", "second-period-19625"},
			},
		},
		"expected only one table from the second period": {
			from: model.TimeFromUnix(time.Date(2023, time.September, 25, 0, 0, 0, 0, time.UTC).Unix()),
			to:   model.TimeFromUnix(time.Date(2023, time.September, 25, 1, 0, 0, 0, time.UTC).Unix()),
			expectedTablesByPeriod: map[config.DayTime][]string{
				secondPeriodFrom: {"second-period-19625"},
			},
		},
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			result := tablesByPeriod(configs, data.from, data.to)
			for periodFrom, expectedTables := range data.expectedTablesByPeriod {
				actualTables, exists := result[periodFrom]
				require.Truef(t, exists, "tables for %s period must be provided but was not in the result: %+v", periodFrom.String(), result)
				require.ElementsMatchf(t, expectedTables, actualTables, "tables mismatch for period %s", periodFrom.String())
			}
			require.Len(t, result, len(data.expectedTablesByPeriod))
		})
	}
}

func Test_createMetaRef(t *testing.T) {
	tests := map[string]struct {
		objectKey string
		tenantID  string
		tableName string

		expectedRef MetaRef
		expectedErr string
	}{
		"ValidObjectKey": {
			objectKey: "bloom/ignored-during-parsing-table-name/ignored-during-parsing-tenant-ID/metas/aaa-bbb-1234567890-9876543210-abcdef",
			tenantID:  "tenant1",
			tableName: "table1",
			expectedRef: MetaRef{
				Ref: Ref{
					TenantID:       "tenant1",
					TableName:      "table1",
					MinFingerprint: 0xaaa,
					MaxFingerprint: 0xbbb,
					StartTimestamp: 1234567890,
					EndTimestamp:   9876543210,
					Checksum:       0xabcdef,
				},
				FilePath: "bloom/ignored-during-parsing-table-name/ignored-during-parsing-tenant-ID/metas/aaa-bbb-1234567890-9876543210-abcdef",
			},
		},
		"InvalidObjectKeyDelimiterCount": {
			objectKey:   "invalid/key/with/too/many/objectKeyWithoutDelimiters",
			tenantID:    "tenant1",
			tableName:   "table1",
			expectedRef: MetaRef{},
			expectedErr: "filename parts count must be 5 but was 1: [objectKeyWithoutDelimiters]",
		},
		"InvalidMinFingerprint": {
			objectKey:   "invalid/folder/key/metas/zzz-bbb-123-9876543210-abcdef",
			tenantID:    "tenant1",
			tableName:   "table1",
			expectedErr: "error parsing minFingerprint zzz",
		},
		"InvalidMaxFingerprint": {
			objectKey:   "invalid/folder/key/metas/123-zzz-1234567890-9876543210-abcdef",
			tenantID:    "tenant1",
			tableName:   "table1",
			expectedErr: "error parsing maxFingerprint zzz",
		},
		"InvalidStartTimestamp": {
			objectKey:   "invalid/folder/key/metas/aaa-bbb-abc-9876543210-abcdef",
			tenantID:    "tenant1",
			tableName:   "table1",
			expectedErr: "error parsing startTimestamp abc",
		},
		"InvalidEndTimestamp": {
			objectKey:   "invalid/folder/key/metas/aaa-bbb-1234567890-xyz-abcdef",
			tenantID:    "tenant1",
			tableName:   "table1",
			expectedErr: "error parsing endTimestamp xyz",
		},
		"InvalidChecksum": {
			objectKey:   "invalid/folder/key/metas/aaa-bbb-1234567890-9876543210-ghijklm",
			tenantID:    "tenant1",
			tableName:   "table1",
			expectedErr: "error parsing checksum ghijklm",
		},
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			actualRef, err := createMetaRef(data.objectKey, data.tenantID, data.tableName)
			if data.expectedErr != "" {
				require.ErrorContains(t, err, data.expectedErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, data.expectedRef, actualRef)
		})
	}
}

func createShipper(t *testing.T) *BloomClient {
	periodicConfigs := createPeriodConfigs()
	namedStores := storage.NamedStores{
		Filesystem: map[string]storage.NamedFSConfig{
			"folder-1": {Directory: t.TempDir()},
			"folder-2": {Directory: t.TempDir()},
		}}
	//required to populate StoreType map in named config
	require.NoError(t, namedStores.Validate())
	storageConfig := storage.Config{NamedStores: namedStores}

	metrics := storage.NewClientMetrics()
	t.Cleanup(metrics.Unregister)
	bshipper, err := NewBloomClient(periodicConfigs, storageConfig, metrics)
	require.NoError(t, err)
	return bshipper
}

func createPeriodConfigs() []config.PeriodConfig {
	periodicConfigs := []config.PeriodConfig{
		{
			ObjectType: "folder-1",
			// from 2023-09-20: table range [19620:19623]
			From: config.DayTime{Time: model.TimeFromUnix(time.Date(2023, time.September, 20, 0, 0, 0, 0, time.UTC).Unix())},
			IndexTables: config.PeriodicTableConfig{
				Period: day,
				Prefix: "first-period-",
			},
		},
		{
			ObjectType: "folder-2",
			// from 2023-09-24: table range [19624:19627]
			From: config.DayTime{Time: model.TimeFromUnix(time.Date(2023, time.September, 24, 0, 0, 0, 0, time.UTC).Unix())},
			IndexTables: config.PeriodicTableConfig{
				Period: day,
				Prefix: "second-period-",
			},
		},
	}
	return periodicConfigs
}

func createMetaInStorage(t *testing.T, folder string, tableName string, tenant string, minFingerprint uint64, maxFingerprint uint64, start model.Time) Meta {
	startTimestamp := start.Unix()
	endTimestamp := start.Add(12 * time.Hour).Unix()

	metaChecksum := rand.Uint32()
	metaFileName := fmt.Sprintf("%x-%x-%v-%v-%x", minFingerprint, maxFingerprint, startTimestamp, endTimestamp, metaChecksum)
	metaFilePath := filepath.Join(rootFolder, tableName, tenant, metasFolder, metaFileName)
	err := os.MkdirAll(filepath.Join(folder, metaFilePath[:strings.LastIndex(metaFilePath, delimiter)]), 0700)
	require.NoError(t, err)
	meta := createMetaEntity(tenant, tableName, minFingerprint, maxFingerprint, startTimestamp, endTimestamp, metaChecksum, metaFilePath)

	metaFileContent, err := json.Marshal(meta)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(folder, metaFilePath), metaFileContent, 0644)
	require.NoError(t, err)
	return meta
}

func createMetaEntity(
	tenant string,
	tableName string,
	minFingerprint uint64,
	maxFingerprint uint64,
	startTimestamp int64,
	endTimestamp int64,
	metaChecksum uint32,
	metaFilePath string) Meta {
	return Meta{
		MetaRef: MetaRef{
			Ref: Ref{
				TenantID:       tenant,
				TableName:      tableName,
				MinFingerprint: minFingerprint,
				MaxFingerprint: maxFingerprint,
				StartTimestamp: startTimestamp,
				EndTimestamp:   endTimestamp,
				Checksum:       metaChecksum,
			},
			FilePath: metaFilePath,
		},
		Tombstones: []BlockRef{
			{
				Ref: Ref{
					TenantID:       tenant,
					Checksum:       metaChecksum + 1,
					MinFingerprint: minFingerprint,
					MaxFingerprint: maxFingerprint,
					StartTimestamp: startTimestamp,
					EndTimestamp:   endTimestamp,
				},
				IndexPath: uuid.New().String(),
				BlockPath: uuid.New().String(),
			},
		},
		Blocks: []BlockRef{
			{
				Ref: Ref{
					TenantID:       tenant,
					Checksum:       metaChecksum + 2,
					MinFingerprint: minFingerprint,
					MaxFingerprint: maxFingerprint,
					StartTimestamp: startTimestamp,
					EndTimestamp:   endTimestamp,
				},
				IndexPath: uuid.New().String(),
				BlockPath: uuid.New().String(),
			},
		},
	}
}
