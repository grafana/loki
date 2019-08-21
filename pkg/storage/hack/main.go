package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/weaveworks/common/user"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	"github.com/cortexproject/cortex/pkg/chunk/storage"
	"github.com/cortexproject/cortex/pkg/ingester/client"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	lstore "github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/validation"
)

var (
	start     = model.Time(1523750400000)
	ctx       = user.InjectOrgID(context.Background(), "fake")
	maxChunks = 600 // 600 chunks is 1.2bib of data enough to run benchmark
)

// fill up the local filesystem store with 1gib of data to run benchmark
func main() {
	if _, err := os.Stat("/tmp/benchmark/chunks"); os.IsNotExist(err) {
		if err := fillStore(); err != nil {
			log.Fatal("error filling up storage:", err)
		}
	}
}

func getStore() (lstore.Store, error) {
	store, err := lstore.NewStore(
		lstore.Config{
			Config: storage.Config{
				BoltDBConfig: local.BoltDBConfig{Directory: "/tmp/benchmark/index"},
				FSConfig:     local.FSConfig{Directory: "/tmp/benchmark/chunks"},
			},
		},
		chunk.StoreConfig{},
		chunk.SchemaConfig{
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
		},
		&validation.Overrides{},
	)
	if err != nil {
		return nil, err
	}
	return store, nil
}

func fillStore() error {

	store, err := getStore()
	if err != nil {
		return err
	}
	defer store.Stop()

	var wgPush sync.WaitGroup
	var flushCount int
	// insert 5 streams with a random logs every nanoseconds
	// the string is randomize so chunks are big ~2mb
	// take ~1min to build 1gib of data
	for i := 0; i < 5; i++ {
		wgPush.Add(1)
		go func(j int) {
			defer wgPush.Done()
			lbs, err := util.ToClientLabels(fmt.Sprintf("{foo=\"bar\",level=\"%d\"}", j))
			if err != nil {
				panic(err)
			}
			labelsBuilder := labels.NewBuilder(client.FromLabelAdaptersToLabels(lbs))
			labelsBuilder.Set(labels.MetricName, "logs")
			metric := labelsBuilder.Labels()
			fp := client.FastFingerprint(lbs)
			chunkEnc := chunkenc.NewMemChunkSize(chunkenc.EncGZIP, 262144)
			for ts := start.UnixNano(); ts < start.UnixNano()+time.Hour.Nanoseconds(); ts = ts + time.Millisecond.Nanoseconds() {
				entry := &logproto.Entry{
					Timestamp: time.Unix(0, ts),
					Line:      randString(250),
				}
				if chunkEnc.SpaceFor(entry) {
					_ = chunkEnc.Append(entry)
				} else {
					from, to := chunkEnc.Bounds()
					c := chunk.NewChunk("fake", fp, metric, chunkenc.NewFacade(chunkEnc), model.TimeFromUnixNano(from.UnixNano()), model.TimeFromUnixNano(to.UnixNano()))
					if err := c.Encode(); err != nil {
						panic(err)
					}
					err := store.Put(ctx, []chunk.Chunk{c})
					if err != nil {
						panic(err)
					}
					flushCount++
					log.Println("flushed ", flushCount, from.UnixNano(), to.UnixNano(), metric)
					if flushCount >= maxChunks {
						return
					}
					chunkEnc = chunkenc.NewMemChunkSize(chunkenc.EncGZIP, 262144)
				}
			}

		}(i)

	}
	wgPush.Wait()
	return nil
}

const charset = "abcdefghijklmnopqrstuvwxyz" +
	"ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func randStringWithCharset(length int, charset string) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset)-1)]
	}
	return string(b)
}

func randString(length int) string {
	return randStringWithCharset(length, charset)
}
