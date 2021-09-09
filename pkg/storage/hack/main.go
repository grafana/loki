package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	lstore "github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/local"
	"github.com/grafana/loki/pkg/storage/chunk/storage"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/validation"
)

var (
	start     = model.Time(1523750400000)
	ctx       = user.InjectOrgID(context.Background(), "fake")
	maxChunks = 1200 // 1200 chunks is 2gib ish of data enough to run benchmark
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
	storeConfig := lstore.Config{
		Config: storage.Config{
			BoltDBConfig: local.BoltDBConfig{Directory: "/tmp/benchmark/index"},
			FSConfig:     local.FSConfig{Directory: "/tmp/benchmark/chunks"},
		},
	}

	schemaCfg := lstore.SchemaConfig{
		SchemaConfig: chunk.SchemaConfig{
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
	}

	chunkStore, err := storage.NewStore(
		storeConfig.Config,
		chunk.StoreConfig{},
		schemaCfg.SchemaConfig,
		&validation.Overrides{},
		prometheus.DefaultRegisterer,
		nil,
		util_log.Logger,
	)
	if err != nil {
		return nil, err
	}

	return lstore.NewStore(storeConfig, schemaCfg, chunkStore, prometheus.DefaultRegisterer)
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
			lbs, err := logql.ParseLabels(fmt.Sprintf("{foo=\"bar\",level=\"%d\"}", j))
			if err != nil {
				panic(err)
			}
			labelsBuilder := labels.NewBuilder(lbs)
			labelsBuilder.Set(labels.MetricName, "logs")
			metric := labelsBuilder.Labels()
			fp := client.Fingerprint(lbs)
			chunkEnc := chunkenc.NewMemChunk(chunkenc.EncLZ4_4M, chunkenc.UnorderedHeadBlockFmt, 262144, 1572864)
			for ts := start.UnixNano(); ts < start.UnixNano()+time.Hour.Nanoseconds(); ts = ts + time.Millisecond.Nanoseconds() {
				entry := &logproto.Entry{
					Timestamp: time.Unix(0, ts),
					Line:      randString(250),
				}
				if chunkEnc.SpaceFor(entry) {
					_ = chunkEnc.Append(entry)
				} else {
					from, to := chunkEnc.Bounds()
					c := chunk.NewChunk("fake", fp, metric, chunkenc.NewFacade(chunkEnc, 0, 0), model.TimeFromUnixNano(from.UnixNano()), model.TimeFromUnixNano(to.UnixNano()))
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
					chunkEnc = chunkenc.NewMemChunk(chunkenc.EncLZ4_64k, chunkenc.UnorderedHeadBlockFmt, 262144, 1572864)
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
