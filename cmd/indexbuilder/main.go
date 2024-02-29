package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/loki"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/pkg/storage/stores/index"
	s_index "github.com/grafana/loki/pkg/storage/stores/series/index"
	"github.com/grafana/loki/pkg/util/cfg"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/validation"


)

var (
	BackendDelimeter = "/" // fairly safe to hard-code
)

func main() {
	var defaultsConfig loki.Config

	from := flag.String("from", "min", "Start Time RFC339Nano 2006-01-02T15:04:05.999999999Z07:00")
	to := flag.String("to", "max", "End Time RFC339Nano 2006-01-02T15:04:05.999999999Z07:00")
	sf := flag.String("source.config.file", "", "source datasource config")
	source := flag.String("source.tenant", "fake", "Source tenant identifier, default is `fake` for single tenant Loki")
	batch := flag.Int("batchLen", 500, "Specify how many chunks to read/write in one batch")

	flag.Parse()

	// Create a set of defaults
	if err := cfg.Unmarshal(&defaultsConfig, cfg.Defaults(flag.CommandLine)); err != nil {
		log.Println("Failed parsing defaults config:", err)
		os.Exit(1)
	}

	var sourceConfig loki.ConfigWrapper
	srcArgs := []string{"-config.file=" + *sf}
	if err := cfg.DynamicUnmarshal(&sourceConfig, srcArgs, flag.NewFlagSet("config-file-loader", flag.ContinueOnError)); err != nil {
		fmt.Fprintf(os.Stderr, "failed parsing config: %v\n", err)
		os.Exit(1)
	}

	sourceConfig.ChunkStoreConfig.ChunkCacheConfig.EmbeddedCache.Enabled = false
	sourceConfig.ChunkStoreConfig.ChunkCacheConfig.MemcacheClient = defaultsConfig.ChunkStoreConfig.ChunkCacheConfig.MemcacheClient
	sourceConfig.ChunkStoreConfig.ChunkCacheConfig.Redis = defaultsConfig.ChunkStoreConfig.ChunkCacheConfig.Redis
	sourceConfig.ChunkStoreConfig.WriteDedupeCacheConfig.MemcacheClient = defaultsConfig.ChunkStoreConfig.WriteDedupeCacheConfig.MemcacheClient
	sourceConfig.ChunkStoreConfig.WriteDedupeCacheConfig.Redis = defaultsConfig.ChunkStoreConfig.WriteDedupeCacheConfig.Redis

	// Don't want to use the index gateway for this, this makes sure the index files are properly uploaded when the store is stopped.
	sourceConfig.StorageConfig.BoltDBShipperConfig.IndexGatewayClientConfig.Disabled = true
	sourceConfig.StorageConfig.TSDBShipperConfig.IndexGatewayClientConfig.Disabled = true

	// The long nature of queries requires stretching out the cardinality limit some and removing the query length limit
	sourceConfig.LimitsConfig.CardinalityLimit = 1e9
	sourceConfig.LimitsConfig.MaxQueryLength = 0

	err := sourceConfig.Validate()
	if err != nil {
		log.Println("Failed to validate source store config:", err)
		os.Exit(1)
	}

	// Create a new registerer to avoid registering duplicate metrics
	//prometheus.DefaultRegisterer = prometheus.NewRegistry()
	clientMetrics := storage.NewClientMetrics()

	// Create a new registerer to avoid registering duplicate metrics
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	limits, err := validation.NewOverrides(sourceConfig.LimitsConfig, nil)

	ctx := context.Background()
	// This is a little weird but it was the easiest way to guarantee the userID is in the right format
	ctx = user.InjectOrgID(ctx, *source)

	parsedFrom := mustParse(*from)
	parsedTo := mustParse(*to)

	now := config.NewDayTime(model.Now())

	for _, pconfig := range sourceConfig.SchemaConfig.Configs {
		chunks, err := storage.NewObjectClient(pconfig.ObjectType, sourceConfig.StorageConfig, clientMetrics)
		if err != nil {
			log.Println("Error creating object client", err)
			os.Exit(1)
		}

		schema, err := s_index.CreateSchema(pconfig)
		if err != nil {
			log.Println("Error creating series store schema", err)
			os.Exit(1)
		}

		// This could be so easy, if the factory supported tsdb, but see below
		//strategy := indexgateway.NewNoopStrategy()

		//client, err := storage.NewIndexClient(pconfig, pconfig.GetIndexTableNumberRange(now), sourceConfig.StorageConfig, sourceConfig.SchemaConfig, limits, clientMetrics, strategy, prometheus.DefaultRegisterer, util_log.Logger, *metricsNamespace)
		//if err != nil {
		//	log.Println("Error creating index client", err)
		//	os.Exit(1)
		//}

		name := fmt.Sprintf("%s_%s", pconfig.ObjectType, pconfig.From.String())
		if pconfig.ObjectType == "tsdb" {
			indexReaderWriter, stopFunction, err := tsdb.NewStore(name, pconfig.IndexTables.PathPrefix, sourceConfig.StorageConfig.TSDBShipperConfig, sourceConfig.SchemaConfig, nil, chunks, limits, pconfig.GetIndexTableNumberRange(now), prometheus.DefaultRegisterer, util_log.Logger)
			if err != nil {
				log.Println("Error creating tsdb store", err)
			}
			ci := newChunkIndexer(ctx, chunks, *source, schema, indexReaderWriter, *batch)

			ci.from = parsedFrom
			ci.through = parsedTo

			ci.indexChunks(ctx)

			stopFunction()
		} else {
			log.Println("Error, indexbuilder only supports tsdb")
			os.Exit(1)
		}
	}

}

type stats struct {
	totalChunks uint64
	totalBytes  uint64
}

type chunkIndexer struct {
	ctx        context.Context
	source     client.ObjectClient
	sourceUser string
	schema	   s_index.SeriesStoreSchema
	writer     index.Writer
	batch      int
	from       model.Time
	through    model.Time
}

func newChunkIndexer(ctx context.Context, source client.ObjectClient, sourceUser string, schema s_index.SeriesStoreSchema, writer index.Writer, batch int) *chunkIndexer {
	cm := &chunkIndexer{
		ctx:        ctx,
		source:     source,
		sourceUser: sourceUser,
		schema:		schema,
		writer:		writer,
		batch:      batch,
		from:       0,
		through:    math.MaxInt64,
	}
	return cm
}

func (m *chunkIndexer) indexChunks(ctx context.Context) {
	objs, _, err := m.source.List(m.ctx, m.sourceUser, "") // ([]StorageObject, []StorageCommonPrefix, error)
	if err != nil {
		log.Println("Error fetching chunks:", err)
		return
	}
	for i, o := range objs {
		log.Printf("Processing %vth object: %v", i, o.Key)
		chunk := m.loadChunkFromStorageObject(o)
		if chunk.From > m.from && chunk.Through < m.through {
			m.insertIntoIndex(chunk)
		}
	}
}

func (m *chunkIndexer) loadChunkFromStorageObject(obj client.StorageObject) chunk.Chunk {
	comps := strings.Split(obj.Key, BackendDelimeter)

	chk, err := chunk.ParseExternalKey(comps[0], obj.Key)
	if err != nil {
		log.Fatalf("Unable to parse external key %v", err)
	}

	ctx := chunk.NewDecodeContext()

	readcloser, readBytes, err := m.source.GetObject(context.Background(), obj.Key)
	if err != nil {
		log.Fatalf("Unable to get object %v", err)
	}

	log.Printf("loaded %v bytes", readBytes)

	bytes, err := io.ReadAll(readcloser)
	if err != nil {
		log.Fatalf("Unable to read object %v", err)
	}

	err = chk.Decode(ctx, bytes)
	if err != nil {
		log.Fatalf("Unable to decode chunk %v", err)
	}

	return chk
}

func (m *chunkIndexer) insertIntoIndex(chunk chunk.Chunk) {
	m.writer.IndexChunk(context.Background(), chunk.From, chunk.Through, chunk)
}

func mustParse(t string) model.Time {
	if t == "min" {
		return model.Earliest
	} else if t == "max" {
		return model.Latest
	} else {
		t, err := time.Parse(time.RFC3339Nano, t)
		if err != nil {
			log.Fatalf("Unable to parse time %v", err)
		}
		return model.TimeFromUnixNano(t.UnixNano())
	}
}
