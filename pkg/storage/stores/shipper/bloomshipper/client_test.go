package bloomshipper

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
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

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/storage"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/config"
	bloomshipperconfig "github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper/config"
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
	// metas that belong to 1st schema stored in folder-1
	// must not be present in results because it is outside of time range
	createMetaInStorage(t, store, "19621", "tenantA", 0, 100, fixedDay.Add(-7*day))
	// must be present in the results
	expected = append(expected, createMetaInStorage(t, store, "19621", "tenantA", 0, 100, fixedDay.Add(-6*day)))
	// must not be present in results because it belongs to another tenant
	createMetaInStorage(t, store, "19621", "tenantB", 0, 100, fixedDay.Add(-6*day))
	// must be present in the results
	expected = append(expected, createMetaInStorage(t, store, "19621", "tenantA", 101, 200, fixedDay.Add(-6*day)))

	// metas that belong to 2nd schema stored in folder-2
	// must not be present in results because it's out of the time range
	createMetaInStorage(t, store, "19626", "tenantA", 0, 100, fixedDay.Add(-1*day))
	// must be present in the results
	expected = append(expected, createMetaInStorage(t, store, "19625", "tenantA", 0, 100, fixedDay.Add(-2*day)))
	// must not be present in results because it belongs to another tenant
	createMetaInStorage(t, store, "19624", "tenantB", 0, 100, fixedDay.Add(-3*day))

	searchParams := MetaSearchParams{
		TenantID: "tenantA",
		Keyspace: v1.NewBounds(50, 150),
		Interval: NewInterval(fixedDay.Add(-6*day), fixedDay.Add(-1*day-1*time.Hour)),
	}

	fetched, err := store.FetchMetas(context.Background(), searchParams)
	require.NoError(t, err)

	require.Equal(t, len(expected), len(fetched))
	for i := range expected {
		require.Equal(t, expected[i].String(), fetched[i].String())
		require.ElementsMatch(t, expected[i].Blocks, fetched[i].Blocks)
		require.ElementsMatch(t, expected[i].Tombstones, fetched[i].Tombstones)
	}

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
				"table_19621",
				0xff,
				0xfff,
				Date(2023, time.September, 21, 5, 0, 0),
				Date(2023, time.September, 21, 6, 0, 0),
			),
			expectedStorage:  "folder-1",
			expectedFilePath: "bloom/table_19621/tenantA/metas/00000000000000ff-0000000000000fff-0",
		},
		"expected meta to be uploaded to the second folder": {
			source: createMetaEntity("tenantA",
				"table_19625",
				200,
				300,
				Date(2023, time.September, 25, 0, 0, 0),
				Date(2023, time.September, 25, 1, 0, 0),
			),
			expectedStorage:  "folder-2",
			expectedFilePath: "bloom/table_19625/tenantA/metas/00000000000000c8-000000000000012c-0",
		},
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			bloomClient := createStore(t)

			err := bloomClient.PutMeta(context.Background(), data.source)
			require.NoError(t, err)

			directory := bloomClient.storageConfig.NamedStores.Filesystem[data.expectedStorage].Directory
			filePath := filepath.Join(directory, data.expectedFilePath)
			require.FileExistsf(t, filePath, data.source.String())
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
				"table_19621",
				0xff,
				0xfff,
				Date(2023, time.September, 21, 5, 0, 0),
				Date(2023, time.September, 21, 6, 0, 0),
			),
			expectedStorage:  "folder-1",
			expectedFilePath: "bloom/table_19621/tenantA/metas/00000000000000ff-0000000000000fff-0",
		},
		"expected meta to be delete from the second folder": {
			source: createMetaEntity("tenantA",
				"table_19625",
				200,
				300,
				Date(2023, time.September, 25, 0, 0, 0),
				Date(2023, time.September, 25, 1, 0, 0),
			),
			expectedStorage:  "folder-2",
			expectedFilePath: "bloom/table_19625/tenantA/metas/00000000000000c8-000000000000012c-0",
		},
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			bloomClient := createStore(t)

			directory := bloomClient.storageConfig.NamedStores.Filesystem[data.expectedStorage].Directory
			file := filepath.Join(directory, data.expectedFilePath)

			// requires that Test_BloomClient_PutMeta does not fail
			err := bloomClient.PutMeta(context.Background(), data.source)
			require.NoError(t, err)
			require.FileExists(t, file, data.source.String())

			err = bloomClient.DeleteMetas(context.Background(), []MetaRef{data.source.MetaRef})
			require.NoError(t, err)

			require.NoFileExists(t, file, data.source.String())
		})
	}

}

func Test_BloomClient_GetBlocks(t *testing.T) {
	firstBlockRef := BlockRef{
		Ref: Ref{
			TenantID:       "tenantA",
			TableName:      "schema_a_table_19621",
			Bounds:         v1.NewBounds(0xeeee, 0xffff),
			StartTimestamp: Date(2023, time.September, 21, 5, 0, 0),
			EndTimestamp:   Date(2023, time.September, 21, 6, 0, 0),
			Checksum:       1,
		},
	}
	secondBlockRef := BlockRef{
		Ref: Ref{
			TenantID:       "tenantA",
			TableName:      "schema_b_table_19624",
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
	_ = createBlockFile(t, firstBlockFullPath)
	require.FileExists(t, firstBlockFullPath)

	secondBlockFullPath := NewPrefixedResolver(
		fsNamedStores["folder-2"].Directory,
		defaultKeyResolver{},
	).Block(secondBlockRef).LocalPath()
	_ = createBlockFile(t, secondBlockFullPath)
	require.FileExists(t, secondBlockFullPath)

	_, err := bloomClient.GetBlock(context.Background(), firstBlockRef)
	require.NoError(t, err)
	// firstBlockActualData, err := io.ReadAll(downloadedFirstBlock.Data)
	// require.NoError(t, err)
	// require.Equal(t, firstBlockData, string(firstBlockActualData))

	_, err = bloomClient.GetBlock(context.Background(), secondBlockRef)
	require.NoError(t, err)
	// secondBlockActualData, err := io.ReadAll(downloadedSecondBlock.Data)
	// require.NoError(t, err)
	// require.Equal(t, secondBlockData, string(secondBlockActualData))
}

func Test_BloomClient_PutBlocks(t *testing.T) {
	bloomClient := createStore(t)
	block := Block{
		BlockRef: BlockRef{
			Ref: Ref{
				TenantID:       "tenantA",
				TableName:      "table_19621",
				Bounds:         v1.NewBounds(0xeeee, 0xffff),
				StartTimestamp: Date(2023, time.September, 21, 5, 0, 0),
				EndTimestamp:   Date(2023, time.September, 21, 6, 0, 0),
				Checksum:       1,
			},
		},
		Data: awsio.ReadSeekNopCloser{ReadSeeker: bytes.NewReader([]byte("data"))},
	}
	err := bloomClient.PutBlock(context.Background(), block)
	require.NoError(t, err)

	_ = bloomClient.storeDo(block.StartTimestamp, func(s *bloomStoreEntry) error {
		c := s.bloomClient.(*BloomClient)
		rc, _, err := c.client.GetObject(context.Background(), block.BlockRef.String())
		require.NoError(t, err)
		data, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.Equal(t, "data", string(data))
		return nil
	})
}

func Test_BloomClient_DeleteBlocks(t *testing.T) {
	block := BlockRef{
		Ref: Ref{
			TenantID:       "tenantA",
			TableName:      "table_19621",
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

func createBlockFile(t *testing.T, dst string) string {
	err := os.MkdirAll(dst[:strings.LastIndex(dst, "/")], 0755)
	require.NoError(t, err)
	fileContent := uuid.NewString()

	src := filepath.Join(t.TempDir(), fileContent)
	err = os.WriteFile(src, []byte(fileContent), 0700)
	require.NoError(t, err)

	fp, err := os.OpenFile(dst, os.O_CREATE|os.O_RDWR, 0700)
	require.NoError(t, err)
	defer fp.Close()

	TarGz(t, fp, src)

	return fileContent
}

func TarGz(t *testing.T, dst io.Writer, file string) {
	src, err := os.Open(file)
	require.NoError(t, err)
	defer src.Close()

	gzipper := chunkenc.GetWriterPool(chunkenc.EncGZIP).GetWriter(dst)
	defer gzipper.Close()

	tarballer := tar.NewWriter(gzipper)
	defer tarballer.Close()

	for _, f := range []*os.File{src} {
		info, err := f.Stat()
		require.NoError(t, err)

		header, err := tar.FileInfoHeader(info, f.Name())
		require.NoError(t, err)

		err = tarballer.WriteHeader(header)
		require.NoError(t, err)

		_, err = io.Copy(tarballer, f)
		require.NoError(t, err)
	}
}

func Test_ParseMetaKey(t *testing.T) {
	tests := map[string]struct {
		objectKey   string
		expectedRef MetaRef
		expectedErr string
	}{
		"ValidObjectKey": {
			objectKey: "bloom/table/tenant/metas/aaa-bbb-abcdef",
			expectedRef: MetaRef{
				Ref: Ref{
					TenantID:       "tenant",
					TableName:      "table",
					Bounds:         v1.NewBounds(0xaaa, 0xbbb),
					StartTimestamp: 0, // ignored
					EndTimestamp:   0, // ignored
					Checksum:       0xabcdef,
				},
			},
		},
		"InvalidObjectKeyDelimiterCount": {
			objectKey:   "invalid/key/with/too/many/objectKeyWithoutDelimiters",
			expectedRef: MetaRef{},
			expectedErr: "failed to split filename parts",
		},
		"InvalidMinFingerprint": {
			objectKey:   "invalid/folder/key/metas/zzz-bbb-abcdef",
			expectedErr: "failed to parse bounds",
		},
		"InvalidMaxFingerprint": {
			objectKey:   "invalid/folder/key/metas/123-zzz-abcdef",
			expectedErr: "failed to parse bounds",
		},
		"InvalidChecksum": {
			objectKey:   "invalid/folder/key/metas/aaa-bbb-ghijklm",
			expectedErr: "failed to parse checksum",
		},
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			actualRef, err := defaultKeyResolver{}.ParseMetaKey(key(data.objectKey))
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
	storageConfig := storage.Config{
		NamedStores: namedStores,
		BloomShipperConfig: bloomshipperconfig.Config{
			WorkingDirectory: t.TempDir(),
			BlocksDownloadingQueue: bloomshipperconfig.DownloadingQueueConfig{
				WorkersCount: 1,
			},
		},
	}

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
					// TODO(chaudum): Integrate {,Parse}MetaKey into schema config
					// Prefix: "schema_a_table_",
				}},
		},
		{
			ObjectType: "folder-2",
			// from 2023-09-24: table range [19624:19627]
			From: parseDayTime("2023-09-24"),
			IndexTables: config.IndexPeriodicTableConfig{
				PeriodicTableConfig: config.PeriodicTableConfig{
					Period: day,
					// TODO(chaudum): Integrate {,Parse}MetaKey into schema config
					// Prefix: "schema_b_table_",
				}},
		},
	}
	return periodicConfigs
}

func createMetaInStorage(t *testing.T, s Client, tableName string, tenant string, minFingerprint uint64, maxFingerprint uint64, start model.Time) Meta {
	end := start.Add(12 * time.Hour)

	meta := createMetaEntity(tenant, tableName, minFingerprint, maxFingerprint, start, end)
	err := s.PutMeta(context.Background(), meta)
	require.NoError(t, err)
	t.Log("create meta in store", meta.String())
	return meta
}

func createMetaEntity(
	tenant string,
	tableName string,
	minFingerprint uint64,
	maxFingerprint uint64,
	startTimestamp model.Time,
	endTimestamp model.Time,
) Meta {
	return Meta{
		MetaRef: MetaRef{
			Ref: Ref{
				TenantID:       tenant,
				TableName:      tableName,
				Bounds:         v1.NewBounds(model.Fingerprint(minFingerprint), model.Fingerprint(maxFingerprint)),
				StartTimestamp: startTimestamp,
				EndTimestamp:   endTimestamp,
			},
		},
		Tombstones: []BlockRef{
			{
				Ref: Ref{
					TenantID:       tenant,
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
					Bounds:         v1.NewBounds(model.Fingerprint(minFingerprint), model.Fingerprint(maxFingerprint)),
					StartTimestamp: startTimestamp,
					EndTimestamp:   endTimestamp,
				},
			},
		},
	}
}
