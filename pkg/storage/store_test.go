package storage

import (
	"context"
	"io/ioutil"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path"
	"runtime"
	"testing"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/cortexproject/cortex/pkg/chunk"
	cortex_local "github.com/cortexproject/cortex/pkg/chunk/local"
	"github.com/cortexproject/cortex/pkg/chunk/storage"
	"github.com/cortexproject/cortex/pkg/querier/astmapper"
	"github.com/cortexproject/cortex/pkg/util/flagext"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/marshal"
	"github.com/grafana/loki/pkg/storage/stores/local"
	"github.com/grafana/loki/pkg/util/validation"
)

var (
	start      = model.Time(1523750400000)
	m          runtime.MemStats
	ctx        = user.InjectOrgID(context.Background(), "fake")
	chunkStore = getLocalStore()
)

//go test -bench=. -benchmem -memprofile memprofile.out -cpuprofile profile.out
func Benchmark_store_SelectLogsRegexBackward(b *testing.B) {
	benchmarkStoreQuery(b, &logproto.QueryRequest{
		Selector:  `{foo="bar"} |~ "fuzz"`,
		Limit:     1000,
		Start:     time.Unix(0, start.UnixNano()),
		End:       time.Unix(0, (24*time.Hour.Nanoseconds())+start.UnixNano()),
		Direction: logproto.BACKWARD,
	})
}

func Benchmark_store_SelectLogsLogQLBackward(b *testing.B) {
	benchmarkStoreQuery(b, &logproto.QueryRequest{
		Selector:  `{foo="bar"} |= "test" != "toto" |= "fuzz"`,
		Limit:     1000,
		Start:     time.Unix(0, start.UnixNano()),
		End:       time.Unix(0, (24*time.Hour.Nanoseconds())+start.UnixNano()),
		Direction: logproto.BACKWARD,
	})
}

func Benchmark_store_SelectLogsRegexForward(b *testing.B) {
	benchmarkStoreQuery(b, &logproto.QueryRequest{
		Selector:  `{foo="bar"} |~ "fuzz"`,
		Limit:     1000,
		Start:     time.Unix(0, start.UnixNano()),
		End:       time.Unix(0, (24*time.Hour.Nanoseconds())+start.UnixNano()),
		Direction: logproto.FORWARD,
	})
}

func Benchmark_store_SelectLogsForward(b *testing.B) {
	benchmarkStoreQuery(b, &logproto.QueryRequest{
		Selector:  `{foo="bar"}`,
		Limit:     1000,
		Start:     time.Unix(0, start.UnixNano()),
		End:       time.Unix(0, (24*time.Hour.Nanoseconds())+start.UnixNano()),
		Direction: logproto.FORWARD,
	})
}

func Benchmark_store_SelectLogsBackward(b *testing.B) {
	benchmarkStoreQuery(b, &logproto.QueryRequest{
		Selector:  `{foo="bar"}`,
		Limit:     1000,
		Start:     time.Unix(0, start.UnixNano()),
		End:       time.Unix(0, (24*time.Hour.Nanoseconds())+start.UnixNano()),
		Direction: logproto.BACKWARD,
	})
}

// rm -Rf /tmp/benchmark/chunks/ /tmp/benchmark/index
// go run  -mod=vendor ./pkg/storage/hack/main.go
// go test -benchmem -run=^$ -mod=vendor  ./pkg/storage -bench=Benchmark_store_SelectSample   -memprofile memprofile.out -cpuprofile cpuprofile.out
func Benchmark_store_SelectSample(b *testing.B) {
	var sampleRes []logproto.Sample
	for _, test := range []string{
		`count_over_time({foo="bar"}[5m])`,
		`rate({foo="bar"}[5m])`,
		`bytes_rate({foo="bar"}[5m])`,
		`bytes_over_time({foo="bar"}[5m])`,
	} {
		b.Run(test, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				iter, err := chunkStore.SelectSamples(ctx, logql.SelectSampleParams{
					SampleQueryRequest: newSampleQuery(test, time.Unix(0, start.UnixNano()), time.Unix(0, (24*time.Hour.Nanoseconds())+start.UnixNano())),
				})
				if err != nil {
					b.Fatal(err)
				}

				for iter.Next() {
					sampleRes = append(sampleRes, iter.Sample())
				}
				iter.Close()
			}
		})
	}
	log.Print("sample processed ", len(sampleRes))

}

func benchmarkStoreQuery(b *testing.B, query *logproto.QueryRequest) {
	b.ReportAllocs()
	// force to run gc 10x more often this can be useful to detect fast allocation vs leak.
	//debug.SetGCPercent(10)
	stop := make(chan struct{})
	go func() {
		_ = http.ListenAndServe(":6060", http.DefaultServeMux)
	}()
	go func() {
		ticker := time.NewTicker(time.Millisecond)
		for {
			select {
			case <-ticker.C:
				// print and capture the max in use heap size
				printHeap(b, false)
			case <-stop:
				ticker.Stop()
				return
			}
		}
	}()
	for i := 0; i < b.N; i++ {
		iter, err := chunkStore.SelectLogs(ctx, logql.SelectLogParams{QueryRequest: query})
		if err != nil {
			b.Fatal(err)
		}
		res := []logproto.Entry{}
		printHeap(b, true)
		j := uint32(0)
		for iter.Next() {
			j++
			printHeap(b, false)
			res = append(res, iter.Entry())
			// limit result like the querier would do.
			if j == query.Limit {
				break
			}
		}
		iter.Close()
		printHeap(b, true)
		log.Println("line fetched", len(res))
	}
	close(stop)
}

var maxHeapInuse uint64

func printHeap(b *testing.B, show bool) {
	runtime.ReadMemStats(&m)
	if m.HeapInuse > maxHeapInuse {
		maxHeapInuse = m.HeapInuse
	}
	if show {
		log.Printf("Benchmark %d maxHeapInuse: %d Mbytes\n", b.N, maxHeapInuse/1024/1024)
		log.Printf("Benchmark %d currentHeapInuse: %d Mbytes\n", b.N, m.HeapInuse/1024/1024)
	}
}

func getLocalStore() Store {
	limits, err := validation.NewOverrides(validation.Limits{
		MaxQueryLength: 6000 * time.Hour,
	}, nil)
	if err != nil {
		panic(err)
	}
	store, err := NewStore(Config{
		Config: storage.Config{
			BoltDBConfig: cortex_local.BoltDBConfig{Directory: "/tmp/benchmark/index"},
			FSConfig:     cortex_local.FSConfig{Directory: "/tmp/benchmark/chunks"},
		},
		MaxChunkBatchSize: 10,
	}, chunk.StoreConfig{}, chunk.SchemaConfig{
		Configs: []chunk.PeriodConfig{
			{
				From:       chunk.DayTime{Time: start},
				IndexType:  "boltdb",
				ObjectType: "filesystem",
				Schema:     "v9",
				IndexTables: chunk.PeriodicTableConfig{
					Prefix: "index_",
					Period: time.Hour * 168,
				},
			},
		},
	}, limits, nil)
	if err != nil {
		panic(err)
	}
	return store
}

func Test_store_SelectLogs(t *testing.T) {

	tests := []struct {
		name     string
		req      *logproto.QueryRequest
		expected []logproto.Stream
	}{
		{
			"all",
			newQuery("{foo=~\"ba.*\"}", from, from.Add(6*time.Millisecond), nil),
			[]logproto.Stream{
				{
					Labels: "{foo=\"bar\"}",
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},

						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},

						{
							Timestamp: from.Add(4 * time.Millisecond),
							Line:      "5",
						},
						{
							Timestamp: from.Add(5 * time.Millisecond),
							Line:      "6",
						},
					},
				},
				{
					Labels: "{foo=\"bazz\"}",
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},

						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},

						{
							Timestamp: from.Add(4 * time.Millisecond),
							Line:      "5",
						},
						{
							Timestamp: from.Add(5 * time.Millisecond),
							Line:      "6",
						},
					},
				},
			},
		},
		{
			"filter regex",
			newQuery("{foo=~\"ba.*\"} |~ \"1|2|3\" !~ \"2|3\"", from, from.Add(6*time.Millisecond), nil),
			[]logproto.Stream{
				{
					Labels: "{foo=\"bar\"}",
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
					},
				},
				{
					Labels: "{foo=\"bazz\"}",
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
					},
				},
			},
		},
		{
			"filter matcher",
			newQuery("{foo=\"bar\"}", from, from.Add(6*time.Millisecond), nil),
			[]logproto.Stream{
				{
					Labels: "{foo=\"bar\"}",
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},

						{
							Timestamp: from.Add(time.Millisecond),
							Line:      "2",
						},
						{
							Timestamp: from.Add(2 * time.Millisecond),
							Line:      "3",
						},
						{
							Timestamp: from.Add(3 * time.Millisecond),
							Line:      "4",
						},

						{
							Timestamp: from.Add(4 * time.Millisecond),
							Line:      "5",
						},
						{
							Timestamp: from.Add(5 * time.Millisecond),
							Line:      "6",
						},
					},
				},
			},
		},
		{
			"filter time",
			newQuery("{foo=~\"ba.*\"}", from, from.Add(time.Millisecond), nil),
			[]logproto.Stream{
				{
					Labels: "{foo=\"bar\"}",
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
					},
				},
				{
					Labels: "{foo=\"bazz\"}",
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &store{
				Store: storeFixture,
				cfg: Config{
					MaxChunkBatchSize: 10,
				},
			}

			ctx = user.InjectOrgID(context.Background(), "test-user")
			it, err := s.SelectLogs(ctx, logql.SelectLogParams{QueryRequest: tt.req})
			if err != nil {
				t.Errorf("store.LazyQuery() error = %v", err)
				return
			}

			streams, _, err := iter.ReadBatch(it, tt.req.Limit)
			_ = it.Close()
			if err != nil {
				t.Fatalf("error reading batch %s", err)
			}
			assertStream(t, tt.expected, streams.Streams)
		})
	}
}

func Test_store_SelectSample(t *testing.T) {

	tests := []struct {
		name     string
		req      *logproto.SampleQueryRequest
		expected []logproto.Series
	}{
		{
			"all",
			newSampleQuery("count_over_time({foo=~\"ba.*\"}[5m])", from, from.Add(6*time.Millisecond)),
			[]logproto.Series{
				{
					Labels: "{foo=\"bar\"}",
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      xxhash.Sum64String("1"),
							Value:     1.,
						},

						{
							Timestamp: from.Add(time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("2"),
							Value:     1.,
						},
						{
							Timestamp: from.Add(2 * time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("3"),
							Value:     1.,
						},
						{
							Timestamp: from.Add(3 * time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("4"),
							Value:     1.,
						},

						{
							Timestamp: from.Add(4 * time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("5"),
							Value:     1.,
						},
						{
							Timestamp: from.Add(5 * time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("6"),
							Value:     1.,
						},
					},
				},
				{
					Labels: "{foo=\"bazz\"}",
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      xxhash.Sum64String("1"),
							Value:     1.,
						},

						{
							Timestamp: from.Add(time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("2"),
							Value:     1.,
						},
						{
							Timestamp: from.Add(2 * time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("3"),
							Value:     1.,
						},
						{
							Timestamp: from.Add(3 * time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("4"),
							Value:     1.,
						},

						{
							Timestamp: from.Add(4 * time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("5"),
							Value:     1.,
						},
						{
							Timestamp: from.Add(5 * time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("6"),
							Value:     1.,
						},
					},
				},
			},
		},
		{
			"filter regex",
			newSampleQuery("rate({foo=~\"ba.*\"} |~ \"1|2|3\" !~ \"2|3\"[1m])", from, from.Add(6*time.Millisecond)),
			[]logproto.Series{
				{
					Labels: "{foo=\"bar\"}",
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      xxhash.Sum64String("1"),
							Value:     1.,
						},
					},
				},
				{
					Labels: "{foo=\"bazz\"}",
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      xxhash.Sum64String("1"),
							Value:     1.,
						},
					},
				},
			},
		},
		{
			"filter matcher",
			newSampleQuery("count_over_time({foo=\"bar\"}[10m])", from, from.Add(6*time.Millisecond)),
			[]logproto.Series{
				{
					Labels: "{foo=\"bar\"}",
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      xxhash.Sum64String("1"),
							Value:     1.,
						},

						{
							Timestamp: from.Add(time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("2"),
							Value:     1.,
						},
						{
							Timestamp: from.Add(2 * time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("3"),
							Value:     1.,
						},
						{
							Timestamp: from.Add(3 * time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("4"),
							Value:     1.,
						},

						{
							Timestamp: from.Add(4 * time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("5"),
							Value:     1.,
						},
						{
							Timestamp: from.Add(5 * time.Millisecond).UnixNano(),
							Hash:      xxhash.Sum64String("6"),
							Value:     1.,
						},
					},
				},
			},
		},
		{
			"filter time",
			newSampleQuery("count_over_time({foo=~\"ba.*\"}[1s])", from, from.Add(time.Millisecond)),
			[]logproto.Series{
				{
					Labels: "{foo=\"bar\"}",
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      xxhash.Sum64String("1"),
							Value:     1.,
						},
					},
				},
				{
					Labels: "{foo=\"bazz\"}",
					Samples: []logproto.Sample{
						{
							Timestamp: from.UnixNano(),
							Hash:      xxhash.Sum64String("1"),
							Value:     1.,
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &store{
				Store: storeFixture,
				cfg: Config{
					MaxChunkBatchSize: 10,
				},
			}

			ctx = user.InjectOrgID(context.Background(), "test-user")
			it, err := s.SelectSamples(ctx, logql.SelectSampleParams{SampleQueryRequest: tt.req})
			if err != nil {
				t.Errorf("store.LazyQuery() error = %v", err)
				return
			}

			series, _, err := iter.ReadSampleBatch(it, uint32(100000))
			_ = it.Close()
			if err != nil {
				t.Fatalf("error reading batch %s", err)
			}
			assertSeries(t, tt.expected, series.Series)
		})
	}
}

func Test_store_GetSeries(t *testing.T) {

	tests := []struct {
		name      string
		req       *logproto.QueryRequest
		expected  []logproto.SeriesIdentifier
		batchSize int
	}{
		{
			"all",
			newQuery("{foo=~\"ba.*\"}", from, from.Add(6*time.Millisecond), nil),
			[]logproto.SeriesIdentifier{
				{Labels: mustParseLabels("{foo=\"bar\"}")},
				{Labels: mustParseLabels("{foo=\"bazz\"}")},
			},
			1,
		},
		{
			"all-single-batch",
			newQuery("{foo=~\"ba.*\"}", from, from.Add(6*time.Millisecond), nil),
			[]logproto.SeriesIdentifier{
				{Labels: mustParseLabels("{foo=\"bar\"}")},
				{Labels: mustParseLabels("{foo=\"bazz\"}")},
			},
			5,
		},
		{
			"regexp filter (post chunk fetching)",
			newQuery("{foo=~\"bar.*\"}", from, from.Add(6*time.Millisecond), nil),
			[]logproto.SeriesIdentifier{
				{Labels: mustParseLabels("{foo=\"bar\"}")},
			},
			1,
		},
		{
			"filter matcher",
			newQuery("{foo=\"bar\"}", from, from.Add(6*time.Millisecond), nil),
			[]logproto.SeriesIdentifier{
				{Labels: mustParseLabels("{foo=\"bar\"}")},
			},
			1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &store{
				Store: storeFixture,
				cfg: Config{
					MaxChunkBatchSize: tt.batchSize,
				},
			}
			ctx = user.InjectOrgID(context.Background(), "test-user")
			out, err := s.GetSeries(ctx, logql.SelectLogParams{QueryRequest: tt.req})
			if err != nil {
				t.Errorf("store.GetSeries() error = %v", err)
				return
			}
			require.Equal(t, tt.expected, out)
		})
	}
}

func Test_store_decodeReq_Matchers(t *testing.T) {
	tests := []struct {
		name     string
		req      *logproto.QueryRequest
		matchers []*labels.Matcher
	}{
		{
			"unsharded",
			newQuery("{foo=~\"ba.*\"}", from, from.Add(6*time.Millisecond), nil),
			[]*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "foo", "ba.*"),
				labels.MustNewMatcher(labels.MatchEqual, labels.MetricName, "logs"),
			},
		},
		{
			"unsharded",
			newQuery(
				"{foo=~\"ba.*\"}", from, from.Add(6*time.Millisecond),
				[]astmapper.ShardAnnotation{
					{Shard: 1, Of: 2},
				},
			),
			[]*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "foo", "ba.*"),
				labels.MustNewMatcher(labels.MatchEqual, labels.MetricName, "logs"),
				labels.MustNewMatcher(
					labels.MatchEqual,
					astmapper.ShardLabel,
					astmapper.ShardAnnotation{Shard: 1, Of: 2}.String(),
				),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms, _, _, _, err := decodeReq(logql.SelectLogParams{QueryRequest: tt.req})
			if err != nil {
				t.Errorf("store.GetSeries() error = %v", err)
				return
			}
			require.Equal(t, tt.matchers, ms)
		})
	}
}

type timeRange struct {
	from, to time.Time
}

func TestStore_MultipleBoltDBShippersInConfig(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "multiple-boltdb-shippers")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	limits, err := validation.NewOverrides(validation.Limits{}, nil)
	require.NoError(t, err)

	// config for BoltDB Shipper
	boltdbShipperConfig := local.ShipperConfig{}
	flagext.DefaultValues(&boltdbShipperConfig)
	boltdbShipperConfig.ActiveIndexDirectory = path.Join(tempDir, "index")
	boltdbShipperConfig.SharedStoreType = "filesystem"
	boltdbShipperConfig.CacheLocation = path.Join(tempDir, "boltdb-shipper-cache")

	// dates for activation of boltdb shippers
	firstStoreDate := parseDate("2019-01-01")
	secondStoreDate := parseDate("2019-01-02")

	config := Config{
		Config: storage.Config{
			FSConfig: cortex_local.FSConfig{Directory: path.Join(tempDir, "chunks")},
		},
		BoltDBShipperConfig: boltdbShipperConfig,
	}

	RegisterCustomIndexClients(config, nil)

	store, err := NewStore(config, chunk.StoreConfig{}, chunk.SchemaConfig{
		Configs: []chunk.PeriodConfig{
			{
				From:       chunk.DayTime{Time: timeToModelTime(firstStoreDate)},
				IndexType:  "boltdb-shipper",
				ObjectType: "filesystem",
				Schema:     "v9",
				IndexTables: chunk.PeriodicTableConfig{
					Prefix: "index_",
					Period: time.Hour * 168,
				},
			},
			{
				From:       chunk.DayTime{Time: timeToModelTime(secondStoreDate)},
				IndexType:  "boltdb-shipper",
				ObjectType: "filesystem",
				Schema:     "v11",
				IndexTables: chunk.PeriodicTableConfig{
					Prefix: "index_",
					Period: time.Hour * 168,
				},
				RowShards: 2,
			},
		},
	}, limits, nil)
	require.NoError(t, err)

	// time ranges adding a chunk for each store and a chunk which overlaps both the stores
	chunksToBuildForTimeRanges := []timeRange{
		{
			// chunk just for first store
			secondStoreDate.Add(-3 * time.Hour),
			secondStoreDate.Add(-2 * time.Hour),
		},
		{
			// chunk overlapping both the stores
			secondStoreDate.Add(-time.Hour),
			secondStoreDate.Add(time.Hour),
		},
		{
			// chunk just for second store
			secondStoreDate.Add(2 * time.Hour),
			secondStoreDate.Add(3 * time.Hour),
		},
	}

	// build and add chunks to the store
	addedChunkIDs := map[string]struct{}{}
	for _, tr := range chunksToBuildForTimeRanges {
		chk := newChunk(buildTestStreams(fooLabelsWithName, tr))

		err := store.PutOne(ctx, chk.From, chk.Through, chk)
		require.NoError(t, err)

		addedChunkIDs[chk.ExternalKey()] = struct{}{}
	}

	// get all the chunks from both the stores
	chunks, err := store.Get(ctx, "fake", timeToModelTime(firstStoreDate), timeToModelTime(secondStoreDate.Add(24*time.Hour)), newMatchers(fooLabelsWithName)...)
	require.NoError(t, err)

	// we get common chunk twice because it is indexed in both the stores
	require.Len(t, chunks, len(addedChunkIDs)+1)

	// check whether we got back all the chunks which were added
	for i := range chunks {
		_, ok := addedChunkIDs[chunks[i].ExternalKey()]
		require.True(t, ok)
	}
}

func mustParseLabels(s string) map[string]string {
	l, err := marshal.NewLabelSet(s)

	if err != nil {
		log.Fatalf("Failed to parse %s", s)
	}

	return l
}

func parseDate(in string) time.Time {
	t, err := time.Parse("2006-01-02", in)
	if err != nil {
		panic(err)
	}
	return t
}

func buildTestStreams(labels string, tr timeRange) logproto.Stream {
	stream := logproto.Stream{
		Labels:  labels,
		Entries: []logproto.Entry{},
	}

	for from := tr.from; from.Before(tr.to); from = from.Add(time.Second) {
		stream.Entries = append(stream.Entries, logproto.Entry{
			Timestamp: from,
			Line:      from.String(),
		})
	}

	return stream
}

func timeToModelTime(t time.Time) model.Time {
	return model.TimeFromUnixNano(t.UnixNano())
}
