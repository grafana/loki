package retention

import (
	"context"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	ww "github.com/weaveworks/common/server"
	"github.com/weaveworks/common/user"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/local"
	chunk_storage "github.com/grafana/loki/pkg/storage/chunk/storage"
	chunk_util "github.com/grafana/loki/pkg/storage/chunk/util"
	"github.com/grafana/loki/pkg/storage/stores/shipper"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/validation"
)

func dayFromTime(t model.Time) chunk.DayTime {
	parsed, err := time.Parse("2006-01-02", t.Time().In(time.UTC).Format("2006-01-02"))
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
					From:       dayFromTime(start.Add(73 * time.Hour)),
					IndexType:  "boltdb",
					ObjectType: "filesystem",
					Schema:     "v11",
					IndexTables: chunk.PeriodicTableConfig{
						Prefix: "index_",
						Period: time.Hour * 24,
					},
					RowShards: 16,
				},
				{
					From:       dayFromTime(start.Add(100 * time.Hour)),
					IndexType:  "boltdb",
					ObjectType: "filesystem",
					Schema:     "v12",
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
		{"v12", schemaCfg.Configs[3].From.Time, schemaCfg.Configs[3]},
	}

	sweepMetrics = newSweeperMetrics(prometheus.DefaultRegisterer)
)

func newChunkEntry(userID, labels string, from, through model.Time) ChunkEntry {
	lbs, err := syntax.ParseLabels(labels)
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
	limits             chunk_storage.StoreLimits
	clientMetrics      chunk_storage.ClientMetrics
}

// testObjectClient is a testing object client
type testObjectClient struct {
	chunk.ObjectClient
	path string
}

func newTestObjectClient(path string, clientMetrics chunk_storage.ClientMetrics) chunk.ObjectClient {
	c, err := chunk_storage.NewObjectClient("filesystem", chunk_storage.Config{
		FSConfig: local.FSConfig{
			Directory: path,
		},
	}, clientMetrics)
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
	chunks := t.GetChunks(c.UserID, c.From, c.Through, c.Metric)

	chunkIDs := make(map[string]struct{})
	for _, chk := range chunks {
		chunkIDs[t.schemaCfg.ExternalKey(chk)] = struct{}{}
	}
	return len(chunkIDs) == 1 && t.schemaCfg.ExternalKey(c) == t.schemaCfg.ExternalKey(chunks[0])
}

func (t *testStore) GetChunks(userID string, from, through model.Time, metric labels.Labels) []chunk.Chunk {
	t.t.Helper()
	var matchers []*labels.Matcher
	for _, l := range metric {
		matchers = append(matchers, labels.MustNewMatcher(labels.MatchEqual, l.Name, l.Value))
	}
	ctx := user.InjectOrgID(context.Background(), userID)
	chunks, fetchers, err := t.Store.GetChunkRefs(ctx, userID, from, through, matchers...)
	require.NoError(t.t, err)
	fetchedChunk := []chunk.Chunk{}
	for _, f := range fetchers {
		for _, cs := range chunks {
			keys := make([]string, 0, len(cs))
			sort.Slice(chunks, func(i, j int) bool { return schemaCfg.ExternalKey(cs[i]) < schemaCfg.ExternalKey(cs[j]) })

			for _, c := range cs {
				keys = append(keys, schemaCfg.ExternalKey(c))
			}
			cks, err := f.FetchChunks(ctx, cs, keys)
			if err != nil {
				t.t.Fatal(err)
			}
		outer:
			for _, c := range cks {
				for _, matcher := range matchers {
					if !matcher.Matches(c.Metric.Get(matcher.Name)) {
						continue outer
					}
				}
				fetchedChunk = append(fetchedChunk, c)
			}

		}
	}
	return fetchedChunk
}

func (t *testStore) open() {
	chunkStore, err := chunk_storage.NewStore(
		t.cfg.Config,
		chunk.StoreConfig{},
		schemaCfg.SchemaConfig,
		t.limits,
		t.clientMetrics,
		nil,
		nil,
		util_log.Logger,
	)
	require.NoError(t.t, err)

	store, err := storage.NewStore(t.cfg, schemaCfg, chunkStore, nil)
	require.NoError(t.t, err)
	t.Store = store
}

func newTestStore(t testing.TB, clientMetrics chunk_storage.ClientMetrics) *testStore {
	t.Helper()
	cfg := &ww.Config{}
	require.Nil(t, cfg.LogLevel.Set("debug"))
	util_log.InitLogger(cfg, nil)
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
		Config: chunk_storage.Config{
			BoltDBConfig: local.BoltDBConfig{
				Directory: indexDir,
			},
			FSConfig: local.FSConfig{
				Directory: chunkDir,
			},
			MaxParallelGetChunk: 150,
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
	chunkStore, err := chunk_storage.NewStore(
		config.Config,
		chunk.StoreConfig{},
		schemaCfg.SchemaConfig,
		limits,
		clientMetrics,
		nil,
		nil,
		util_log.Logger,
	)
	require.NoError(t, err)

	store, err := storage.NewStore(config, schemaCfg, chunkStore, nil)
	require.NoError(t, err)
	return &testStore{
		indexDir:      indexDir,
		chunkDir:      chunkDir,
		t:             t,
		Store:         store,
		schemaCfg:     schemaCfg,
		objectClient:  newTestObjectClient(workdir, clientMetrics),
		cfg:           config,
		limits:        limits,
		clientMetrics: clientMetrics,
	}
}

func TestExtractIntervalFromTableName(t *testing.T) {
	periodicTableConfig := chunk.PeriodicTableConfig{
		Prefix: "dummy",
		Period: 24 * time.Hour,
	}

	const millisecondsInDay = model.Time(24 * time.Hour / time.Millisecond)

	calculateInterval := func(tm model.Time) (m model.Interval) {
		m.Start = tm - tm%millisecondsInDay
		m.End = m.Start + millisecondsInDay - 1
		return
	}

	for i, tc := range []struct {
		tableName        string
		expectedInterval model.Interval
	}{
		{
			tableName:        periodicTableConfig.TableFor(model.Now()),
			expectedInterval: calculateInterval(model.Now()),
		},
		{
			tableName:        periodicTableConfig.TableFor(model.Now().Add(-24 * time.Hour)),
			expectedInterval: calculateInterval(model.Now().Add(-24 * time.Hour)),
		},
		{
			tableName:        periodicTableConfig.TableFor(model.Now().Add(-24 * time.Hour).Add(time.Minute)),
			expectedInterval: calculateInterval(model.Now().Add(-24 * time.Hour).Add(time.Minute)),
		},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			require.Equal(t, tc.expectedInterval, ExtractIntervalFromTableName(tc.tableName))
		})
	}
}
