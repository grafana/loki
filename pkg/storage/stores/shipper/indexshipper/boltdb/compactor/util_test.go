package compactor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	ww "github.com/grafana/dskit/server"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	chunk_util "github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/boltdb"
	shipper_util "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/validation"
)

func dayFromTime(t model.Time) config.DayTime {
	parsed, err := time.Parse("2006-01-02", t.Time().In(time.UTC).Format("2006-01-02"))
	if err != nil {
		panic(err)
	}
	return config.DayTime{
		Time: model.TimeFromUnix(parsed.Unix()),
	}
}

var (
	start     = model.Now().Add(-30 * 24 * time.Hour)
	schemaCfg = config.SchemaConfig{
		// we want to test over all supported schema.
		Configs: []config.PeriodConfig{
			{
				From:       dayFromTime(start),
				IndexType:  "boltdb",
				ObjectType: "filesystem",
				Schema:     "v9",
				IndexTables: config.IndexPeriodicTableConfig{
					PathPrefix: "index/",
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: "index_",
						Period: time.Hour * 24,
					}},
				RowShards: 16,
			},
			{
				From:       dayFromTime(start.Add(25 * time.Hour)),
				IndexType:  "boltdb",
				ObjectType: "filesystem",
				Schema:     "v10",
				IndexTables: config.IndexPeriodicTableConfig{
					PathPrefix: "index/",
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: "index_",
						Period: time.Hour * 24,
					}},
				RowShards: 16,
			},
			{
				From:       dayFromTime(start.Add(73 * time.Hour)),
				IndexType:  "boltdb",
				ObjectType: "filesystem",
				Schema:     "v11",
				IndexTables: config.IndexPeriodicTableConfig{
					PathPrefix: "index/",
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: "index_",
						Period: time.Hour * 24,
					}},
				RowShards: 16,
			},
			{
				From:       dayFromTime(start.Add(100 * time.Hour)),
				IndexType:  "boltdb",
				ObjectType: "filesystem",
				Schema:     "v12",
				IndexTables: config.IndexPeriodicTableConfig{
					PathPrefix: "index/",
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: "index_",
						Period: time.Hour * 24,
					}},
				RowShards: 16,
			},
		},
	}
	allSchemas = []struct {
		schema string
		from   model.Time
		config config.PeriodConfig
	}{
		{"v9", schemaCfg.Configs[0].From.Time, schemaCfg.Configs[0]},
		{"v10", schemaCfg.Configs[1].From.Time, schemaCfg.Configs[1]},
		{"v11", schemaCfg.Configs[2].From.Time, schemaCfg.Configs[2]},
		{"v12", schemaCfg.Configs[3].From.Time, schemaCfg.Configs[3]},
	}
)

type testStore struct {
	storage.Store
	cfg                storage.Config
	objectClient       client.ObjectClient
	indexDir, chunkDir string
	schemaCfg          config.SchemaConfig
	t                  testing.TB
	limits             storage.StoreLimits
	clientMetrics      storage.ClientMetrics
}

// testObjectClient is a testing object client
type testObjectClient struct {
	client.ObjectClient
	path string
}

func newTestObjectClient(path string, clientMetrics storage.ClientMetrics) client.ObjectClient {
	c, err := storage.NewObjectClient("filesystem", storage.Config{
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
	dirEntries, err := os.ReadDir(t.indexDir)
	require.NoError(t.t, err)
	for _, entry := range dirEntries {
		db, err := shipper_util.SafeOpenBoltdbFile(filepath.Join(t.indexDir, entry.Name()))
		require.NoError(t.t, err)
		res = append(res, table{name: entry.Name(), DB: db})
	}
	return res
}

func (t *testStore) HasChunk(c chunk.Chunk) bool {
	chunks := t.GetChunks(c.UserID, c.From, c.Through, c.Metric)

	chunkIDs := make(map[string]struct{})
	for _, chk := range chunks {
		chunkIDs[t.schemaCfg.ExternalKey(chk.ChunkRef)] = struct{}{}
	}
	return len(chunkIDs) == 1 && t.schemaCfg.ExternalKey(c.ChunkRef) == t.schemaCfg.ExternalKey(chunks[0].ChunkRef)
}

func (t *testStore) GetChunks(userID string, from, through model.Time, metric labels.Labels) []chunk.Chunk {
	t.t.Helper()
	var matchers []*labels.Matcher
	for _, l := range metric {
		matchers = append(matchers, labels.MustNewMatcher(labels.MatchEqual, l.Name, l.Value))
	}
	ctx := user.InjectOrgID(context.Background(), userID)
	chunks, fetchers, err := t.Store.GetChunks(ctx, userID, from, through, chunk.NewPredicate(matchers, nil), nil)
	require.NoError(t.t, err)
	fetchedChunk := []chunk.Chunk{}
	for _, f := range fetchers {
		for _, cs := range chunks {
			cks, err := f.FetchChunks(ctx, cs)
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

func newTestStore(t testing.TB, clientMetrics storage.ClientMetrics) *testStore {
	t.Helper()
	servercfg := &ww.Config{}
	require.Nil(t, servercfg.LogLevel.Set("debug"))
	util_log.InitLogger(servercfg, nil, false)
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

	require.NoError(t, schemaCfg.Validate())

	cfg := storage.Config{
		BoltDBConfig: local.BoltDBConfig{
			Directory: indexDir,
		},
		FSConfig: local.FSConfig{
			Directory: chunkDir,
		},
		MaxParallelGetChunk: 150,

		BoltDBShipperConfig: boltdb.IndexCfg{
			Config: indexshipper.Config{
				ActiveIndexDirectory: indexDir,
				ResyncInterval:       1 * time.Millisecond,
				IngesterName:         "foo",
				Mode:                 indexshipper.ModeReadWrite,
			},
		},
	}

	store, err := storage.NewStore(cfg, config.ChunkStoreConfig{}, schemaCfg, limits, clientMetrics, nil, util_log.Logger, constants.Loki)
	require.NoError(t, err)
	return &testStore{
		indexDir:      indexDir,
		chunkDir:      chunkDir,
		t:             t,
		Store:         store,
		schemaCfg:     schemaCfg,
		objectClient:  newTestObjectClient(workdir, clientMetrics),
		cfg:           cfg,
		limits:        limits,
		clientMetrics: clientMetrics,
	}
}

func TestExtractIntervalFromTableName(t *testing.T) {
	periodicTableConfig := config.PeriodicTableConfig{
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
