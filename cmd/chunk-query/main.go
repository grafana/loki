package main

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"github.com/grafana/dskit/tenant"
	"github.com/grafana/loki/pkg/util"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"sort"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/loki"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk"
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
	util_log.InitLogger(&sourceConfig.Server, prometheus.DefaultRegisterer)
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
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		panic(err)
	}
	parsedFrom := mustParse(*from)
	parsedTo := mustParse(*to)

	err = os.MkdirAll("chunks", os.ModePerm)
	if err != nil {
		panic(err)
	}

	f, t := util.RoundToMilliseconds(parsedFrom, parsedTo)
	schemaGroups, fetchers, err := s.GetChunkRefs(ctx, userID, f, t, matchers...)

	batch := 50

	for i, f := range fetchers {
		// Slice up into batches
		for j := 0; j < len(schemaGroups[i]); j += batch {
			k := j + batch
			if k > len(schemaGroups[i]) {
				k = len(schemaGroups[i])
			}

			chunks := schemaGroups[i][j:k]
			//log.Printf("%v Processing chunks %v-%v of %v\n", threadID, j, k, len(schemaGroups[i]))

			keys := make([]string, 0, len(chunks))
			chks := make([]chunk.Chunk, 0, len(chunks))

			// FetchChunks requires chunks to be ordered by external key.
			sort.Slice(chunks, func(x, y int) bool {
				return sourceConfig.SchemaConfig.ExternalKey(chunks[x].ChunkRef) < sourceConfig.SchemaConfig.ExternalKey(chunks[y].ChunkRef)
			})
			for _, chk := range chunks {
				key := sourceConfig.SchemaConfig.ExternalKey(chk.ChunkRef)
				keys = append(keys, key)
				chks = append(chks, chk)
			}
			for retry := 10; retry >= 0; retry-- {
				chks, err = f.FetchChunks(ctx, chks, keys)
				if err != nil {
					if retry == 0 {
						log.Println("Final error retrieving chunks, giving up:", err)
						return
					}
					log.Println("Error fetching chunks, will retry:", err)
					time.Sleep(5 * time.Second)
				} else {
					break
				}
			}
			for k := range chks {
				b, err := chks[k].Encoded()
				if err != nil {
					log.Printf("Error encoding chunk %v, it will be skipped, err: %v\n", keys[k], err)
					continue
				}
				err = os.WriteFile("chunks/"+base64.StdEncoding.EncodeToString([]byte(keys[k])), b, 0644)
				if err != nil {
					log.Printf("Error writing chunk to disk %v, it will be skipped, err: %v\n", keys[k], err)
					continue
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
