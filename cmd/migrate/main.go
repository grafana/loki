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
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/loki"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/validation"
)

func main() {

	var defaultsConfig loki.Config

	from := flag.String("from", "", "Start Time RFC339Nano 2006-01-02T15:04:05.999999999Z07:00")
	to := flag.String("to", "", "End Time RFC339Nano 2006-01-02T15:04:05.999999999Z07:00")
	sf := flag.String("source.config.file", "", "source datasource config")
	df := flag.String("dest.config.file", "", "dest datasource config")
	source := flag.String("source.tenant", "fake", "Source tenant identifier, default is `fake` for single tenant Loki")
	dest := flag.String("dest.tenant", "fake", "Destination tenant identifier, default is `fake` for single tenant Loki")
	match := flag.String("match", "", "Optional label match")

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

	matchers := []*labels.Matcher{nameLabelMatcher}

	if *match != "" {
		m, err := logql.ParseMatchers(*match)
		if err != nil {
			panic(err)
		}
		matchers = append(matchers, m...)
	}

	ctx := context.Background()
	ctx = user.InjectOrgID(ctx, *source)
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		panic(err)
	}

	f, t := util.RoundToMilliseconds(mustParse(*from), mustParse(*to))

	schemaGroups, fetchers, err := s.GetChunkRefs(ctx, userID, f, t, matchers...)

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
			earliestThrough := t
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

			output := make([]chunk.Chunk, 0, len(chks))

			// Calculate some size stats and change the tenant ID if necessary
			for i, chk := range chks {
				if enc, err := chk.Encoded(); err == nil {
					totalBytes += len(enc)
				} else {
					log.Fatalf("Error encoding a chunk: %v\n", err)
				}
				if *source != *dest {
					// Because the incoming chunks are already encoded, to change the username we have to make a new chunk
					nc := chunk.NewChunk(*dest, chk.Fingerprint, chk.Metric, chk.Data, chk.From, chk.Through)
					err := nc.Encode()
					if err != nil {
						log.Fatalf("Failed to encode new chunk with new user: %v\n", err)
					}
					output = append(output, nc)
				} else {
					output = append(output, chks[i])
				}

			}

			err = d.Put(ctx, output)
			if err != nil {
				log.Fatalf("Error sending chunks to new store: %v\n", err)
			}
			fmt.Printf("Batch sent successfully, if restarting after this batch you can safely start at time: %v\n", earliestThrough.Time())
		}
	}

	fmt.Printf("Finished transfering %v chunks totalling %v bytes in %v\n", totalChunks, totalBytes, time.Now().Sub(start))

}

func mustParse(t string) time.Time {

	ret, err := time.Parse(time.RFC3339Nano, t)

	if err != nil {
		log.Fatalf("Unable to parse time %v", err)
	}

	return ret
}
