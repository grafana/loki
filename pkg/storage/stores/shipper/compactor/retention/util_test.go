package retention

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	cortex_storage "github.com/cortexproject/cortex/pkg/chunk/storage"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/storage"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
	"github.com/grafana/loki/pkg/util/validation"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"
)

var schemaCfg = storage.SchemaConfig{
	SchemaConfig: chunk.SchemaConfig{
		// we want to test over all supported schema.
		Configs: []chunk.PeriodConfig{
			{
				From:       chunk.DayTime{Time: model.Earliest},
				IndexType:  "boltdb",
				ObjectType: "filesystem",
				Schema:     "v9",
				IndexTables: chunk.PeriodicTableConfig{
					Prefix: "index_",
					Period: time.Hour * 24,
				},
				RowShards: 16,
			},
			{
				From:       chunk.DayTime{Time: model.Earliest.Add(25 * time.Hour)},
				IndexType:  "boltdb",
				ObjectType: "filesystem",
				Schema:     "v10",
				IndexTables: chunk.PeriodicTableConfig{
					Prefix: "index_",
					Period: time.Hour * 24,
				},
				RowShards: 16,
			},
			{
				From:       chunk.DayTime{Time: model.Earliest.Add(49 * time.Hour)},
				IndexType:  "boltdb",
				ObjectType: "filesystem",
				Schema:     "v11",
				IndexTables: chunk.PeriodicTableConfig{
					Prefix: "index_",
					Period: time.Hour * 24,
				},
				RowShards: 16,
			},
		},
	},
}

var allSchemas = []struct {
	schema string
	from   model.Time
}{
	{"v9", model.Earliest},
	{"v10", model.Earliest.Add(25 * time.Hour)},
	{"v11", model.Earliest.Add(49 * time.Hour)},
}

type fakeRule struct {
	streams []StreamRule
	tenants map[string]time.Duration
}

func (f fakeRule) PerTenant(userID string) time.Duration {
	return f.tenants[userID]
}

func (f fakeRule) PerStream() []StreamRule {
	return f.streams
}

func newChunkRef(userID, labels string, from, through model.Time) ChunkRef {
	lbs, err := logql.ParseLabels(labels)
	if err != nil {
		panic(err)
	}
	return ChunkRef{
		UserID:   []byte(userID),
		SeriesID: labelsSeriesID(lbs),
		From:     from,
		Through:  through,
	}
}

type testStore struct {
	storage.Store
	indexDir, chunkDir string
	schemaCfg          storage.SchemaConfig
	t                  *testing.T
}

func (t *testStore) cleanup() {
	t.t.Helper()
	require.NoError(t.t, os.RemoveAll(t.indexDir))
	require.NoError(t.t, os.RemoveAll(t.indexDir))
}

type table struct {
	name string
	*bbolt.DB
}

func (t *testStore) indexTables() []table {
	t.t.Helper()
	res := []table{}
	indexFilesInfo, err := ioutil.ReadDir(t.indexDir)
	require.NoError(t.t, err)
	for _, indexFileInfo := range indexFilesInfo {
		db, err := shipper_util.SafeOpenBoltdbFile(filepath.Join(t.indexDir, indexFileInfo.Name()))
		require.NoError(t.t, err)
		res = append(res, table{name: indexFileInfo.Name(), DB: db})
	}
	return res
}

func newTestStore(t *testing.T) *testStore {
	t.Helper()
	indexDir, err := ioutil.TempDir("", "boltdb_test")
	require.Nil(t, err)

	chunkDir, err := ioutil.TempDir("", "chunk_test")
	require.Nil(t, err)

	defer func() {
	}()
	limits, err := validation.NewOverrides(validation.Limits{}, nil)
	require.NoError(t, err)

	require.NoError(t, schemaCfg.SchemaConfig.Validate())

	config := storage.Config{
		Config: cortex_storage.Config{
			BoltDBConfig: local.BoltDBConfig{
				Directory: indexDir,
			},
			FSConfig: local.FSConfig{
				Directory: chunkDir,
			},
		},
	}
	chunkStore, err := cortex_storage.NewStore(
		config.Config,
		chunk.StoreConfig{},
		schemaCfg.SchemaConfig,
		limits,
		nil,
		nil,
		util_log.Logger,
	)
	require.NoError(t, err)

	store, err := storage.NewStore(config, schemaCfg, chunkStore, nil)
	require.NoError(t, err)
	return &testStore{
		indexDir:  indexDir,
		chunkDir:  chunkDir,
		t:         t,
		Store:     store,
		schemaCfg: schemaCfg,
	}
}
