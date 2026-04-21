package compactor

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/testutil"
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
				IndexType:  "boltdb-shipper",
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
				IndexType:  "boltdb-shipper",
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
				IndexType:  "boltdb-shipper",
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
				IndexType:  "boltdb-shipper",
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
	c, err := storage.NewObjectClient("filesystem", "test", storage.Config{
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

	// This change is temporary and only required while boltdb storage has been removed,
	// but boltdb-shipper is still available, and this test was updated to work with boltdb-shipper.
	// boltdb-shipper uploads compressed (gzip) index files to the object store
	// under <chunkDir>/index/<tableName>/<uploader>-<dbName>.gz. After
	// store.Stop() the local active boltdb files may have been removed, so we
	// read the uploaded index files, decompress them into a temp location and
	// open them as boltdb databases.
	decompressDir := t.t.TempDir()
	err := filepath.Walk(filepath.Join(t.chunkDir, "index"), func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if info == nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".gz") {
			return nil
		}
		// The table name is the parent directory name, e.g. "index_19770".
		tableName := filepath.Base(filepath.Dir(path))
		decompressed := filepath.Join(decompressDir, strings.TrimSuffix(info.Name(), ".gz"))
		gt, ok := t.t.(*testing.T)
		require.True(t.t, ok, "indexTables requires *testing.T")
		testutil.DecompressFile(gt, path, decompressed)
		db, err := shipper_util.SafeOpenBoltdbFile(decompressed)
		require.NoError(t.t, err)
		res = append(res, table{name: tableName, DB: db})
		return nil
	})
	require.NoError(t.t, err)

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
	metric.Range(func(l labels.Label) {
		matchers = append(matchers, labels.MustNewMatcher(labels.MatchEqual, l.Name, l.Value))
	})
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
	// Reset the boltdb-shipper singleton so each test gets a client bound
	// to its own tempDir rather than reusing one from a previous test.
	storage.ResetBoltDBIndexClientsWithShipper()
	servercfg := &ww.Config{}
	require.Nil(t, servercfg.LogLevel.Set("debug"))
	util_log.InitLogger(servercfg, nil, false)
	workdir := t.TempDir()
	indexDir := filepath.Join(workdir, "index")
	err := chunk_util.EnsureDirectory(indexDir)
	require.Nil(t, err)

	objectStoreDir := filepath.Join(workdir, "objectstorage")
	err = chunk_util.EnsureDirectory(objectStoreDir)
	require.Nil(t, err)

	cacheDir := filepath.Join(workdir, "cache")
	err = chunk_util.EnsureDirectory(cacheDir)
	require.Nil(t, err)

	limits, err := validation.NewOverrides(validation.Limits{}, nil)
	require.NoError(t, err)

	require.NoError(t, schemaCfg.Validate())

	cfg := storage.Config{
		FSConfig: local.FSConfig{
			Directory: objectStoreDir,
		},
		MaxParallelGetChunk: 150,

		BoltDBShipperConfig: boltdb.IndexCfg{
			Config: indexshipper.Config{
				ActiveIndexDirectory: indexDir,
				CacheLocation:        cacheDir,
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
		chunkDir:      objectStoreDir,
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
