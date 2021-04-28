package retention

import (
	"context"
	"io/ioutil"
	"path/filepath"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	cortex_storage "github.com/cortexproject/cortex/pkg/chunk/storage"
	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
	ww "github.com/weaveworks/common/server"
	"github.com/weaveworks/common/user"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/stores/shipper"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
	"github.com/grafana/loki/pkg/validation"
)

func dayFromTime(t model.Time) chunk.DayTime {
	parsed, err := time.Parse("2006-01-02", t.Time().Format("2006-01-02"))
	if err != nil {
		panic(err)
	}
	return chunk.DayTime{
		Time: model.TimeFromUnix(parsed.Unix()),
	}
}

var (
	start     = model.Now().Add(-30 * 24 * time.Hour)
	schemaCfg = storage.SchemaConfig{
		SchemaConfig: chunk.SchemaConfig{
			// we want to test over all supported schema.
			Configs: []chunk.PeriodConfig{
				{
					From:       dayFromTime(start),
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
					From:       dayFromTime(start.Add(25 * time.Hour)),
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
					From:       dayFromTime(start.Add(49 * time.Hour)),
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
	allSchemas = []struct {
		schema string
		from   model.Time
		config chunk.PeriodConfig
	}{
		{"v9", schemaCfg.Configs[0].From.Time, schemaCfg.Configs[0]},
		{"v10", schemaCfg.Configs[1].From.Time, schemaCfg.Configs[1]},
		{"v11", schemaCfg.Configs[2].From.Time, schemaCfg.Configs[2]},
	}

	sweepMetrics = newSweeperMetrics(prometheus.DefaultRegisterer)
)

func newChunkEntry(userID, labels string, from, through model.Time) ChunkEntry {
	lbs, err := logql.ParseLabels(labels)
	if err != nil {
		panic(err)
	}
	return ChunkEntry{
		ChunkRef: ChunkRef{
			UserID:   []byte(userID),
			SeriesID: labelsSeriesID(lbs),
			From:     from,
			Through:  through,
		},
		Labels: lbs,
	}
}

type testStore struct {
	storage.Store
	cfg                storage.Config
	objectClient       chunk.ObjectClient
	indexDir, chunkDir string
	schemaCfg          storage.SchemaConfig
	t                  testing.TB
	limits             cortex_storage.StoreLimits
}

// testObjectClient is a testing object client
type testObjectClient struct {
	chunk.ObjectClient
	path string
}

func (t testObjectClient) DeleteObject(ctx context.Context, objectKey string) error {
	_ = t.ObjectClient.DeleteObject(ctx, objectKey) // locally we don't have a table folder so we swallow the error.
	return nil
}

func newTestObjectClient(path string) chunk.ObjectClient {
	c, err := cortex_storage.NewObjectClient("filesystem", cortex_storage.Config{
		FSConfig: local.FSConfig{
			Directory: path,
		},
	})
	if err != nil {
		panic(err)
	}
	return &testObjectClient{
		ObjectClient: c,
		path:         path,
	}
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

func (t *testStore) HasChunk(c chunk.Chunk) bool {
	t.t.Helper()
	var matchers []*labels.Matcher
	for _, l := range c.Metric {
		matchers = append(matchers, labels.MustNewMatcher(labels.MatchEqual, l.Name, l.Value))
	}
	chunks, err := t.Store.Get(user.InjectOrgID(context.Background(), c.UserID),
		c.UserID, c.From, c.Through, matchers...)
	require.NoError(t.t, err)
	return len(chunks) == 1 && c.ExternalKey() == chunks[0].ExternalKey()
}

func (t *testStore) open() {
	chunkStore, err := cortex_storage.NewStore(
		t.cfg.Config,
		chunk.StoreConfig{},
		schemaCfg.SchemaConfig,
		t.limits,
		nil,
		nil,
		util_log.Logger,
	)
	require.NoError(t.t, err)

	store, err := storage.NewStore(t.cfg, schemaCfg, chunkStore, nil)
	require.NoError(t.t, err)
	t.Store = store
}

func newTestStore(t testing.TB) *testStore {
	t.Helper()
	cfg := &ww.Config{}
	require.Nil(t, cfg.LogLevel.Set("debug"))
	util_log.InitLogger(cfg)
	workdir := t.TempDir()
	filepath.Join(workdir, "index")
	indexDir := filepath.Join(workdir, "index")
	err := chunk_util.EnsureDirectory(indexDir)
	require.Nil(t, err)

	chunkDir := filepath.Join(workdir, "chunk_test")
	err = chunk_util.EnsureDirectory(indexDir)
	require.Nil(t, err)
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
		BoltDBShipperConfig: shipper.Config{
			ActiveIndexDirectory: indexDir,
			SharedStoreType:      "filesystem",
			SharedStoreKeyPrefix: "index",
			ResyncInterval:       1 * time.Millisecond,
			IngesterName:         "foo",
			Mode:                 shipper.ModeReadWrite,
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
		indexDir:     indexDir,
		chunkDir:     chunkDir,
		t:            t,
		Store:        store,
		schemaCfg:    schemaCfg,
		objectClient: newTestObjectClient(workdir),
		cfg:          config,
		limits:       limits,
	}
}
