package storage

import (
	"context"
	"log"
	"runtime"
	"testing"
	"time"

	"net/http"
	_ "net/http/pprof"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	"github.com/cortexproject/cortex/pkg/chunk/storage"
	"github.com/cortexproject/cortex/pkg/util/validation"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/user"
)

var (
	start      = model.Time(1523750400000)
	m          runtime.MemStats
	ctx        = user.InjectOrgID(context.Background(), "fake")
	chunkStore = getStore()
)

//go test -bench=. -benchmem -memprofile memprofile.out -cpuprofile profile.out
func Benchmark_store_LazyQueryRegexBackward(b *testing.B) {
	benchmarkStoreQuery(b, &logproto.QueryRequest{
		Query:     "{foo=\"bar\"}",
		Regex:     "fuzz",
		Limit:     1000,
		Start:     time.Unix(0, start.UnixNano()),
		End:       time.Unix(0, (24*time.Hour.Nanoseconds())+start.UnixNano()),
		Direction: logproto.BACKWARD,
	})
}

func Benchmark_store_LazyQueryLogQLBackward(b *testing.B) {
	benchmarkStoreQuery(b, &logproto.QueryRequest{
		Query:     "{foo=\"bar\"} |= \"test\" != \"toto\"",
		Regex:     "fuzz",
		Limit:     1000,
		Start:     time.Unix(0, start.UnixNano()),
		End:       time.Unix(0, (24*time.Hour.Nanoseconds())+start.UnixNano()),
		Direction: logproto.BACKWARD,
	})
}

func Benchmark_store_LazyQueryRegexForward(b *testing.B) {
	benchmarkStoreQuery(b, &logproto.QueryRequest{
		Query:     "{foo=\"bar\"}",
		Regex:     "fuzz",
		Limit:     1000,
		Start:     time.Unix(0, start.UnixNano()),
		End:       time.Unix(0, (24*time.Hour.Nanoseconds())+start.UnixNano()),
		Direction: logproto.FORWARD,
	})
}

func Benchmark_store_LazyQueryForward(b *testing.B) {
	benchmarkStoreQuery(b, &logproto.QueryRequest{
		Query:     "{foo=\"bar\"}",
		Limit:     1000,
		Start:     time.Unix(0, start.UnixNano()),
		End:       time.Unix(0, (24*time.Hour.Nanoseconds())+start.UnixNano()),
		Direction: logproto.FORWARD,
	})
}

func Benchmark_store_LazyQueryBackward(b *testing.B) {
	benchmarkStoreQuery(b, &logproto.QueryRequest{
		Query:     "{foo=\"bar\"}",
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
		iter, err := chunkStore.LazyQuery(ctx, query)
		if err != nil {
			b.Fatal(err)
		}
		res := []logproto.Entry{}
		printHeap(b, false)
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

func getStore() Store {
	store, err := NewStore(storage.Config{
		BoltDBConfig: local.BoltDBConfig{Directory: "/tmp/benchmark/index"},
		FSConfig:     local.FSConfig{Directory: "/tmp/benchmark/chunks"},
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
