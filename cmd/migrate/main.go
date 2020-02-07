package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/cfg"
	"github.com/grafana/loki/pkg/loki"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/validation"
)

func main() {

	var defaultsConfig loki.Config

	sf := flag.String("source.config.file", "", "source datasource config")
	df := flag.String("dest.config.file", "", "dest datasource config")
	id := flag.String("tenant", "fake", "Tenant identifier, default is `fake` for single tenant Loki")
	batch := flag.Int("batchLen", 1000, "Specify how many chunks to read/write in one batch")
	flag.Parse()

	if err := cfg.Unmarshal(&defaultsConfig, cfg.Defaults()); err != nil {
		fmt.Fprintf(os.Stderr, "failed parsing defaults config: %v\n", err)
		os.Exit(1)
	}

	sourceConfig := defaultsConfig
	destConfig := defaultsConfig

	if err := cfg.YAML(sf)(&sourceConfig); err != nil {
		fmt.Fprintf(os.Stderr, "failed parsing source config file %v: %v\n", *sf, err)
		os.Exit(1)
	}
	if err := cfg.YAML(df)(&destConfig); err != nil {
		fmt.Fprintf(os.Stderr, "failed parsing dest config file %v: %v\n", *df, err)
		os.Exit(1)
	}

	//if sourceConfig.StorageConfig.IndexQueriesCacheConfig

	limits, err := validation.NewOverrides(sourceConfig.LimitsConfig, nil)
	s, err := storage.NewStore(sourceConfig.StorageConfig, sourceConfig.ChunkStoreConfig, sourceConfig.SchemaConfig, limits)
	if err != nil {
		panic(err)
	}

	d, err := storage.NewStore(destConfig.StorageConfig, destConfig.ChunkStoreConfig, destConfig.SchemaConfig, limits)
	if err != nil {
		panic(err)
	}

	nameLabelMatcher, err := labels.NewMatcher(labels.MatchEqual, labels.MetricName, "logs")
	if err != nil {
		panic(err)
	}
	ctx := context.Background()
	ctx = user.InjectOrgID(ctx, *id)
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		panic(err)
	}

	from, through := util.RoundToMilliseconds(time.Now().Add(-60*time.Minute), time.Now())

	schemaGroups, fetchers, err := s.GetChunkRefs(ctx, userID, from, through, nameLabelMatcher)

	var totalChunks int
	for i := range schemaGroups {
		totalChunks += len(schemaGroups[i])
	}
	rdr := bufio.NewReader(os.Stdin)
	fmt.Printf("Timespan will sync %v chunks spanning %v schemas.\n", totalChunks, len(fetchers))
	fmt.Print("Proceed? (Y/n):")
	in, err := rdr.ReadString('\n')
	if err != nil {
		log.Fatalf("Error reading input: %v", err)
	}
	if strings.ToLower(strings.TrimSpace(in)) == "n" {
		fmt.Println("Exiting")
		os.Exit(0)
	}
	totalBytes := 0
	start := time.Now()
	for i, f := range fetchers {
		fmt.Printf("Processing Schema %v which contains %v chunks\n", i, len(schemaGroups[i]))

		//Slice up into batches
		for j := 0; j < len(schemaGroups[i]); j += *batch {
			k := j + *batch
			if k > len(schemaGroups[i]) {
				k = len(schemaGroups[i])
			}

			chunks := schemaGroups[i][j:k]
			fmt.Printf("Processing chunks %v-%v of %v\n", j, k, len(schemaGroups[i]))

			keys := make([]string, 0, len(chunks))
			chks := make([]chunk.Chunk, 0, len(chunks))

			// For retry purposes, find the earliest "Through" and keep it,
			// if restarting you would want to set the "From" to this value
			// This might send some duplicate chunks but should ensure nothing is missed.
			earliestThrough := through
			for _, c := range chunks {
				if c.Through < earliestThrough {
					earliestThrough = c.Through
				}
			}

			// FetchChunks requires chunks to be ordered by external key.
			sort.Slice(chunks, func(l, m int) bool { return chunks[l].ExternalKey() < chunks[m].ExternalKey() })
			for _, chk := range chunks {
				key := chk.ExternalKey()
				keys = append(keys, key)
				chks = append(chks, chk)
			}
			chks, err := f.FetchChunks(ctx, chks, keys)
			if err != nil {
				log.Fatalf("Error fetching chunks: %v\n", err)
			}

			// Calculate some size stats
			for _, chk := range chks {
				if enc, err := chk.Encoded(); err == nil {
					totalBytes += len(enc)
				} else {
					log.Fatalf("Error encoding a chunk: %v\n", err)
				}
			}

			err = d.Put(ctx, chks)
			if err != nil {
				log.Fatalf("Error sending chunks to new store: %v\n", err)
			}
			fmt.Printf("Batch sent successfully, if restarting after this batch you can safely start at time: %v\n", earliestThrough.Time())
		}
	}

	fmt.Printf("Finished transfering %v chunks totalling %v bytes in %v\n", totalChunks, totalBytes, time.Now().Sub(start))

}
