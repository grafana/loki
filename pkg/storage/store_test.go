package storage

import (
	"context"
	"log"
	"runtime"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	"github.com/cortexproject/cortex/pkg/chunk/storage"
	"github.com/cortexproject/cortex/pkg/util/validation"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/user"
)

var (
	start = model.Time(1523750400000)
	m     runtime.MemStats
	ctx   = user.InjectOrgID(context.Background(), "fake")
)

//go test -bench=. -benchmem -memprofile memprofile.out -cpuprofile profile.out
func Benchmark_store_LazyQuery(b *testing.B) {

	for i := 0; i < b.N; i++ {
		store, err := getStore()
		if err != nil {
			b.Fatal(err)
		}
		iter, err := store.LazyQuery(ctx, &logproto.QueryRequest{
			Query:     "{foo=\"bar\"}",
			Regex:     "fuzz",
			Limit:     1000,
			Start:     time.Unix(0, start.UnixNano()),
			End:       time.Unix(0, (24*time.Hour.Nanoseconds())+start.UnixNano()),
			Direction: logproto.BACKWARD,
		})
		if err != nil {
			b.Fatal(err)
		}
		res := []logproto.Entry{}
		printHeap(b)
		j := 0
		for iter.Next() {
			j++
			printHeap(b)
			res = append(res, iter.Entry())
			// todo this should be done in the store.
			if j == 1000 {
				break
			}
		}
		iter.Close()
		printHeap(b)
		log.Println("line fetched", len(res))
		store.Stop()
	}
}

func printHeap(b *testing.B) {
	runtime.ReadMemStats(&m)
	log.Printf("Benchmark %d HeapInuse: %d Mbytes\n", b.N, m.HeapInuse/1024/1024)
}

func getStore() (Store, error) {
	store, err := NewStore(storage.Config{
		BoltDBConfig: local.BoltDBConfig{Directory: "/tmp/benchmark/index"},
		FSConfig:     local.FSConfig{Directory: "/tmp/benchmark/chunks"},
	}, chunk.StoreConfig{}, chunk.SchemaConfig{
		Configs: []chunk.PeriodConfig{
			chunk.PeriodConfig{
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
		return nil, err
	}
	return store, nil
}
