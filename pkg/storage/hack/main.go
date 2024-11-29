package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"sync"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/validation"
)

var (
	start     = model.Time(1523750400000)
	ctx       = user.InjectOrgID(context.Background(), "fake")
	maxChunks = 1200 // 1200 chunks is 2gib ish of data enough to run benchmark
)

// fill up the local filesystem store with 1gib of data to run benchmark
func main() {
	cm := storage.NewClientMetrics()
	defer cm.Unregister()
	if _, err := os.Stat("/tmp/benchmark/chunks"); os.IsNotExist(err) {
		if err := fillStore(cm); err != nil {
			log.Fatal("error filling up storage:", err)
		}
	}
}

func getStore(cm storage.ClientMetrics) (storage.Store, *config.SchemaConfig, error) {
	storeConfig := storage.Config{
		BoltDBConfig: local.BoltDBConfig{Directory: "/tmp/benchmark/index"},
		FSConfig:     local.FSConfig{Directory: "/tmp/benchmark/chunks"},
	}

	schemaCfg := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:       config.DayTime{Time: start},
				IndexType:  "boltdb",
				ObjectType: "filesystem",
				Schema:     "v13",
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: "index_",
						Period: time.Hour * 168,
					}},
			},
		},
	}

	store, err := storage.NewStore(storeConfig, config.ChunkStoreConfig{}, schemaCfg, &validation.Overrides{}, cm, prometheus.DefaultRegisterer, util_log.Logger, "cortex")
	return store, &schemaCfg, err
}

func fillStore(cm storage.ClientMetrics) error {
	store, schemacfg, err := getStore(cm)
	if err != nil {
		return err
	}
	defer store.Stop()

	periodcfg, err := schemacfg.SchemaForTime(start)
	if err != nil {
		return err
	}

	chunkfmt, headfmt, err := periodcfg.ChunkFormat()
	if err != nil {
		return err
	}

	var wgPush sync.WaitGroup
	var flushCount int
	// insert 5 streams with a random logs every nanoseconds
	// the string is randomize so chunks are big ~2mb
	// take ~1min to build 1gib of data
	for i := 0; i < 5; i++ {
		wgPush.Add(1)
		go func(j int) {
			defer wgPush.Done()
			lbs, err := syntax.ParseLabels(fmt.Sprintf("{foo=\"bar\",level=\"%d\"}", j))
			if err != nil {
				panic(err)
			}
			labelsBuilder := labels.NewBuilder(lbs)
			labelsBuilder.Set(labels.MetricName, "logs")
			metric := labelsBuilder.Labels()
			fp := client.Fingerprint(lbs)
			chunkEnc := chunkenc.NewMemChunk(chunkfmt, compression.LZ4_4M, headfmt, 262144, 1572864)
			for ts := start.UnixNano(); ts < start.UnixNano()+time.Hour.Nanoseconds(); ts = ts + time.Millisecond.Nanoseconds() {
				entry := &logproto.Entry{
					Timestamp: time.Unix(0, ts),
					Line:      randString(250),
				}
				if chunkEnc.SpaceFor(entry) {
					_, _ = chunkEnc.Append(entry)
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
					chunkEnc = chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, compression.LZ4_64k, chunkenc.UnorderedWithStructuredMetadataHeadBlockFmt, 262144, 1572864)
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
		b[i] = charset[rand.Intn(len(charset)-1)] //#nosec G404 -- Generation of test data does not require CSPRNG, this is not meant to be secret.
	}
	return string(b)
}

func randString(length int) string {
	return randStringWithCharset(length, charset)
}
