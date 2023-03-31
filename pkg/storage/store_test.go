package storage

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
	_ "net/http/pprof"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/dskit/flagext"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/querier/astmapper"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper"
	"github.com/grafana/loki/pkg/storage/stores/shipper"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/marshal"
	"github.com/grafana/loki/pkg/validation"
)

var (
	start      = model.Time(1523750400000)
	m          runtime.MemStats
	ctx        = user.InjectOrgID(context.Background(), "fake")
	cm         = NewClientMetrics()
	chunkStore = getLocalStore(cm)
)

// go test -bench=. -benchmem -memprofile memprofile.out -cpuprofile profile.out
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
	for _, test := range []string{
		`count_over_time({foo="bar"}[5m])`,
		`rate({foo="bar"}[5m])`,
		`bytes_rate({foo="bar"}[5m])`,
		`bytes_over_time({foo="bar"}[5m])`,
	} {
		b.Run(test, func(b *testing.B) {
			sampleCount := 0
			for i := 0; i < b.N; i++ {
				iter, err := chunkStore.SelectSamples(ctx, logql.SelectSampleParams{
					SampleQueryRequest: newSampleQuery(test, time.Unix(0, start.UnixNano()), time.Unix(0, (24*time.Hour.Nanoseconds())+start.UnixNano()), nil),
				})
				if err != nil {
					b.Fatal(err)
				}

				for iter.Next() {
					_ = iter.Sample()
					sampleCount++
				}
				iter.Close()
			}
			b.ReportMetric(float64(sampleCount)/float64(b.N), "samples/op")
		})
	}
}

func benchmarkStoreQuery(b *testing.B, query *logproto.QueryRequest) {
	b.ReportAllocs()
	// force to run gc 10x more often this can be useful to detect fast allocation vs leak.
	// debug.SetGCPercent(10)
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

func getLocalStore(cm ClientMetrics) Store {
	limits, err := validation.NewOverrides(validation.Limits{
		MaxQueryLength: model.Duration(6000 * time.Hour),
	}, nil)
	if err != nil {
		panic(err)
	}

	storeConfig := Config{
		BoltDBConfig:      local.BoltDBConfig{Directory: "/tmp/benchmark/index"},
		FSConfig:          local.FSConfig{Directory: "/tmp/benchmark/chunks"},
		MaxChunkBatchSize: 10,
	}

	schemaConfig := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:       config.DayTime{Time: start},
				IndexType:  "boltdb",
				ObjectType: config.StorageTypeFileSystem,
				Schema:     "v9",
				IndexTables: config.PeriodicTableConfig{
					Prefix: "index_",
					Period: time.Hour * 168,
				},
			},
		},
	}

	store, err := NewStore(storeConfig, config.ChunkStoreConfig{}, schemaConfig, limits, cm, nil, util_log.Logger)
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
			newQuery("{foo=~\"ba.*\"}", from, from.Add(6*time.Millisecond), nil, nil),
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
			newQuery("{foo=~\"ba.*\"} |~ \"1|2|3\" !~ \"2|3\"", from, from.Add(6*time.Millisecond), nil, nil),
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
			newQuery("{foo=\"bar\"}", from, from.Add(6*time.Millisecond), nil, nil),
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
			newQuery("{foo=~\"ba.*\"}", from, from.Add(time.Millisecond), nil, nil),
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
			"delete covers whole time range",
			newQuery(
				"{foo=~\"ba.*\"}",
				from,
				from.Add(6*time.Millisecond),
				nil,
				[]*logproto.Delete{
					{
						Selector: `{foo="bar"}`,
						Start:    from.Add(-1 * time.Millisecond).UnixNano(),
						End:      from.Add(7 * time.Millisecond).UnixNano(),
					},
					{
						Selector: `{foo="bazz"} |= "6"`,
						Start:    from.Add(-1 * time.Millisecond).UnixNano(),
						End:      from.Add(7 * time.Millisecond).UnixNano(),
					},
				}),
			[]logproto.Stream{
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
					},
				},
			},
		},
		{
			"delete covers partial time range",
			newQuery(
				"{foo=~\"ba.*\"}",
				from,
				from.Add(6*time.Millisecond),
				nil,
				[]*logproto.Delete{
					{
						Selector: `{foo="bar"}`,
						Start:    from.Add(-1 * time.Millisecond).UnixNano(),
						End:      from.Add(3 * time.Millisecond).UnixNano(),
					},
					{
						Selector: `{foo="bazz"} |= "2"`,
						Start:    from.Add(-1 * time.Millisecond).UnixNano(),
						End:      from.Add(3 * time.Millisecond).UnixNano(),
					},
				}),
			[]logproto.Stream{
				{
					Labels: "{foo=\"bar\"}",
					Entries: []logproto.Entry{
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &store{
				Store: storeFixture,
				cfg: Config{
					MaxChunkBatchSize: 10,
				},
				chunkMetrics: NilMetrics,
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
			newSampleQuery("count_over_time({foo=~\"ba.*\"}[5m])", from, from.Add(6*time.Millisecond), nil),
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
			newSampleQuery("rate({foo=~\"ba.*\"} |~ \"1|2|3\" !~ \"2|3\"[1m])", from, from.Add(6*time.Millisecond), nil),
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
			newSampleQuery("count_over_time({foo=\"bar\"}[10m])", from, from.Add(6*time.Millisecond), nil),
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
			newSampleQuery("count_over_time({foo=~\"ba.*\"}[1s])", from, from.Add(time.Millisecond), nil),
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
			"delete covers whole time range",
			newSampleQuery(
				"count_over_time({foo=~\"ba.*\"}[5m])",
				from,
				from.Add(6*time.Millisecond),
				[]*logproto.Delete{
					{
						Selector: `{foo="bar"}`,
						Start:    from.Add(-1 * time.Millisecond).UnixNano(),
						End:      from.Add(7 * time.Millisecond).UnixNano(),
					},
					{
						Selector: `{foo="bazz"} |= "6"`,
						Start:    from.Add(-1 * time.Millisecond).UnixNano(),
						End:      from.Add(7 * time.Millisecond).UnixNano(),
					},
				}),
			[]logproto.Series{
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
					},
				},
			},
		},
		{
			"delete covers partial time range",
			newSampleQuery(
				"count_over_time({foo=~\"ba.*\"}[5m])",
				from,
				from.Add(6*time.Millisecond),
				[]*logproto.Delete{
					{
						Selector: `{foo="bar"}`,
						Start:    from.Add(-1 * time.Millisecond).UnixNano(),
						End:      from.Add(3 * time.Millisecond).UnixNano(),
					},
					{
						Selector: `{foo="bazz"} |= "2"`,
						Start:    from.Add(-1 * time.Millisecond).UnixNano(),
						End:      from.Add(3 * time.Millisecond).UnixNano(),
					},
				}),
			[]logproto.Series{
				{
					Labels: "{foo=\"bar\"}",
					Samples: []logproto.Sample{
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &store{
				Store: storeFixture,
				cfg: Config{
					MaxChunkBatchSize: 10,
				},
				chunkMetrics: NilMetrics,
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

type fakeChunkFilterer struct{}

func (f fakeChunkFilterer) ForRequest(ctx context.Context) chunk.Filterer {
	return f
}

func (f fakeChunkFilterer) ShouldFilter(metric labels.Labels) bool {
	return metric.Get("foo") == "bazz"
}

func Test_ChunkFilterer(t *testing.T) {
	s := &store{
		Store: storeFixture,
		cfg: Config{
			MaxChunkBatchSize: 10,
		},
		chunkMetrics: NilMetrics,
	}
	s.SetChunkFilterer(&fakeChunkFilterer{})
	ctx = user.InjectOrgID(context.Background(), "test-user")
	it, err := s.SelectSamples(ctx, logql.SelectSampleParams{SampleQueryRequest: newSampleQuery("count_over_time({foo=~\"ba.*\"}[1s])", from, from.Add(1*time.Hour), nil)})
	if err != nil {
		t.Errorf("store.SelectSamples() error = %v", err)
		return
	}
	defer it.Close()
	for it.Next() {
		v := mustParseLabels(it.Labels())["foo"]
		require.NotEqual(t, "bazz", v)
	}

	logit, err := s.SelectLogs(ctx, logql.SelectLogParams{QueryRequest: newQuery("{foo=~\"ba.*\"}", from, from.Add(1*time.Hour), nil, nil)})
	if err != nil {
		t.Errorf("store.SelectLogs() error = %v", err)
		return
	}
	defer logit.Close()
	for logit.Next() {
		v := mustParseLabels(it.Labels())["foo"]
		require.NotEqual(t, "bazz", v)
	}
	ids, err := s.Series(ctx, logql.SelectLogParams{QueryRequest: newQuery("{foo=~\"ba.*\"}", from, from.Add(1*time.Hour), nil, nil)})
	require.NoError(t, err)
	for _, id := range ids {
		v := id.Labels["foo"]
		require.NotEqual(t, "bazz", v)
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
			newQuery("{foo=~\"ba.*\"}", from, from.Add(6*time.Millisecond), nil, nil),
			[]logproto.SeriesIdentifier{
				{Labels: mustParseLabels("{foo=\"bar\"}")},
				{Labels: mustParseLabels("{foo=\"bazz\"}")},
			},
			1,
		},
		{
			"all-single-batch",
			newQuery("{foo=~\"ba.*\"}", from, from.Add(6*time.Millisecond), nil, nil),
			[]logproto.SeriesIdentifier{
				{Labels: mustParseLabels("{foo=\"bar\"}")},
				{Labels: mustParseLabels("{foo=\"bazz\"}")},
			},
			5,
		},
		{
			"regexp filter (post chunk fetching)",
			newQuery("{foo=~\"bar.*\"}", from, from.Add(6*time.Millisecond), nil, nil),
			[]logproto.SeriesIdentifier{
				{Labels: mustParseLabels("{foo=\"bar\"}")},
			},
			1,
		},
		{
			"filter matcher",
			newQuery("{foo=\"bar\"}", from, from.Add(6*time.Millisecond), nil, nil),
			[]logproto.SeriesIdentifier{
				{Labels: mustParseLabels("{foo=\"bar\"}")},
			},
			1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &store{
				Store: newMockChunkStore(streamsFixture),
				cfg: Config{
					MaxChunkBatchSize: tt.batchSize,
				},
				chunkMetrics: NilMetrics,
			}
			ctx = user.InjectOrgID(context.Background(), "test-user")
			out, err := s.Series(ctx, logql.SelectLogParams{QueryRequest: tt.req})
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
			newQuery("{foo=~\"ba.*\"}", from, from.Add(6*time.Millisecond), nil, nil),
			[]*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "foo", "ba(?-s:.)*?"),
				labels.MustNewMatcher(labels.MatchEqual, labels.MetricName, "logs"),
			},
		},
		{
			"sharded",
			newQuery(
				"{foo=~\"ba.*\"}", from, from.Add(6*time.Millisecond),
				[]astmapper.ShardAnnotation{
					{Shard: 1, Of: 2},
				},
				nil,
			),
			[]*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "foo", "ba(?-s:.)*?"),
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
			ms, _, _, err := decodeReq(logql.SelectLogParams{QueryRequest: tt.req})
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
	tempDir := t.TempDir()

	limits, err := validation.NewOverrides(validation.Limits{}, nil)
	require.NoError(t, err)

	// config for BoltDB Shipper
	boltdbShipperConfig := shipper.Config{}
	flagext.DefaultValues(&boltdbShipperConfig)
	boltdbShipperConfig.ActiveIndexDirectory = path.Join(tempDir, "index")
	boltdbShipperConfig.SharedStoreType = config.StorageTypeFileSystem
	boltdbShipperConfig.CacheLocation = path.Join(tempDir, "boltdb-shipper-cache")
	boltdbShipperConfig.Mode = indexshipper.ModeReadWrite

	// dates for activation of boltdb shippers
	firstStoreDate := parseDate("2019-01-01")
	secondStoreDate := parseDate("2019-01-02")

	cfg := Config{
		FSConfig:            local.FSConfig{Directory: path.Join(tempDir, "chunks")},
		BoltDBShipperConfig: boltdbShipperConfig,
	}

	schemaConfig := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:       config.DayTime{Time: timeToModelTime(firstStoreDate)},
				IndexType:  "boltdb-shipper",
				ObjectType: config.StorageTypeFileSystem,
				Schema:     "v9",
				IndexTables: config.PeriodicTableConfig{
					Prefix: "index_",
					Period: time.Hour * 168,
				},
			},
			{
				From:       config.DayTime{Time: timeToModelTime(secondStoreDate)},
				IndexType:  "boltdb-shipper",
				ObjectType: config.StorageTypeFileSystem,
				Schema:     "v11",
				IndexTables: config.PeriodicTableConfig{
					Prefix: "index_",
					Period: time.Hour * 168,
				},
				RowShards: 2,
			},
		},
	}

	ResetBoltDBIndexClientWithShipper()
	store, err := NewStore(cfg, config.ChunkStoreConfig{}, schemaConfig, limits, cm, nil, util_log.Logger)
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

		addedChunkIDs[schemaConfig.ExternalKey(chk.ChunkRef)] = struct{}{}
	}

	// recreate the store because boltdb-shipper now runs queriers on snapshots which are created every 1 min and during startup.
	store.Stop()

	store, err = NewStore(cfg, config.ChunkStoreConfig{}, schemaConfig, limits, cm, nil, util_log.Logger)
	require.NoError(t, err)

	defer store.Stop()

	// get all the chunks from both the stores
	chunks, _, err := store.GetChunkRefs(ctx, "fake", timeToModelTime(firstStoreDate), timeToModelTime(secondStoreDate.Add(24*time.Hour)), newMatchers(fooLabelsWithName.String())...)
	require.NoError(t, err)
	var totalChunks int
	for _, chks := range chunks {
		totalChunks += len(chks)
	}
	// we get common chunk twice because it is indexed in both the stores
	require.Equal(t, totalChunks, len(addedChunkIDs)+1)

	// check whether we got back all the chunks which were added
	for i := range chunks {
		for _, c := range chunks[i] {
			_, ok := addedChunkIDs[schemaConfig.ExternalKey(c.ChunkRef)]
			require.True(t, ok)
		}
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

func buildTestStreams(labels labels.Labels, tr timeRange) logproto.Stream {
	stream := logproto.Stream{
		Labels:  labels.String(),
		Hash:    labels.Hash(),
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

func Test_OverlappingChunks(t *testing.T) {
	chunks := []chunk.Chunk{
		newChunk(logproto.Stream{
			Labels: `{foo="bar"}`,
			Entries: []logproto.Entry{
				{Timestamp: time.Unix(0, 1), Line: "1"},
				{Timestamp: time.Unix(0, 4), Line: "4"},
			},
		}),
		newChunk(logproto.Stream{
			Labels: `{foo="bar"}`,
			Entries: []logproto.Entry{
				{Timestamp: time.Unix(0, 2), Line: "2"},
				{Timestamp: time.Unix(0, 3), Line: "3"},
			},
		}),
	}
	s := &store{
		Store: &mockChunkStore{chunks: chunks, client: &mockChunkStoreClient{chunks: chunks}},
		cfg: Config{
			MaxChunkBatchSize: 10,
		},
		chunkMetrics: NilMetrics,
	}

	ctx = user.InjectOrgID(context.Background(), "test-user")
	it, err := s.SelectLogs(ctx, logql.SelectLogParams{QueryRequest: &logproto.QueryRequest{
		Selector:  `{foo="bar"}`,
		Limit:     1000,
		Direction: logproto.BACKWARD,
		Start:     time.Unix(0, 0),
		End:       time.Unix(0, 10),
	}})
	if err != nil {
		t.Errorf("store.SelectLogs() error = %v", err)
		return
	}
	defer it.Close()
	require.True(t, it.Next())
	require.Equal(t, "4", it.Entry().Line)
	require.True(t, it.Next())
	require.Equal(t, "3", it.Entry().Line)
	require.True(t, it.Next())
	require.Equal(t, "2", it.Entry().Line)
	require.True(t, it.Next())
	require.Equal(t, "1", it.Entry().Line)
	require.False(t, it.Next())
}

func Test_GetSeries(t *testing.T) {
	var (
		store = &store{
			Store: newMockChunkStore([]*logproto.Stream{
				{
					Labels: `{foo="bar",buzz="boo"}`,
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(0, 1), Line: "1"},
					},
				},
				{
					Labels: `{foo="buzz"}`,
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(0, 1), Line: "1"},
					},
				},
				{
					Labels: `{bar="foo"}`,
					Entries: []logproto.Entry{
						{Timestamp: time.Unix(0, 1), Line: "1"},
					},
				},
			}),
			cfg: Config{
				MaxChunkBatchSize: 10,
			},
			chunkMetrics: NilMetrics,
		}
		ctx            = user.InjectOrgID(context.Background(), "test-user")
		expectedSeries = []logproto.SeriesIdentifier{
			{
				Labels: map[string]string{"bar": "foo"},
			},
			{
				Labels: map[string]string{"foo": "bar", "buzz": "boo"},
			},
			{
				Labels: map[string]string{"foo": "buzz"},
			},
		}
	)

	for _, tt := range []struct {
		name           string
		req            logql.SelectLogParams
		expectedSeries []logproto.SeriesIdentifier
	}{
		{
			"all series",
			logql.SelectLogParams{
				QueryRequest: &logproto.QueryRequest{
					Selector: ``,
					Start:    time.Unix(0, 0),
					End:      time.Unix(0, 10),
				},
			},
			expectedSeries,
		},
		{
			"selected series",
			logql.SelectLogParams{
				QueryRequest: &logproto.QueryRequest{
					Selector: `{buzz=~".oo"}`,
					Start:    time.Unix(0, 0),
					End:      time.Unix(0, 10),
				},
			},
			[]logproto.SeriesIdentifier{
				{
					Labels: map[string]string{"foo": "bar", "buzz": "boo"},
				},
			},
		},
		{
			"no match",
			logql.SelectLogParams{
				QueryRequest: &logproto.QueryRequest{
					Selector: `{buzz=~"foo"}`,
					Start:    time.Unix(0, 0),
					End:      time.Unix(0, 10),
				},
			},
			[]logproto.SeriesIdentifier{},
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			series, err := store.Series(ctx, tt.req)
			require.NoError(t, err)
			require.Equal(t, tt.expectedSeries, series)
		})
	}
}

func TestGetIndexStoreTableRanges(t *testing.T) {
	now := model.Now()
	schemaConfig := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:       config.DayTime{Time: now.Add(30 * 24 * time.Hour)},
				IndexType:  config.BoltDBShipperType,
				ObjectType: config.StorageTypeFileSystem,
				Schema:     "v9",
				IndexTables: config.PeriodicTableConfig{
					Prefix: "index_",
					Period: time.Hour * 24,
				},
			},
			{
				From:       config.DayTime{Time: now.Add(20 * 24 * time.Hour)},
				IndexType:  config.BoltDBShipperType,
				ObjectType: config.StorageTypeFileSystem,
				Schema:     "v11",
				IndexTables: config.PeriodicTableConfig{
					Prefix: "index_",
					Period: time.Hour * 24,
				},
				RowShards: 2,
			},
			{
				From:       config.DayTime{Time: now.Add(15 * 24 * time.Hour)},
				IndexType:  config.TSDBType,
				ObjectType: config.StorageTypeFileSystem,
				Schema:     "v11",
				IndexTables: config.PeriodicTableConfig{
					Prefix: "index_",
					Period: time.Hour * 24,
				},
				RowShards: 2,
			},
			{
				From:       config.DayTime{Time: now.Add(10 * 24 * time.Hour)},
				IndexType:  config.StorageTypeBigTable,
				ObjectType: config.StorageTypeFileSystem,
				Schema:     "v11",
				IndexTables: config.PeriodicTableConfig{
					Prefix: "index_",
					Period: time.Hour * 24,
				},
				RowShards: 2,
			},
			{
				From:       config.DayTime{Time: now.Add(5 * 24 * time.Hour)},
				IndexType:  config.TSDBType,
				ObjectType: config.StorageTypeFileSystem,
				Schema:     "v11",
				IndexTables: config.PeriodicTableConfig{
					Prefix: "index_",
					Period: time.Hour * 24,
				},
				RowShards: 2,
			},
		},
	}

	require.Equal(t, config.TableRanges{
		{
			Start:        schemaConfig.Configs[0].From.Unix() / int64(schemaConfig.Configs[0].IndexTables.Period/time.Second),
			End:          schemaConfig.Configs[1].From.Add(-time.Millisecond).Unix() / int64(schemaConfig.Configs[0].IndexTables.Period/time.Second),
			PeriodConfig: &schemaConfig.Configs[0],
		},
		{
			Start:        schemaConfig.Configs[1].From.Unix() / int64(schemaConfig.Configs[0].IndexTables.Period/time.Second),
			End:          schemaConfig.Configs[2].From.Add(-time.Millisecond).Unix() / int64(schemaConfig.Configs[0].IndexTables.Period/time.Second),
			PeriodConfig: &schemaConfig.Configs[1],
		},
	}, getIndexStoreTableRanges(config.BoltDBShipperType, schemaConfig.Configs))

	require.Equal(t, config.TableRanges{
		{
			Start:        schemaConfig.Configs[3].From.Unix() / int64(schemaConfig.Configs[0].IndexTables.Period/time.Second),
			End:          schemaConfig.Configs[4].From.Add(-time.Millisecond).Unix() / int64(schemaConfig.Configs[0].IndexTables.Period/time.Second),
			PeriodConfig: &schemaConfig.Configs[3],
		},
	}, getIndexStoreTableRanges(config.StorageTypeBigTable, schemaConfig.Configs))

	require.Equal(t, config.TableRanges{
		{
			Start:        schemaConfig.Configs[2].From.Unix() / int64(schemaConfig.Configs[0].IndexTables.Period/time.Second),
			End:          schemaConfig.Configs[3].From.Add(-time.Millisecond).Unix() / int64(schemaConfig.Configs[0].IndexTables.Period/time.Second),
			PeriodConfig: &schemaConfig.Configs[2],
		},
		{
			Start:        schemaConfig.Configs[4].From.Unix() / int64(schemaConfig.Configs[0].IndexTables.Period/time.Second),
			End:          model.Time(math.MaxInt64).Unix() / int64(schemaConfig.Configs[0].IndexTables.Period/time.Second),
			PeriodConfig: &schemaConfig.Configs[4],
		},
	}, getIndexStoreTableRanges(config.TSDBType, schemaConfig.Configs))
}

func TestStore_BoltdbTsdbSameIndexPrefix(t *testing.T) {
	tempDir := t.TempDir()

	ingesterName := "ingester-1"
	limits, err := validation.NewOverrides(validation.Limits{}, nil)
	require.NoError(t, err)

	// config for BoltDB Shipper
	boltdbShipperConfig := shipper.Config{}
	flagext.DefaultValues(&boltdbShipperConfig)
	boltdbShipperConfig.ActiveIndexDirectory = path.Join(tempDir, "index")
	boltdbShipperConfig.SharedStoreType = config.StorageTypeFileSystem
	boltdbShipperConfig.CacheLocation = path.Join(tempDir, "boltdb-shipper-cache")
	boltdbShipperConfig.Mode = indexshipper.ModeReadWrite
	boltdbShipperConfig.IngesterName = ingesterName

	// config for tsdb Shipper
	tsdbShipperConfig := indexshipper.Config{}
	flagext.DefaultValues(&tsdbShipperConfig)
	tsdbShipperConfig.ActiveIndexDirectory = path.Join(tempDir, "tsdb-index")
	tsdbShipperConfig.SharedStoreType = config.StorageTypeFileSystem
	tsdbShipperConfig.CacheLocation = path.Join(tempDir, "tsdb-shipper-cache")
	tsdbShipperConfig.Mode = indexshipper.ModeReadWrite
	tsdbShipperConfig.IngesterName = ingesterName

	// dates for activation of boltdb shippers
	boltdbShipperStartDate := parseDate("2019-01-01")
	tsdbStartDate := parseDate("2019-01-02")

	cfg := Config{
		FSConfig:            local.FSConfig{Directory: path.Join(tempDir, "chunks")},
		BoltDBShipperConfig: boltdbShipperConfig,
		TSDBShipperConfig:   tsdbShipperConfig,
	}

	schemaConfig := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:       config.DayTime{Time: timeToModelTime(boltdbShipperStartDate)},
				IndexType:  "boltdb-shipper",
				ObjectType: config.StorageTypeFileSystem,
				Schema:     "v12",
				IndexTables: config.PeriodicTableConfig{
					Prefix: "index_",
					Period: time.Hour * 24,
				},
				RowShards: 2,
			},
			{
				From:       config.DayTime{Time: timeToModelTime(tsdbStartDate)},
				IndexType:  "tsdb",
				ObjectType: config.StorageTypeFileSystem,
				Schema:     "v12",
				IndexTables: config.PeriodicTableConfig{
					Prefix: "index_",
					Period: time.Hour * 24,
				},
			},
		},
	}

	ResetBoltDBIndexClientWithShipper()
	store, err := NewStore(cfg, config.ChunkStoreConfig{}, schemaConfig, limits, cm, nil, util_log.Logger)
	require.NoError(t, err)

	// time ranges adding a chunk for each store and a chunk which overlaps both the stores
	chunksToBuildForTimeRanges := []timeRange{
		{
			// chunk just for first store
			tsdbStartDate.Add(-3 * time.Hour),
			tsdbStartDate.Add(-2 * time.Hour),
		},
		{
			// chunk overlapping both the stores
			tsdbStartDate.Add(-time.Hour),
			tsdbStartDate.Add(time.Hour),
		},
		{
			// chunk just for second store
			tsdbStartDate.Add(2 * time.Hour),
			tsdbStartDate.Add(3 * time.Hour),
		},
	}

	// build and add chunks to the store
	addedChunkIDs := map[string]struct{}{}
	for _, tr := range chunksToBuildForTimeRanges {
		chk := newChunk(buildTestStreams(fooLabelsWithName, tr))

		err := store.PutOne(ctx, chk.From, chk.Through, chk)
		require.NoError(t, err)

		addedChunkIDs[schemaConfig.ExternalKey(chk.ChunkRef)] = struct{}{}
	}

	// recreate the store because boltdb-shipper now runs queriers on snapshots which are created every 1 min and during startup.
	store.Stop()

	// there should be 2 index tables in the object storage
	indexTables, err := os.ReadDir(filepath.Join(cfg.FSConfig.Directory, "index"))
	require.NoError(t, err)
	require.Len(t, indexTables, 2)
	require.Equal(t, "index_17897", indexTables[0].Name())
	require.Equal(t, "index_17898", indexTables[1].Name())

	// there should be just 1 file in each table in the object storage
	boltdbFiles, err := os.ReadDir(filepath.Join(cfg.FSConfig.Directory, "index", indexTables[0].Name()))
	require.NoError(t, err)
	require.Len(t, boltdbFiles, 1)
	require.Regexp(t, regexp.MustCompile(fmt.Sprintf(`%s-\d{19}-\d{10}\.gz`, ingesterName)), boltdbFiles[0].Name())

	tsdbFiles, err := os.ReadDir(filepath.Join(cfg.FSConfig.Directory, "index", indexTables[1].Name()))
	require.NoError(t, err)
	require.Len(t, tsdbFiles, 1)
	require.Regexp(t, regexp.MustCompile(fmt.Sprintf(`\d{10}-%s-\d{19}\.tsdb\.gz`, ingesterName)), tsdbFiles[0].Name())

	store, err = NewStore(cfg, config.ChunkStoreConfig{}, schemaConfig, limits, cm, nil, util_log.Logger)
	require.NoError(t, err)

	defer store.Stop()

	// get all the chunks from both the stores
	chunks, _, err := store.GetChunkRefs(ctx, "fake", timeToModelTime(boltdbShipperStartDate), timeToModelTime(tsdbStartDate.Add(24*time.Hour)), newMatchers(fooLabelsWithName.String())...)
	require.NoError(t, err)
	var totalChunks int
	for _, chks := range chunks {
		totalChunks += len(chks)
	}
	// we get common chunk twice because it is indexed in both the stores
	require.Equal(t, totalChunks, len(addedChunkIDs)+1)

	// check whether we got back all the chunks which were added
	for i := range chunks {
		for _, c := range chunks[i] {
			_, ok := addedChunkIDs[schemaConfig.ExternalKey(c.ChunkRef)]
			require.True(t, ok)
		}
	}
}
