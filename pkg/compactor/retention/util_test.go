package retention

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"testing"
	"time"

	ww "github.com/grafana/dskit/server"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	chunk_util "github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/config"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
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
	start = model.Now().Add(-30 * 24 * time.Hour)
	// ToDo(Sandeep): See if we can get rid of schemaCfg now that we mock the index store.
	schemaCfg = config.SchemaConfig{
		// we want to test over all supported schema.
		Configs: []config.PeriodConfig{
			{
				From:       dayFromTime(start),
				IndexType:  "boltdb",
				ObjectType: "filesystem",
				Schema:     "v9",
				IndexTables: config.IndexPeriodicTableConfig{
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
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: "index_",
						Period: time.Hour * 24,
					}},
				RowShards: 16,
			},
			{
				From:       dayFromTime(start.Add(125 * time.Hour)),
				IndexType:  "tsdb",
				ObjectType: "filesystem",
				Schema:     "v13",
				IndexTables: config.IndexPeriodicTableConfig{
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
		{"v13", schemaCfg.Configs[4].From.Time, schemaCfg.Configs[4]},
	}

	sweepMetrics = newSweeperMetrics(prometheus.DefaultRegisterer)
)

func mustParseLabels(labels string) labels.Labels {
	lbs, err := syntax.ParseLabels(labels)
	if err != nil {
		panic(err)
	}

	return lbs
}

type table struct {
	name   string
	chunks map[string]map[string][]logproto.ChunkRef
}

func (t *table) ForEachSeries(ctx context.Context, callback SeriesCallback) error {
	for userID := range t.chunks {
		for seriesID := range t.chunks[userID] {
			chunks := make([]Chunk, 0, len(t.chunks[userID][seriesID]))
			for _, chk := range t.chunks[userID][seriesID] {
				chunks = append(chunks, Chunk{
					ChunkID: getChunkID(chk),
					From:    chk.From,
					Through: chk.Through,
				})
			}
			series := series{}
			lbls, err := syntax.ParseLabels(seriesID)
			if err != nil {
				return err
			}
			series.Reset(
				[]byte(seriesID),
				[]byte(userID),
				labels.NewBuilder(lbls).Del(labels.MetricName).Labels(),
			)
			series.AppendChunks(chunks...)
			if err := callback(&series); err != nil {
				return err
			}
		}
	}

	return ctx.Err()
}

func (t *table) IndexChunk(chunkRef logproto.ChunkRef, lbls labels.Labels, _ uint32, _ uint32) (bool, error) {
	seriesID := lbls.String()
	t.chunks[chunkRef.UserID][seriesID] = append(t.chunks[chunkRef.UserID][seriesID], chunkRef)
	return true, nil
}

func (t *table) CleanupSeries(_ []byte, _ labels.Labels) error {
	return nil
}

func (t *table) RemoveChunk(_, _ model.Time, userID []byte, lbls labels.Labels, chunkID string) error {
	seriesID := labels.NewBuilder(lbls).Set(labels.MetricName, "logs").Labels().String()
	for i, chk := range t.chunks[string(userID)][seriesID] {
		if getChunkID(chk) == chunkID {
			t.chunks[string(userID)][seriesID] = append(t.chunks[string(userID)][seriesID][:i], t.chunks[string(userID)][seriesID][i+1:]...)
		}
	}

	return nil
}

func newTable(name string) *table {
	return &table{
		name:   name,
		chunks: map[string]map[string][]logproto.ChunkRef{},
	}
}

func (t *table) Put(chk chunk.Chunk) {
	if _, ok := t.chunks[chk.UserID]; !ok {
		t.chunks[chk.UserID] = make(map[string][]logproto.ChunkRef)
	}
	seriesID := chk.Metric.String()
	if _, ok := t.chunks[chk.UserID][seriesID]; !ok {
		t.chunks[chk.UserID][seriesID] = []logproto.ChunkRef{}
	}

	t.chunks[chk.UserID][seriesID] = append(t.chunks[chk.UserID][seriesID], chk.ChunkRef)
}

func (t *table) GetChunks(userID string, from, through model.Time, metric labels.Labels) ([]logproto.ChunkRef, error) {
	var chunks []logproto.ChunkRef
	var matchers []*labels.Matcher
	metric.Range(func(l labels.Label) {
		matchers = append(matchers, labels.MustNewMatcher(labels.MatchEqual, l.Name, l.Value))
	})

	for seriesID := range t.chunks[userID] {
		lbls, err := syntax.ParseLabels(seriesID)
		if err != nil {
			return nil, err
		}
		for _, chk := range t.chunks[userID][seriesID] {
			if chk.From > through || chk.Through < from || !allMatch(matchers, lbls) {
				continue
			}
			chunks = append(chunks, chk)
		}
	}

	return chunks, nil
}

func allMatch(matchers []*labels.Matcher, labels labels.Labels) bool {
	for _, m := range matchers {
		if !m.Matches(labels.Get(m.Name)) {
			return false
		}
	}
	return true
}

func tablesInInterval(from, through model.Time) (res []string) {
	start := from.Time().UnixNano() / int64(config.ObjectStorageIndexRequiredPeriod)
	end := through.Time().UnixNano() / int64(config.ObjectStorageIndexRequiredPeriod)
	for cur := start; cur <= end; cur++ {
		res = append(res, fmt.Sprintf("index_%d", cur))
	}
	return
}

type testStore struct {
	chunkClient  client.Client
	objectClient client.ObjectClient
	t            testing.TB
	tables       map[string]*table
}

func (t *testStore) Put(ctx context.Context, chunks []chunk.Chunk) error {
	for _, chk := range chunks {
		for _, tableName := range tablesInInterval(chk.From, chk.Through) {
			if _, ok := t.tables[tableName]; !ok {
				t.tables[tableName] = newTable(tableName)
			}

			t.tables[tableName].Put(chk)
		}
	}

	return t.chunkClient.PutChunks(ctx, chunks)
}

func (t *testStore) Stop() {}

// testObjectClient is a testing object client
type testObjectClient struct {
	client.ObjectClient
	path string
}

func newTestObjectClient(path string) client.ObjectClient {
	c, err := local.NewFSObjectClient(local.FSConfig{
		Directory: path,
	})
	if err != nil {
		panic(err)
	}
	return &testObjectClient{
		ObjectClient: c,
		path:         path,
	}
}

func (t *testStore) indexTables() []*table {
	t.t.Helper()
	res := []*table{}

	for _, table := range t.tables {
		res = append(res, table)
	}

	sort.Slice(res, func(i, j int) bool {
		return res[i].name < res[j].name
	})
	return res
}

func (t *testStore) HasChunk(c chunk.Chunk) bool {
	chunks := t.GetChunks(c.UserID, c.From, c.Through, c.Metric)

	for _, chk := range chunks {
		if chk.ChunkRef != c.ChunkRef {
			return false
		}
	}
	return len(chunks) > 0
}

func (t *testStore) GetChunks(userID string, from, through model.Time, metric labels.Labels) []chunk.Chunk {
	t.t.Helper()
	fetchedChunks := []chunk.Chunk{}
	seen := map[string]struct{}{}

	for _, tableName := range tablesInInterval(from, through) {
		table, ok := t.tables[tableName]
		if !ok {
			continue
		}

		chunkRefs, err := table.GetChunks(userID, from, through, metric)
		if err != nil {
			t.t.Fatal(err)
		}
		for _, chunkRef := range chunkRefs {
			chunkID := getChunkID(chunkRef)
			if _, ok := seen[chunkID]; ok {
				continue
			}

			chk, err := chunk.ParseExternalKey(userID, chunkID)
			if err != nil {
				t.t.Fatal(err)
			}

			fetchedChunk, err := t.chunkClient.GetChunks(context.Background(), []chunk.Chunk{chk})
			if err != nil {
				t.t.Fatal(err)
			}

			fetchedChunks = append(fetchedChunks, fetchedChunk...)
			seen[chunkID] = struct{}{}
		}
	}

	return fetchedChunks
}

func getChunkID(c logproto.ChunkRef) string {
	return schemaCfg.ExternalKey(c)
}

func newTestStore(t testing.TB) *testStore {
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

	require.NoError(t, schemaCfg.Validate())

	return &testStore{
		chunkClient:  client.NewClient(newTestObjectClient(chunkDir), client.FSEncoder, schemaCfg),
		t:            t,
		objectClient: newTestObjectClient(workdir),
		tables:       map[string]*table{},
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
