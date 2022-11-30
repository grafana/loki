package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	gcs "cloud.google.com/go/storage"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/weaveworks/common/user"
	"google.golang.org/api/iterator"

	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/loki"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/util/cfg"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/validation"
)

func main() {
	var defaultsConfig loki.Config

	from := flag.String("from", "", "Start Time RFC339Nano 2006-01-02T15:04:05.999999999Z07:00")
	to := flag.String("to", "", "End Time RFC339Nano 2006-01-02T15:04:05.999999999Z07:00")
	sf := flag.String("source.config.file", "", "source datasource config")
	source := flag.String("source.tenant", "fake", "Source tenant identifier, default is `fake` for single tenant Loki")
	match := flag.String("match", "", "Optional label match")

	flag.Parse()

	go func() {
		log.Println(http.ListenAndServe("localhost:8080", nil))
	}()

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

	// This is a little brittle, if we add a new cache it may easily get missed here but it's important to disable
	// any of the chunk caches to save on memory because we write chunks to the cache when we call Put operations on the store.
	sourceConfig.ChunkStoreConfig.ChunkCacheConfig.EnableFifoCache = false
	sourceConfig.ChunkStoreConfig.ChunkCacheConfig.MemcacheClient = defaultsConfig.ChunkStoreConfig.ChunkCacheConfig.MemcacheClient
	sourceConfig.ChunkStoreConfig.ChunkCacheConfig.Redis = defaultsConfig.ChunkStoreConfig.ChunkCacheConfig.Redis
	sourceConfig.ChunkStoreConfig.WriteDedupeCacheConfig.EnableFifoCache = false
	sourceConfig.ChunkStoreConfig.WriteDedupeCacheConfig.MemcacheClient = defaultsConfig.ChunkStoreConfig.WriteDedupeCacheConfig.MemcacheClient
	sourceConfig.ChunkStoreConfig.WriteDedupeCacheConfig.Redis = defaultsConfig.ChunkStoreConfig.WriteDedupeCacheConfig.Redis

	// Don't keep fetched index files for very long
	sourceConfig.StorageConfig.BoltDBShipperConfig.CacheTTL = 30 * time.Minute

	// The long nature of queries requires stretching out the cardinality limit some and removing the query length limit
	sourceConfig.LimitsConfig.CardinalityLimit = 1e9
	sourceConfig.LimitsConfig.MaxQueryLength = 0
	limits, err := validation.NewOverrides(sourceConfig.LimitsConfig, nil)
	if err != nil {
		log.Println("Failed to create limit overrides:", err)
		os.Exit(1)
	}
	err = sourceConfig.Validate()
	if err != nil {
		log.Println("Failed to validate source store config:", err)
		os.Exit(1)
	}

	// Create a new registerer to avoid registering duplicate metrics
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	clientMetrics := storage.NewClientMetrics()
	s, err := storage.NewStore(sourceConfig.StorageConfig, sourceConfig.ChunkStoreConfig, sourceConfig.SchemaConfig, limits, clientMetrics, prometheus.DefaultRegisterer, util_log.Logger)
	if err != nil {
		log.Println("Failed to create source store:", err)
		os.Exit(1)
	}

	nameLabelMatcher, err := labels.NewMatcher(labels.MatchEqual, labels.MetricName, "logs")
	if err != nil {
		log.Println("Failed to create label matcher:", err)
		os.Exit(1)
	}

	matchers := []*labels.Matcher{nameLabelMatcher}

	if *match != "" {
		m, err := syntax.ParseMatchers(*match)
		if err != nil {
			log.Println("Failed to parse log matcher:", err)
			os.Exit(1)
		}
		matchers = append(matchers, m...)
	}

	ctx := context.Background()
	// This is a little weird but it was the easiest way to guarantee the userID is in the right format
	ctx = user.InjectOrgID(ctx, *source)

	parsedFrom := mustParse(*from)
	parsedTo := mustParse(*to)

	schemaGroups, fetchers, err := s.GetChunkRefs(ctx, *source, model.TimeFromUnixNano(parsedFrom.UnixNano()), model.TimeFromUnixNano(parsedTo.UnixNano()), matchers...)

	bucket := "prod-eu-west-0-loki-prod"
	for i := range fetchers {
		for _, c := range schemaGroups[i] {
			key := sourceConfig.SchemaConfig.ExternalKey(c.ChunkRef)
			log.Println(key)
			ctx := context.Background()
			client, err := gcs.NewClient(ctx)
			if err != nil {
				panic(err)
			}
			defer client.Close()

			ctx, cancel := context.WithTimeout(ctx, time.Second*10)
			defer cancel()

			it := client.Bucket(bucket).Objects(ctx, &gcs.Query{
				// Versions true to output all generations of objects
				Versions: true,
				Prefix:   key,
			})
			for {
				attrs, err := it.Next()
				if err == iterator.Done {
					break
				}
				if err != nil {
					panic(err)
					//return fmt.Errorf("Bucket(%q).Objects(): %v", bucket, err)
				}
				log.Println(attrs.Name, attrs.Generation, attrs.Metageneration)
				src := client.Bucket(bucket).Object(attrs.Name)
				dst := client.Bucket(bucket).Object(attrs.Name)
				dst = dst.If(gcs.Conditions{DoesNotExist: true})
				if _, err := dst.CopierFrom(src.Generation(attrs.Generation)).Run(ctx); err != nil {
					log.Printf("Error copying object: Object(%q).CopierFrom(%q).Generation(%v).Run: %v\n", attrs.Name, attrs.Name, attrs.Generation, err)
				} else {
					log.Printf("Generation %v of object %v in bucket %v was copied to %v\n", attrs.Generation, attrs.Name, bucket, attrs.Name)
				}
			}
		}
	}
}

func mustParse(t string) time.Time {
	ret, err := time.Parse(time.RFC3339Nano, t)
	if err != nil {
		log.Fatalf("Unable to parse time %v", err)
	}

	return ret
}
