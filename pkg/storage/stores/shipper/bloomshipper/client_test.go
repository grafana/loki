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

	awsio "github.com/aws/smithy-go/io"
	"github.com/go-kit/log"
	"github.com/google/uuid"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/config"
)

const (
	day = 24 * time.Hour
)

var (
	// table 19627
	fixedDay = Date(2023, time.September, 27, 0, 0, 0)
)

func Date(year int, month time.Month, day, hour, min, sec int) model.Time {
	date := time.Date(year, month, day, hour, min, sec, 0, time.UTC)
	return model.TimeFromUnixNano(date.UnixNano())
}

func parseDayTime(s string) config.DayTime {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return config.DayTime{
		Time: model.TimeFromUnix(t.Unix()),
	}
}

func Test_BloomClient_FetchMetas(t *testing.T) {
	store := createStore(t)

	var expected []Meta
	folder1 := store.storageConfig.NamedStores.Filesystem["folder-1"].Directory
	// must not be present in results because it is outside of time range
	createMetaInStorage(t, folder1, "first-period-19621", "tenantA", 0, 100, fixedDay.Add(-7*day))
	// must be present in the results
	expected = append(expected, createMetaInStorage(t, folder1, "first-period-19621", "tenantA", 0, 100, fixedDay.Add(-6*day)))
	// must not be present in results because it belongs to another tenant
	createMetaInStorage(t, folder1, "first-period-19621", "tenantB", 0, 100, fixedDay.Add(-6*day))
	// must be present in the results
	expected = append(expected, createMetaInStorage(t, folder1, "first-period-19621", "tenantA", 101, 200, fixedDay.Add(-6*day)))

	folder2 := store.storageConfig.NamedStores.Filesystem["folder-2"].Directory
	// must not be present in results because it's out of the time range
	createMetaInStorage(t, folder2, "second-period-19626", "tenantA", 0, 100, fixedDay.Add(-1*day))
	// must be present in the results
	expected = append(expected, createMetaInStorage(t, folder2, "second-period-19625", "tenantA", 0, 100, fixedDay.Add(-2*day)))
	// must not be present in results because it belongs to another tenant
	createMetaInStorage(t, folder2, "second-period-19624", "tenantB", 0, 100, fixedDay.Add(-3*day))

	searchParams := MetaSearchParams{
		TenantID: "tenantA",

		Keyspace: v1.NewBounds(50, 150),
		Interval: NewInterval(fixedDay.Add(-6*day), fixedDay.Add(-1*day-1*time.Hour)),
	}

	fetched, err := store.FetchMetas(context.Background(), searchParams)
	require.NoError(t, err)

	require.Equal(t, len(expected), len(fetched))
	require.ElementsMatch(t, expected, fetched)

	resolved, _, err := store.ResolveMetas(context.Background(), searchParams)
	require.NoError(t, err)

	var resolvedRefs []MetaRef
	for _, refs := range resolved {
		resolvedRefs = append(resolvedRefs, refs...)
	}
	for i := range resolvedRefs {
		require.Equal(t, fetched[i].MetaRef, resolvedRefs[i])
	}
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
				Date(2023, time.September, 21, 5, 0, 0),
				Date(2023, time.September, 21, 6, 0, 0),
				0xaaa,
				"ignored-file-path-during-uploading",
			),
			expectedStorage:  "folder-1",
			expectedFilePath: fmt.Sprintf("bloom/first-period-19621/tenantA/metas/%s-1695272400000-1695276000000-aaa", v1.NewBounds(0xff, 0xfff)),
		},
		"expected meta to be uploaded to the second folder": {
			source: createMetaEntity("tenantA",
				"second-period-19625",
				200,
				300,
				Date(2023, time.September, 25, 0, 0, 0),
				Date(2023, time.September, 25, 1, 0, 0),
				0xbbb,
				"ignored-file-path-during-uploading",
			),
			expectedStorage:  "folder-2",
			expectedFilePath: fmt.Sprintf("bloom/second-period-19625/tenantA/metas/%s-1695600000000-1695603600000-bbb", v1.NewBounds(200, 300)),
		},
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			bloomClient := createStore(t)

			err := bloomClient.PutMeta(context.Background(), data.source)
			require.NoError(t, err)

			directory := bloomClient.storageConfig.NamedStores.Filesystem[data.expectedStorage].Directory
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
				Date(2023, time.September, 21, 5, 0, 0),
				Date(2023, time.September, 21, 6, 0, 0),
				0xaaa,
				"ignored-file-path-during-uploading",
			),
			expectedStorage:  "folder-1",
			expectedFilePath: fmt.Sprintf("bloom/first-period-19621/tenantA/metas/%s-1695272400000-1695276000000-aaa", v1.NewBounds(0xff, 0xfff)),
		},
		"expected meta to be delete from the second folder": {
			source: createMetaEntity("tenantA",
				"second-period-19625",
				200,
				300,
				Date(2023, time.September, 25, 0, 0, 0),
				Date(2023, time.September, 25, 1, 0, 0),
				0xbbb,
				"ignored-file-path-during-uploading",
			),
			expectedStorage:  "folder-2",
			expectedFilePath: fmt.Sprintf("bloom/second-period-19625/tenantA/metas/%s-1695600000000-1695603600000-bbb", v1.NewBounds(200, 300)),
		},
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			bloomClient := createStore(t)
			directory := bloomClient.storageConfig.NamedStores.Filesystem[data.expectedStorage].Directory
			file := filepath.Join(directory, data.expectedFilePath)
			err := os.MkdirAll(file[:strings.LastIndex(file, delimiter)], 0755)
			require.NoError(t, err)
			err = os.WriteFile(file, []byte("dummy content"), 0700)
			require.NoError(t, err)

			err = bloomClient.DeleteMeta(context.Background(), data.source)
			require.NoError(t, err)

			require.NoFileExists(t, file)
		})
	}

}

func Test_BloomClient_GetBlocks(t *testing.T) {
	firstBlockRef := BlockRef{
		Ref: Ref{
			TenantID:       "tenantA",
			TableName:      "first-period-19621",
			Bounds:         v1.NewBounds(0xeeee, 0xffff),
			StartTimestamp: Date(2023, time.September, 21, 5, 0, 0),
			EndTimestamp:   Date(2023, time.September, 21, 6, 0, 0),
			Checksum:       1,
		},
	}
	secondBlockRef := BlockRef{
		Ref: Ref{
			TenantID:       "tenantA",
			TableName:      "second-period-19624",
			Bounds:         v1.NewBounds(0xaaaa, 0xbbbb),
			StartTimestamp: Date(2023, time.September, 24, 5, 0, 0),
			EndTimestamp:   Date(2023, time.September, 24, 6, 0, 0),
			Checksum:       2,
		},
	}

	bloomClient := createStore(t)
	fsNamedStores := bloomClient.storageConfig.NamedStores.Filesystem
	firstBlockFullPath := NewPrefixedResolver(
		fsNamedStores["folder-1"].Directory,
		defaultKeyResolver{},
	).Block(firstBlockRef).LocalPath()
	firstBlockData := createBlockFile(t, firstBlockFullPath)
	secondBlockFullPath := NewPrefixedResolver(
		fsNamedStores["folder-2"].Directory,
		defaultKeyResolver{},
	).Block(secondBlockRef).LocalPath()
	secondBlockData := createBlockFile(t, secondBlockFullPath)
	require.FileExists(t, firstBlockFullPath)
	require.FileExists(t, secondBlockFullPath)

	downloadedFirstBlock, err := bloomClient.GetBlock(context.Background(), firstBlockRef)
	require.NoError(t, err)
	firstBlockActualData, err := io.ReadAll(downloadedFirstBlock.Data)
	require.NoError(t, err)
	require.Equal(t, firstBlockData, string(firstBlockActualData))

	downloadedSecondBlock, err := bloomClient.GetBlock(context.Background(), secondBlockRef)
	require.NoError(t, err)
	secondBlockActualData, err := io.ReadAll(downloadedSecondBlock.Data)
	require.NoError(t, err)
	require.Equal(t, secondBlockData, string(secondBlockActualData))
}

func Test_BloomClient_PutBlocks(t *testing.T) {
	bloomClient := createStore(t)
	block := Block{
		BlockRef: BlockRef{
			Ref: Ref{
				TenantID:       "tenantA",
				TableName:      "first-period-19621",
				Bounds:         v1.NewBounds(0xeeee, 0xffff),
				StartTimestamp: Date(2023, time.September, 21, 5, 0, 0),
				EndTimestamp:   Date(2023, time.September, 21, 6, 0, 0),
				Checksum:       1,
			},
		},
		Data: awsio.ReadSeekNopCloser{ReadSeeker: bytes.NewReader([]byte("data"))},
	}
	_, err := bloomClient.PutBlocks(context.Background(), []Block{block})
	require.NoError(t, err)
	got, err := bloomClient.GetBlock(context.Background(), block.BlockRef)
	require.NoError(t, err)
	require.Equal(t, block.BlockRef, got.BlockRef)
	data, err := io.ReadAll(got.Data)
	require.NoError(t, err)
	require.Equal(t, "data", string(data))
}

func Test_BloomClient_DeleteBlocks(t *testing.T) {
	block := BlockRef{
		Ref: Ref{
			TenantID:       "tenantA",
			TableName:      "first-period-19621",
			Bounds:         v1.NewBounds(0xeeee, 0xffff),
			StartTimestamp: Date(2023, time.September, 21, 5, 0, 0),
			EndTimestamp:   Date(2023, time.September, 21, 6, 0, 0),
			Checksum:       1,
		},
	}

	bloomClient := createStore(t)
	fsNamedStores := bloomClient.storageConfig.NamedStores.Filesystem
	blockFullPath := NewPrefixedResolver(
		fsNamedStores["folder-1"].Directory,
		defaultKeyResolver{},
	).Block(block).LocalPath()
	_ = createBlockFile(t, blockFullPath)
	require.FileExists(t, blockFullPath)

	err := bloomClient.DeleteBlocks(context.Background(), []BlockRef{block})
	require.NoError(t, err)
	require.NoFileExists(t, blockFullPath)

}

func createBlockFile(t *testing.T, path string) string {
	err := os.MkdirAll(path[:strings.LastIndex(path, "/")], 0755)
	require.NoError(t, err)
	fileContent := uuid.NewString()
	err = os.WriteFile(path, []byte(fileContent), 0700)
	require.NoError(t, err)
	return fileContent
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
					Bounds:         v1.NewBounds(0xaaa, 0xbbb),
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

func createStore(t *testing.T) *BloomStore {
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
	store, err := NewBloomStore(periodicConfigs, storageConfig, metrics, cache.NewNoopCache(), nil, log.NewNopLogger())
	require.NoError(t, err)
	return store
}

func createPeriodConfigs() []config.PeriodConfig {
	periodicConfigs := []config.PeriodConfig{
		{
			ObjectType: "folder-1",
			// from 2023-09-20: table range [19620:19623]
			From: parseDayTime("2023-09-20"),
			IndexTables: config.IndexPeriodicTableConfig{
				PeriodicTableConfig: config.PeriodicTableConfig{
					Period: day,
					Prefix: "first-period-",
				}},
		},
		{
			ObjectType: "folder-2",
			// from 2023-09-24: table range [19624:19627]
			From: parseDayTime("2023-09-24"),
			IndexTables: config.IndexPeriodicTableConfig{
				PeriodicTableConfig: config.PeriodicTableConfig{
					Period: day,
					Prefix: "second-period-",
				}},
		},
	}
	return periodicConfigs
}

func createMetaInStorage(t *testing.T, folder string, tableName string, tenant string, minFingerprint uint64, maxFingerprint uint64, start model.Time) Meta {
	end := start.Add(12 * time.Hour)

	metaChecksum := rand.Uint32()
	// make sure this is equal to the createMetaObjectKey()
	metaFileName := fmt.Sprintf("%x-%x-%d-%d-%x", minFingerprint, maxFingerprint, start, end, metaChecksum)
	metaFilePath := filepath.Join(rootFolder, tableName, tenant, metasFolder, metaFileName)
	err := os.MkdirAll(filepath.Join(folder, metaFilePath[:strings.LastIndex(metaFilePath, delimiter)]), 0700)
	require.NoError(t, err)
	meta := createMetaEntity(tenant, tableName, minFingerprint, maxFingerprint, start, end, metaChecksum, metaFilePath)

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
	startTimestamp model.Time,
	endTimestamp model.Time,
	metaChecksum uint32,
	metaFilePath string) Meta {
	return Meta{
		MetaRef: MetaRef{
			Ref: Ref{
				TenantID:       tenant,
				TableName:      tableName,
				Bounds:         v1.NewBounds(model.Fingerprint(minFingerprint), model.Fingerprint(maxFingerprint)),
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
					Bounds:         v1.NewBounds(model.Fingerprint(minFingerprint), model.Fingerprint(maxFingerprint)),
					StartTimestamp: startTimestamp,
					EndTimestamp:   endTimestamp,
				},
			},
		},
		Blocks: []BlockRef{
			{
				Ref: Ref{
					TenantID:       tenant,
					Checksum:       metaChecksum + 2,
					Bounds:         v1.NewBounds(model.Fingerprint(minFingerprint), model.Fingerprint(maxFingerprint)),
					StartTimestamp: startTimestamp,
					EndTimestamp:   endTimestamp,
				},
			},
		},
	}
}
