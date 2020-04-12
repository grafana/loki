package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"github.com/prometheus/common/model"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
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

type syncRange struct {
	from int64
	to   int64
}

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
	shardBy := flag.Duration("shardBy", 6*time.Hour, "Break down the total interval into shards of this size, making this too small can lead to syncing a lot of duplicate chunks")
	parallel := flag.Int("parallel", 4, "How many parallel threads to process each shard")
	flag.Parse()

	if err := cfg.Unmarshal(&defaultsConfig, cfg.Defaults()); err != nil {
		log.Println("Failed parsing defaults config:", err)
		os.Exit(1)
	}

	sourceConfig := defaultsConfig
	destConfig := defaultsConfig

	if err := cfg.YAML(sf)(&sourceConfig); err != nil {
		log.Printf("Failed parsing source config file %v: %v\n", *sf, err)
		os.Exit(1)
	}
	if err := cfg.YAML(df)(&destConfig); err != nil {
		log.Printf("Failed parsing dest config file %v: %v\n", *df, err)
		os.Exit(1)
	}

	// The long nature of queries requires stretching out the cardinality limit some and removing the query length limit
	sourceConfig.LimitsConfig.CardinalityLimit = 1e9
	sourceConfig.LimitsConfig.MaxQueryLength = 0
	limits, err := validation.NewOverrides(sourceConfig.LimitsConfig, nil)
	s, err := storage.NewStore(sourceConfig.StorageConfig, sourceConfig.ChunkStoreConfig, sourceConfig.SchemaConfig, limits)
	if err != nil {
		log.Println("Failed to create source store:", err)
		os.Exit(1)
	}

	d, err := storage.NewStore(destConfig.StorageConfig, destConfig.ChunkStoreConfig, destConfig.SchemaConfig, limits)
	if err != nil {
		log.Println("Failed to create destination store:", err)
		os.Exit(1)
	}

	nameLabelMatcher, err := labels.NewMatcher(labels.MatchEqual, labels.MetricName, "logs")
	if err != nil {
		log.Println("Failed to create label matcher:", err)
		os.Exit(1)
	}

	matchers := []*labels.Matcher{nameLabelMatcher}

	if *match != "" {
		m, err := logql.ParseMatchers(*match)
		if err != nil {
			log.Println("Failed to parse log matcher:", err)
			os.Exit(1)
		}
		matchers = append(matchers, m...)
	}

	ctx := context.Background()
	ctx = user.InjectOrgID(ctx, *source)
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		panic(err)
	}
	parsedFrom := mustParse(*from)
	parsedTo := mustParse(*to)
	f, t := util.RoundToMilliseconds(parsedFrom, parsedTo)

	schemaGroups, fetchers, err := s.GetChunkRefs(ctx, userID, f, t, matchers...)
	if err != nil {
		log.Println("Error querying index for chunk refs:", err)
		os.Exit(1)
	}

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
		log.Println("Exiting")
		os.Exit(0)
	}
	start := time.Now()

	shardByNs := *shardBy
	syncRanges := calcSyncRanges(parsedFrom.UnixNano(), parsedTo.UnixNano(), shardByNs.Nanoseconds())
	log.Printf("With a shard duration of %v, %v ranges have been calculated.\n", shardByNs, len(syncRanges))

	cm := newChunkMover(ctx, s, d, *source, *dest, matchers, *batch)
	syncChan := make(chan *syncRange)
	errorChan := make(chan error)
	statsChan := make(chan stats)

	//Start the parallel processors
	var wg sync.WaitGroup
	cancelContext, cancelFunc := context.WithCancel(ctx)
	for i := 0; i < *parallel; i++ {
		wg.Add(1)
		go func(threadId int) {
			defer wg.Done()
			cm.moveChunks(threadId, cancelContext, syncChan, errorChan, statsChan)
		}(i)
	}

	// Launch a thread to dispatch requests:
	go func() {
		i := 0
		length := len(syncRanges)
		for i < length {
			log.Printf("Dispatching sync range %v of %v\n", i+1, length)
			syncChan <- syncRanges[i]
			i++
		}
		//Everything processed, exit
		cancelFunc()
	}()

	processedChunks := 0
	processedBytes := 0

	// Launch a thread to track stats
	go func() {
		for stat := range statsChan {
			processedChunks += stat.totalChunks
			processedBytes += stat.totalBytes
		}
		log.Printf("Transfering %v chunks totalling %v bytes in %v\n", processedChunks, processedBytes, time.Now().Sub(start))
		log.Println("Exiting stats thread")
	}()

	// Wait for an error or the context to be canceled
	select {
	case <-cancelContext.Done():
		log.Println("Received done call")
	case err := <-errorChan:
		log.Println("Received an error from processing thread, shutting down: ", err)
		cancelFunc()
	}
	log.Println("Waiting for threads to exit")
	wg.Wait()
	close(statsChan)
	log.Println("All threads finished")

	log.Println("Going to sleep....")
	for {
		time.Sleep(100 * time.Second)
	}

}

func calcSyncRanges(from, to int64, shardBy int64) []*syncRange {
	//Calculate the sync ranges
	syncRanges := []*syncRange{}
	//diff := to - from
	//shards := diff / shardBy
	currentFrom := from
	//currentTo := from
	currentTo := from + shardBy
	for currentFrom < to && currentTo <= to {
		s := &syncRange{
			from: currentFrom,
			to:   currentTo,
		}
		syncRanges = append(syncRanges, s)

		currentFrom = currentTo + 1
		currentTo = currentTo + shardBy

		if currentTo > to {
			currentTo = to
		}
	}
	return syncRanges
}

type stats struct {
	totalChunks int
	totalBytes  int
}

type chunkMover struct {
	ctx        context.Context
	source     storage.Store
	dest       storage.Store
	sourceUser string
	destUser   string
	matchers   []*labels.Matcher
	batch      int
}

func newChunkMover(ctx context.Context, source, dest storage.Store, sourceUser, destUser string, matchers []*labels.Matcher, batch int) *chunkMover {
	cm := &chunkMover{
		ctx:        ctx,
		source:     source,
		dest:       dest,
		sourceUser: sourceUser,
		destUser:   destUser,
		matchers:   matchers,
		batch:      batch,
	}
	return cm
}

func (m *chunkMover) moveChunks(threadId int, ctx context.Context, syncRangeCh <-chan *syncRange, errCh chan<- error, statsCh chan<- stats) {

	for {
		select {
		case <-ctx.Done():
			log.Println(threadId, "Requested to be done, context cancelled, quitting.")
			return
		case sr := <-syncRangeCh:
			start := time.Now()
			totalBytes := 0
			totalChunks := 0
			log.Println(threadId, "Processing", time.Unix(0, sr.from).UTC(), time.Unix(0, sr.to).UTC())
			schemaGroups, fetchers, err := m.source.GetChunkRefs(m.ctx, m.sourceUser, model.TimeFromUnixNano(sr.from), model.TimeFromUnixNano(sr.to), m.matchers...)
			if err != nil {
				log.Println(threadId, "Error querying index for chunk refs:", err)
				errCh <- err
				return
			}
			for i, f := range fetchers {
				log.Printf("%v Processing Schema %v which contains %v chunks\n", threadId, i, len(schemaGroups[i]))

				//Slice up into batches
				for j := 0; j < len(schemaGroups[i]); j += m.batch {
					k := j + m.batch
					if k > len(schemaGroups[i]) {
						k = len(schemaGroups[i])
					}

					chunks := schemaGroups[i][j:k]
					log.Printf("%v Processing chunks %v-%v of %v\n", threadId, j, k, len(schemaGroups[i]))

					keys := make([]string, 0, len(chunks))
					chks := make([]chunk.Chunk, 0, len(chunks))

					// FetchChunks requires chunks to be ordered by external key.
					sort.Slice(chunks, func(l, m int) bool { return chunks[l].ExternalKey() < chunks[m].ExternalKey() })
					for _, chk := range chunks {
						key := chk.ExternalKey()
						keys = append(keys, key)
						chks = append(chks, chk)
					}
					chks, err := f.FetchChunks(m.ctx, chks, keys)
					if err != nil {
						log.Println(threadId, "Error fetching chunks:", err)
						errCh <- err
						return
					}
					totalChunks += len(chks)

					output := make([]chunk.Chunk, 0, len(chks))

					// Calculate some size stats and change the tenant ID if necessary
					for i, chk := range chks {
						if enc, err := chk.Encoded(); err == nil {
							totalBytes += len(enc)
						} else {
							log.Println(threadId, "Error encoding a chunk:", err)
							errCh <- err
							return
						}
						if m.sourceUser != m.destUser {
							// Because the incoming chunks are already encoded, to change the username we have to make a new chunk
							nc := chunk.NewChunk(m.destUser, chk.Fingerprint, chk.Metric, chk.Data, chk.From, chk.Through)
							err := nc.Encode()
							if err != nil {
								log.Println(threadId, "Failed to encode new chunk with new user:", err)
								errCh <- err
								return
							}
							output = append(output, nc)
						} else {
							output = append(output, chks[i])
						}

					}

					err = m.dest.Put(m.ctx, output)
					if err != nil {
						log.Println(threadId, "Error sending chunks to new store:", err)
						errCh <- err
						return
					}
					log.Println(threadId, "Batch sent successfully")
				}
			}
			log.Printf("%v Finished processing sync range, %v chunks, %v bytes in %v seconds\n", threadId, totalChunks, totalBytes, time.Now().Sub(start).Seconds())
			statsCh <- stats{
				totalChunks: totalChunks,
				totalBytes:  totalBytes,
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
