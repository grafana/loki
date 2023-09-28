package bloom_shipper

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	day = 24 * time.Hour
)

var (
	// table 19627
	fixedDay = model.TimeFromUnix(time.Date(2023, time.September, 27, 0, 0, 0, 0, time.UTC).Unix())
)

func Test_BloomShipper_List(t *testing.T) {
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

	actual, err := shipper.GetAll(context.Background(), MetaSearchParams{
		TenantID:       "tenantA",
		MinFingerprint: 50,
		MaxFingerprint: 150,
		StartTimestamp: fixedDay.Add(-6 * day).Unix(),
		EndTimestamp:   fixedDay.Add(-1*day - 1*time.Hour).Unix(),
	})
	require.NoError(t, err)
	require.ElementsMatch(t, expected, actual)
}

func Test_BloomShipper_Upload(t *testing.T) {
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
				"31e0a0000000e58de8",
				"ignored-file-path-during-uploading",
			),
			expectedStorage:  "folder-1",
			expectedFilePath: "first-period-19621/tenantA/metas/ff-fff-1695272400-1695276000-31e0a0000000e58de8",
		},
		"expected meta to be uploaded to the second folder": {
			source: createMetaEntity("tenantA",
				"second-period-19625",
				200,
				300,
				time.Date(2023, time.September, 25, 0, 0, 0, 0, time.UTC).Unix(),
				time.Date(2023, time.September, 25, 1, 0, 0, 0, time.UTC).Unix(),
				"31e0a973c2e58de8",
				"ignored-file-path-during-uploading",
			),
			expectedStorage:  "folder-2",
			expectedFilePath: "second-period-19625/tenantA/metas/c8-12c-1695600000-1695603600-31e0a973c2e58de8",
		},
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			shipper := createShipper(t)

			err := shipper.Upload(context.Background(), data.source)
			require.NoError(t, err)

			directory := shipper.storageConfig.NamedStores.Filesystem[data.expectedStorage].Directory
			filePath := filepath.Join(directory, createObjectKey(data.source.MetaRef))
			fmt.Println(createObjectKey(data.source.MetaRef))
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

func Test_BloomShipper_Delete(t *testing.T) {
	tests := map[string]struct {
		source           Meta
		expectedFilePath string
		expectedStorage  string
	}{
		"expected meta to be deleted to the first folder": {
			source: createMetaEntity("tenantA",
				"first-period-19621",
				0xff,
				0xfff,
				time.Date(2023, time.September, 21, 5, 0, 0, 0, time.UTC).Unix(),
				time.Date(2023, time.September, 21, 6, 0, 0, 0, time.UTC).Unix(),
				"31e0a0000000e58de8",
				"ignored-file-path-during-uploading",
			),
			expectedStorage:  "folder-1",
			expectedFilePath: "first-period-19621/tenantA/metas/ff-fff-1695272400-1695276000-31e0a0000000e58de8",
		},
		"expected meta to be delete to the second folder": {
			source: createMetaEntity("tenantA",
				"second-period-19625",
				200,
				300,
				time.Date(2023, time.September, 25, 0, 0, 0, 0, time.UTC).Unix(),
				time.Date(2023, time.September, 25, 1, 0, 0, 0, time.UTC).Unix(),
				"31e0a973c2e58de8",
				"ignored-file-path-during-uploading",
			),
			expectedStorage:  "folder-2",
			expectedFilePath: "second-period-19625/tenantA/metas/c8-12c-1695600000-1695603600-31e0a973c2e58de8",
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

			err = shipper.Delete(context.Background(), data.source)
			require.NoError(t, err)

			require.NoFileExists(t, file)
		})
	}

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

func createShipper(t *testing.T) *bloomShipper {
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
	bshipper, err := NewShipper(periodicConfigs, storageConfig, metrics)
	require.NoError(t, err)
	require.NotNil(t, bshipper)
	require.IsType(t, &bloomShipper{}, bshipper)
	return bshipper.(*bloomShipper)
}

func createPeriodConfigs() []config.PeriodConfig {
	periodicConfigs := []config.PeriodConfig{
		{
			ObjectType: "folder-1",
			// from 2023-09-20: table range [19620:19623]
			From: config.DayTime{Time: fixedDay.Add(-7 * 24 * time.Hour)},
			IndexTables: config.PeriodicTableConfig{
				Period: day,
				Prefix: "first-period-",
			},
		},
		{
			ObjectType: "folder-2",
			// from 2023-09-24: table range [19624:19627]
			From: config.DayTime{Time: fixedDay.Add(-3 * 24 * time.Hour)},
			IndexTables: config.PeriodicTableConfig{
				Period: day,
				Prefix: "second-period-",
			},
		},
	}
	return periodicConfigs
}

func createMetaInStorage(t *testing.T, rootFolder string, tableName string, tenant string, minFingerprint uint64, maxFingerprint uint64, start model.Time) Meta {
	startTimestamp := start.Unix()
	endTimestamp := start.Add(12 * time.Hour).Unix()

	metaChecksum := fmt.Sprintf("%x", rand.Int63())
	metaFileName := fmt.Sprintf("%x-%x-%v-%v-%s", minFingerprint, maxFingerprint, startTimestamp, endTimestamp, metaChecksum)
	metaFolder := filepath.Join(tableName, tenant, "metas")
	err := os.MkdirAll(filepath.Join(rootFolder, metaFolder), 0700)
	require.NoError(t, err)
	metaFilePath := filepath.Join(metaFolder, metaFileName)
	meta := createMetaEntity(tenant, tableName, minFingerprint, maxFingerprint, startTimestamp, endTimestamp, metaChecksum, metaFilePath)

	bytes, err := json.Marshal(meta)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(rootFolder, metaFilePath), bytes, 0644)
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
	metaChecksum string,
	metaFilePath string) Meta {
	return Meta{
		MetaRef: MetaRef{
			TenantID:       tenant,
			TableName:      tableName,
			MinFingerprint: minFingerprint,
			MaxFingerprint: maxFingerprint,
			StartTimestamp: startTimestamp,
			EndTimestamp:   endTimestamp,
			Checksum:       metaChecksum,
			FilePath:       metaFilePath,
		},
		Tombstones: []BlockRef{
			{
				TenantID:       tenant,
				TSDBSource:     uuid.New().String(),
				BlockFilePath:  uuid.New().String(),
				Checksum:       fmt.Sprintf("%x", rand.Int63()),
				MinFingerprint: minFingerprint,
				MaxFingerprint: maxFingerprint,
				StartTimestamp: startTimestamp,
				EndTimestamp:   endTimestamp,
			},
		},
		Blocks: []BlockRef{{
			TenantID:       tenant,
			TSDBSource:     uuid.New().String(),
			BlockFilePath:  uuid.New().String(),
			Checksum:       fmt.Sprintf("%x", rand.Int63()),
			MinFingerprint: minFingerprint,
			MaxFingerprint: maxFingerprint,
			StartTimestamp: startTimestamp,
			EndTimestamp:   endTimestamp,
		}},
	}
}
