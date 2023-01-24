package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper"
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
	cm := storage.NewClientMetrics()
	defer cm.Unregister()
	if _, err := os.Stat("/tmp/benchmark/chunks"); os.IsNotExist(err) {
		if err := fillStore(cm); err != nil {
			log.Fatal("error filling up storage:", err)
		}
	}
}

func defaultLimitsTestConfig() validation.Limits {
	limits := validation.Limits{}
	flagext.DefaultValues(&limits)
	return limits
}

func getStore(cm storage.ClientMetrics) (storage.Store, error) {
	tempDir := "/tmp/benchmark"
	ingesterName := "ingester-1"

	// config for tsdb Shipper
	tsdbShipperConfig := indexshipper.Config{}
	flagext.DefaultValues(&tsdbShipperConfig)
	tsdbShipperConfig.ActiveIndexDirectory = filepath.Join(tempDir, "tsdb-index")
	tsdbShipperConfig.SharedStoreType = config.StorageTypeFileSystem
	tsdbShipperConfig.CacheLocation = filepath.Join(tempDir, "tsdb-shipper-cache")
	tsdbShipperConfig.Mode = indexshipper.ModeReadWrite
	tsdbShipperConfig.IngesterName = ingesterName

	storeConfig := storage.Config{
		FSConfig:          local.FSConfig{Directory: "/tmp/benchmark/chunks"},
		TSDBShipperConfig: tsdbShipperConfig,
	}

	schemaCfg := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:       config.DayTime{Time: start},
				IndexType:  "tsdb",
				ObjectType: "filesystem",
				Schema:     "v12",
				IndexTables: config.PeriodicTableConfig{
					Prefix: "index_",
					Period: time.Hour * 24,
				},
			},
		},
	}

	defaultLimits := defaultLimitsTestConfig()
	limits, err := validation.NewOverrides(defaultLimits, nil)
	if err != nil {
		return nil, err
	}

	return storage.NewStore(storeConfig, config.ChunkStoreConfig{}, schemaCfg, limits, cm, prometheus.DefaultRegisterer, util_log.Logger)
}

func fillStore(cm storage.ClientMetrics) error {
	store, err := getStore(cm)
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
			lbs, err := syntax.ParseLabels(fmt.Sprintf("{foo=\"bar\",level=\"%d\"}", j))
			if err != nil {
				panic(err)
			}
			labelsBuilder := labels.NewBuilder(lbs)
			labelsBuilder.Set(labels.MetricName, "logs")
			metric := labelsBuilder.Labels(nil)
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
