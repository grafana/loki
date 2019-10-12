package storage

import (
	"context"
	"log"
	"net/http"
	_ "net/http/pprof"
	"runtime"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/user"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	"github.com/cortexproject/cortex/pkg/chunk/storage"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/util/validation"
)

var (
	start      = model.Time(1523750400000)
	m          runtime.MemStats
	ctx        = user.InjectOrgID(context.Background(), "fake")
	chunkStore = getLocalStore()
)

//go test -bench=. -benchmem -memprofile memprofile.out -cpuprofile profile.out
func Benchmark_store_LazyQueryRegexBackward(b *testing.B) {
	benchmarkStoreQuery(b, &logproto.QueryRequest{
		Selector:  `{foo="bar"} |= "fuzz"`,
		Limit:     1000,
		Start:     time.Unix(0, start.UnixNano()),
		End:       time.Unix(0, (24*time.Hour.Nanoseconds())+start.UnixNano()),
		Direction: logproto.BACKWARD,
	})
}

func Benchmark_store_LazyQueryLogQLBackward(b *testing.B) {
	benchmarkStoreQuery(b, &logproto.QueryRequest{
		Selector:  `{foo="bar"} |= "test" != "toto" |= "fuzz"`,
		Limit:     1000,
		Start:     time.Unix(0, start.UnixNano()),
		End:       time.Unix(0, (24*time.Hour.Nanoseconds())+start.UnixNano()),
		Direction: logproto.BACKWARD,
	})
}

func Benchmark_store_LazyQueryRegexForward(b *testing.B) {
	benchmarkStoreQuery(b, &logproto.QueryRequest{
		Selector:  `{foo="bar"} |= "fuzz"`,
		Limit:     1000,
		Start:     time.Unix(0, start.UnixNano()),
		End:       time.Unix(0, (24*time.Hour.Nanoseconds())+start.UnixNano()),
		Direction: logproto.FORWARD,
	})
}

func Benchmark_store_LazyQueryForward(b *testing.B) {
	benchmarkStoreQuery(b, &logproto.QueryRequest{
		Selector:  `{foo="bar"}`,
		Limit:     1000,
		Start:     time.Unix(0, start.UnixNano()),
		End:       time.Unix(0, (24*time.Hour.Nanoseconds())+start.UnixNano()),
		Direction: logproto.FORWARD,
	})
}

func Benchmark_store_LazyQueryBackward(b *testing.B) {
	benchmarkStoreQuery(b, &logproto.QueryRequest{
		Selector:  `{foo="bar"}`,
		Limit:     1000,
		Start:     time.Unix(0, start.UnixNano()),
		End:       time.Unix(0, (24*time.Hour.Nanoseconds())+start.UnixNano()),
		Direction: logproto.BACKWARD,
	})
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
		iter, err := chunkStore.LazyQuery(ctx, logql.SelectParams{QueryRequest: query})
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
	store, err := NewStore(Config{
		Config: storage.Config{
			BoltDBConfig: local.BoltDBConfig{Directory: "/tmp/benchmark/index"},
			FSConfig:     local.FSConfig{Directory: "/tmp/benchmark/chunks"},
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
	}, &validation.Overrides{})
	if err != nil {
		panic(err)
	}
	return store
}

func Test_store_LazyQuery(t *testing.T) {

	tests := []struct {
		name     string
		req      *logproto.QueryRequest
		expected []*logproto.Stream
	}{
		{
			"all",
			newQuery("{foo=~\"ba.*\"}", from, from.Add(6*time.Millisecond), logproto.FORWARD),
			[]*logproto.Stream{
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
			newQuery("{foo=~\"ba.*\"} |~ \"1|2|3\" !~ \"2|3\"", from, from.Add(6*time.Millisecond), logproto.FORWARD),
			[]*logproto.Stream{
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
			newQuery("{foo=\"bar\"}", from, from.Add(6*time.Millisecond), logproto.FORWARD),
			[]*logproto.Stream{
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
			newQuery("{foo=~\"ba.*\"}", from, from.Add(time.Millisecond), logproto.FORWARD),
			[]*logproto.Stream{
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
			it, err := s.LazyQuery(ctx, logql.SelectParams{QueryRequest: tt.req})
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
