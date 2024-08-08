package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof"
	"os"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/loki"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper"
	"github.com/grafana/loki/v3/pkg/util/cfg"
	"github.com/grafana/loki/v3/pkg/util/constants"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/validation"
)

type syncRange struct {
	number int
	from   int64
	to     int64
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

	batch := flag.Int("batchLen", 500, "Specify how many chunks to read/write in one batch")
	shardBy := flag.Duration("shardBy", 6*time.Hour, "Break down the total interval into shards of this size, making this too small can lead to syncing a lot of duplicate chunks")
	parallel := flag.Int("parallel", 8, "How many parallel threads to process each shard")
	metricsNamespace := flag.String("metrics.namespace", constants.Loki, "Namespace of the generated metrics")
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

	var destConfig loki.ConfigWrapper
	destArgs := []string{"-config.file=" + *df}
	if err := cfg.DynamicUnmarshal(&destConfig, destArgs, flag.NewFlagSet("config-file-loader", flag.ContinueOnError)); err != nil {
		fmt.Fprintf(os.Stderr, "failed parsing config: %v\n", err)
		os.Exit(1)
	}

	// This is a little brittle, if we add a new cache it may easily get missed here but it's important to disable
	// any of the chunk caches to save on memory because we write chunks to the cache when we call Put operations on the store.
	sourceConfig.ChunkStoreConfig.ChunkCacheConfig.EmbeddedCache.Enabled = false
	sourceConfig.ChunkStoreConfig.ChunkCacheConfig.MemcacheClient = defaultsConfig.ChunkStoreConfig.ChunkCacheConfig.MemcacheClient
	sourceConfig.ChunkStoreConfig.ChunkCacheConfig.Redis = defaultsConfig.ChunkStoreConfig.ChunkCacheConfig.Redis
	sourceConfig.ChunkStoreConfig.WriteDedupeCacheConfig.MemcacheClient = defaultsConfig.ChunkStoreConfig.WriteDedupeCacheConfig.MemcacheClient
	sourceConfig.ChunkStoreConfig.WriteDedupeCacheConfig.Redis = defaultsConfig.ChunkStoreConfig.WriteDedupeCacheConfig.Redis

	destConfig.ChunkStoreConfig.ChunkCacheConfig.EmbeddedCache.Enabled = false
	destConfig.ChunkStoreConfig.ChunkCacheConfig.MemcacheClient = defaultsConfig.ChunkStoreConfig.ChunkCacheConfig.MemcacheClient
	destConfig.ChunkStoreConfig.ChunkCacheConfig.Redis = defaultsConfig.ChunkStoreConfig.ChunkCacheConfig.Redis
	destConfig.ChunkStoreConfig.WriteDedupeCacheConfig.MemcacheClient = defaultsConfig.ChunkStoreConfig.WriteDedupeCacheConfig.MemcacheClient
	destConfig.ChunkStoreConfig.WriteDedupeCacheConfig.Redis = defaultsConfig.ChunkStoreConfig.WriteDedupeCacheConfig.Redis

	// Don't keep fetched index files for very long
	sourceConfig.StorageConfig.BoltDBShipperConfig.CacheTTL = 30 * time.Minute

	sourceConfig.StorageConfig.BoltDBShipperConfig.Mode = indexshipper.ModeReadOnly
	sourceConfig.StorageConfig.TSDBShipperConfig.Mode = indexshipper.ModeReadOnly

	// Shorten these timers up so we resync a little faster and clear index files a little quicker
	destConfig.StorageConfig.IndexCacheValidity = 1 * time.Minute
	destConfig.StorageConfig.BoltDBShipperConfig.ResyncInterval = 1 * time.Minute
	destConfig.StorageConfig.TSDBShipperConfig.ResyncInterval = 1 * time.Minute

	// Don't want to use the index gateway for this, this makes sure the index files are properly uploaded when the store is stopped.
	sourceConfig.StorageConfig.BoltDBShipperConfig.IndexGatewayClientConfig.Disabled = true
	sourceConfig.StorageConfig.TSDBShipperConfig.IndexGatewayClientConfig.Disabled = true

	destConfig.StorageConfig.BoltDBShipperConfig.IndexGatewayClientConfig.Disabled = true
	destConfig.StorageConfig.TSDBShipperConfig.IndexGatewayClientConfig.Disabled = true

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
	err = destConfig.Validate()
	if err != nil {
		log.Println("Failed to validate dest store config:", err)
		os.Exit(1)
	}
	// Create a new registerer to avoid registering duplicate metrics
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	clientMetrics := storage.NewClientMetrics()
	s, err := storage.NewStore(sourceConfig.StorageConfig, sourceConfig.ChunkStoreConfig, sourceConfig.SchemaConfig, limits, clientMetrics, prometheus.DefaultRegisterer, util_log.Logger, *metricsNamespace)
	if err != nil {
		log.Println("Failed to create source store:", err)
		os.Exit(1)
	}

	// Create a new registerer to avoid registering duplicate metrics
	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	d, err := storage.NewStore(destConfig.StorageConfig, destConfig.ChunkStoreConfig, destConfig.SchemaConfig, limits, clientMetrics, prometheus.DefaultRegisterer, util_log.Logger, *metricsNamespace)
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
		m, err := syntax.ParseMatchers(*match, true)
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

	start := time.Now()

	shardByNs := *shardBy
	syncRanges := calcSyncRanges(parsedFrom.UnixNano(), parsedTo.UnixNano(), shardByNs.Nanoseconds())
	log.Printf("With a shard duration of %v, %v ranges have been calculated.\n", shardByNs, len(syncRanges)-1)

	// Pass dest schema config, the destination determines the new chunk external keys using potentially a different schema config.
	cm := newChunkMover(ctx, destConfig.SchemaConfig, s, d, *source, *dest, matchers, *batch, len(syncRanges)-1)
	syncChan := make(chan *syncRange)
	errorChan := make(chan error)
	statsChan := make(chan stats)

	// Start the parallel processors
	var wg sync.WaitGroup
	cancelContext, cancelFunc := context.WithCancel(ctx)
	for i := 0; i < *parallel; i++ {
		wg.Add(1)
		go func(threadId int) {
			defer wg.Done()
			cm.moveChunks(cancelContext, threadId, syncChan, errorChan, statsChan)
		}(i)
	}

	// Launch a thread to dispatch requests:
	go func() {
		i := 0
		length := len(syncRanges)
		for i < length {
			//log.Printf("Dispatching sync range %v of %v\n", i+1, length)
			syncChan <- syncRanges[i]
			i++
		}
		// Everything processed, exit
		cancelFunc()
	}()

	var processedChunks uint64
	var processedBytes uint64

	// Launch a thread to track stats
	go func() {
		for stat := range statsChan {
			processedChunks += stat.totalChunks
			processedBytes += stat.totalBytes
		}
		log.Printf("Transferring %v chunks totalling %s in %v for an average throughput of %s/second\n", processedChunks, ByteCountDecimal(processedBytes), time.Since(start), ByteCountDecimal(uint64(float64(processedBytes)/time.Since(start).Seconds())))
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
	log.Println("All threads finished, stopping destination store (uploading index files for boltdb-shipper)")

	// For boltdb shipper this is important as it will upload all the index files.
	d.Stop()

	log.Println("Going to sleep....")
	for {
		time.Sleep(100 * time.Second)
	}
}

func calcSyncRanges(from, to int64, shardBy int64) []*syncRange {
	// Calculate the sync ranges
	syncRanges := []*syncRange{}
	// diff := to - from
	// shards := diff / shardBy
	currentFrom := from
	// currentTo := from
	currentTo := from + shardBy
	number := 0
	for currentFrom < to && currentTo <= to {
		s := &syncRange{
			number: number,
			from:   currentFrom,
			to:     currentTo,
		}
		syncRanges = append(syncRanges, s)
		number++

		currentFrom = currentTo + 1
		currentTo = currentTo + shardBy

		if currentTo > to {
			currentTo = to
		}
	}
	return syncRanges
}

type stats struct {
	totalChunks uint64
	totalBytes  uint64
}

type chunkMover struct {
	ctx        context.Context
	schema     config.SchemaConfig
	source     storage.Store
	dest       storage.Store
	sourceUser string
	destUser   string
	matchers   []*labels.Matcher
	batch      int
	syncRanges int
}

func newChunkMover(ctx context.Context, s config.SchemaConfig, source, dest storage.Store, sourceUser, destUser string, matchers []*labels.Matcher, batch int, syncRanges int) *chunkMover {
	cm := &chunkMover{
		ctx:        ctx,
		schema:     s,
		source:     source,
		dest:       dest,
		sourceUser: sourceUser,
		destUser:   destUser,
		matchers:   matchers,
		batch:      batch,
		syncRanges: syncRanges,
	}
	return cm
}

func (m *chunkMover) moveChunks(ctx context.Context, threadID int, syncRangeCh <-chan *syncRange, errCh chan<- error, statsCh chan<- stats) {
	for {
		select {
		case <-ctx.Done():
			log.Println(threadID, "Requested to be done, context cancelled, quitting.")
			return
		case sr := <-syncRangeCh:
			start := time.Now()
			var totalBytes uint64
			var totalChunks uint64
			schemaGroups, fetchers, err := m.source.GetChunks(m.ctx, m.sourceUser, model.TimeFromUnixNano(sr.from), model.TimeFromUnixNano(sr.to), chunk.NewPredicate(m.matchers, nil), nil)
			if err != nil {
				log.Println(threadID, "Error querying index for chunk refs:", err)
				errCh <- err
				return
			}
			for i, f := range fetchers {
				//log.Printf("%v Processing Schema %v which contains %v chunks\n", threadID, i, len(schemaGroups[i]))

				// Slice up into batches
				for j := 0; j < len(schemaGroups[i]); j += m.batch {
					k := j + m.batch
					if k > len(schemaGroups[i]) {
						k = len(schemaGroups[i])
					}

					chunks := schemaGroups[i][j:k]
					//log.Printf("%v Processing chunks %v-%v of %v\n", threadID, j, k, len(schemaGroups[i]))

					chks := make([]chunk.Chunk, 0, len(chunks))

					chks = append(chks, chunks...)

					finalChks, err := f.FetchChunks(m.ctx, chks)
					if err != nil {
						log.Println(threadID, "Error retrieving chunks, will go through them one by one:", err)
						finalChks = make([]chunk.Chunk, 0, len(chunks))
						for i := range chks {
							onechunk := []chunk.Chunk{chunks[i]}
							var retry int
							for retry = 4; retry >= 0; retry-- {
								onechunk, err = f.FetchChunks(m.ctx, onechunk)
								if err != nil {
									if retry == 0 {
										log.Println(threadID, "Final error retrieving chunks, giving up:", err)
									}
									log.Println(threadID, "Error fetching chunks, will retry:", err)
									onechunk = []chunk.Chunk{chunks[i]}
									time.Sleep(5 * time.Second)
								} else {
									break
								}
							}

							if retry < 0 {
								continue
							}

							finalChks = append(finalChks, onechunk[0])
						}
					}

					totalChunks += uint64(len(finalChks))

					output := make([]chunk.Chunk, 0, len(finalChks))

					// Calculate some size stats and change the tenant ID if necessary
					for i, chk := range finalChks {
						if enc, err := chk.Encoded(); err == nil {
							totalBytes += uint64(len(enc))
						} else {
							log.Println(threadID, "Error encoding a chunk:", err)
							errCh <- err
							return
						}
						if m.sourceUser != m.destUser {
							// Because the incoming chunks are already encoded, to change the username we have to make a new chunk
							nc := chunk.NewChunk(m.destUser, chk.FingerprintModel(), chk.Metric, chk.Data, chk.From, chk.Through)
							err := nc.Encode()
							if err != nil {
								log.Println(threadID, "Failed to encode new chunk with new user:", err)
								errCh <- err
								return
							}
							output = append(output, nc)
						} else {
							output = append(output, finalChks[i])
						}

					}
					for retry := 4; retry >= 0; retry-- {
						err = m.dest.Put(m.ctx, output)
						if err != nil {
							if retry == 0 {
								log.Println(threadID, "Final error sending chunks to new store, giving up:", err)
								errCh <- err
								return
							}
							log.Println(threadID, "Error sending chunks to new store, will retry:", err)
						} else {
							break
						}
					}
					//log.Println(threadID, "Batch sent successfully")
				}
			}
			log.Printf("%d Finished processing sync range %d of %d - Start: %v, End: %v, %v chunks, %s in %.1f seconds %s/second\n", threadID, sr.number, m.syncRanges, time.Unix(0, sr.from).UTC(), time.Unix(0, sr.to).UTC(), totalChunks, ByteCountDecimal(totalBytes), time.Since(start).Seconds(), ByteCountDecimal(uint64(float64(totalBytes)/time.Since(start).Seconds())))
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

func ByteCountDecimal(b uint64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "kMGTPE"[exp])
}
