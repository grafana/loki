package builder

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/blockbuilder/types"
	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/partition"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	storagetypes "github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/util/flagext"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type Config struct {
	ConcurrentFlushes int `yaml:"concurrent_flushes"`
	ConcurrentWriters int `yaml:"concurrent_writers"`

	BlockSize       flagext.ByteSize  `yaml:"chunk_block_size"`
	TargetChunkSize flagext.ByteSize  `yaml:"chunk_target_size"`
	ChunkEncoding   string            `yaml:"chunk_encoding"`
	parsedEncoding  compression.Codec `yaml:"-"` // placeholder for validated encoding
	MaxChunkAge     time.Duration     `yaml:"max_chunk_age"`

	Backoff           backoff.Config `yaml:"backoff_config"`
	WorkerParallelism int            `yaml:"worker_parallelism"`
	SyncInterval      time.Duration  `yaml:"sync_interval"`

	SchedulerAddress string `yaml:"scheduler_address"`
	// SchedulerGRPCClientConfig configures the gRPC connection between the block-builder and its scheduler.
	SchedulerGRPCClientConfig grpcclient.Config `yaml:"scheduler_grpc_client_config"`
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.IntVar(&cfg.ConcurrentFlushes, prefix+"concurrent-flushes", 1, "How many flushes can happen concurrently")
	f.IntVar(&cfg.ConcurrentWriters, prefix+"concurrent-writers", 1, "How many workers to process writes, defaults to number of available cpus")
	_ = cfg.BlockSize.Set("256KB")
	f.Var(&cfg.BlockSize, prefix+"chunks-block-size", "The targeted _uncompressed_ size in bytes of a chunk block When this threshold is exceeded the head block will be cut and compressed inside the chunk.")
	_ = cfg.TargetChunkSize.Set(fmt.Sprint(onePointFiveMB))
	f.Var(&cfg.TargetChunkSize, prefix+"chunk-target-size", "A target _compressed_ size in bytes for chunks. This is a desired size not an exact size, chunks may be slightly bigger or significantly smaller if they get flushed for other reasons (e.g. chunk_idle_period). A value of 0 creates chunks with a fixed 10 blocks, a non zero value will create chunks with a variable number of blocks to meet the target size.")
	f.StringVar(&cfg.ChunkEncoding, prefix+"chunk-encoding", compression.Snappy.String(), fmt.Sprintf("The algorithm to use for compressing chunk. (%s)", compression.SupportedCodecs()))
	f.DurationVar(&cfg.MaxChunkAge, prefix+"max-chunk-age", 2*time.Hour, "The maximum duration of a timeseries chunk in memory. If a timeseries runs for longer than this, the current chunk will be flushed to the store and a new chunk created.")
	f.DurationVar(&cfg.SyncInterval, prefix+"sync-interval", 30*time.Second, "The interval at which to sync job status with the scheduler.")
	f.IntVar(&cfg.WorkerParallelism, prefix+"worker-parallelism", 1, "The number of workers to run in parallel to process jobs.")
	f.StringVar(&cfg.SchedulerAddress, prefix+"scheduler-address", "", "Address of the scheduler in the format described here: https://github.com/grpc/grpc/blob/master/doc/naming.md")

	cfg.SchedulerGRPCClientConfig.RegisterFlagsWithPrefix(prefix+"scheduler-grpc-client.", f)
	cfg.Backoff.RegisterFlagsWithPrefix(prefix+"backoff.", f)
}

// RegisterFlags registers flags.
func (cfg *Config) RegisterFlags(flags *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("blockbuilder.", flags)
}

func (cfg *Config) Validate() error {
	enc, err := compression.ParseCodec(cfg.ChunkEncoding)
	if err != nil {
		return err
	}
	cfg.parsedEncoding = enc

	if cfg.SyncInterval <= 0 {
		return errors.New("sync interval must be greater than 0")
	}

	if cfg.WorkerParallelism < 1 {
		return errors.New("worker parallelism must be greater than 0")
	}

	return nil
}

// BlockBuilder is a slimmed-down version of the ingester, intended to
// ingest logs without WALs. Broadly, it accumulates logs into per-tenant chunks in the same way the existing ingester does,
// without a WAL. Index (TSDB) creation is also not an out-of-band procedure and must be called directly. In essence, this
// allows us to buffer data, flushing chunks to storage as necessary, and then when ready to commit this, relevant TSDBs (one per period) are created and flushed to storage. This allows an external caller to prepare a batch of data, build relevant chunks+indices, ensure they're flushed, and then return. As long as chunk+index creation is deterministic, this operation is also
// idempotent, making retries simple and impossible to introduce duplicate data.
// It contains the following methods:
//   - `Append(context.Context, logproto.PushRequest) error`
//     Adds a push request to ingested data. May flush existing chunks when they're full/etc.
//   - `Commit(context.Context) error`
//     Serializes (cuts) any buffered data into chunks, flushes them to storage, then creates + flushes TSDB indices
//     containing all chunk references. Finally, clears internal state.
type BlockBuilder struct {
	services.Service
	types.BuilderTransport

	id              string
	cfg             Config
	periodicConfigs []config.PeriodConfig
	metrics         *builderMetrics
	logger          log.Logger

	decoder       *kafka.Decoder
	readerFactory func(partition int32) (partition.Reader, error)

	store    stores.ChunkWriter
	objStore *MultiStore

	jobsMtx      sync.RWMutex
	inflightJobs map[string]*types.Job
}

func NewBlockBuilder(
	id string,
	cfg Config,
	periodicConfigs []config.PeriodConfig,
	readerFactory func(partition int32) (partition.Reader, error),
	store stores.ChunkWriter,
	objStore *MultiStore,
	logger log.Logger,
	reg prometheus.Registerer,
) (*BlockBuilder,
	error) {
	decoder, err := kafka.NewDecoder()
	if err != nil {
		return nil, err
	}

	t, err := types.NewGRPCTransportFromAddress(cfg.SchedulerAddress, cfg.SchedulerGRPCClientConfig, reg)
	if err != nil {
		return nil, fmt.Errorf("create grpc transport: %w", err)
	}

	i := &BlockBuilder{
		id:               id,
		cfg:              cfg,
		periodicConfigs:  periodicConfigs,
		metrics:          newBuilderMetrics(reg),
		logger:           logger,
		decoder:          decoder,
		readerFactory:    readerFactory,
		store:            store,
		objStore:         objStore,
		BuilderTransport: t,
	}

	i.Service = services.NewBasicService(nil, i.running, nil)
	return i, nil
}

func (i *BlockBuilder) running(ctx context.Context) error {
	wg := sync.WaitGroup{}

	for j := 0; j < i.cfg.WorkerParallelism; j++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				default:
					err := i.runOne(ctx, id)
					if err != nil {
						level.Error(i.logger).Log("msg", "block builder run failed", "err", err)
					}
				}
			}
		}(fmt.Sprintf("worker-%d", j))
	}

	wg.Add(1)
	go func() {
		defer wg.Done()

		ticker := time.NewTicker(i.cfg.SyncInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := i.syncJobs(ctx); err != nil {
					level.Error(i.logger).Log("msg", "failed to sync jobs", "err", err)
				}
			}
		}
	}()

	wg.Wait()
	return nil
}

func (i *BlockBuilder) syncJobs(ctx context.Context) error {
	i.jobsMtx.RLock()
	defer i.jobsMtx.RUnlock()

	for _, job := range i.inflightJobs {
		if err := i.SendSyncJob(ctx, &types.SyncJobRequest{
			BuilderID: i.id,
			Job:       job,
		}); err != nil {
			level.Error(i.logger).Log("msg", "failed to sync job", "err", err)
		}
	}

	return nil
}

func (i *BlockBuilder) runOne(ctx context.Context, workerID string) error {
	// assuming GetJob blocks/polls until a job is available
	resp, err := i.SendGetJobRequest(ctx, &types.GetJobRequest{
		BuilderID: workerID,
	})
	if err != nil {
		return err
	}

	if !resp.OK {
		level.Info(i.logger).Log("msg", "no available job to process")
		return nil
	}

	job := resp.Job
	logger := log.With(
		i.logger,
		"worker_id", workerID,
		"partition", job.Partition,
		"job_min_offset", job.Offsets.Min,
		"job_max_offset", job.Offsets.Max,
	)

	i.jobsMtx.Lock()
	i.inflightJobs[job.ID] = job
	i.metrics.inflightJobs.Set(float64(len(i.inflightJobs)))
	i.jobsMtx.Unlock()

	lastConsumedOffset, err := i.processJob(ctx, job, logger)

	if _, err := withBackoff(
		ctx,
		i.cfg.Backoff,
		func() (res struct{}, err error) {
			if err = i.SendCompleteJob(ctx, &types.CompleteJobRequest{
				BuilderID:          workerID,
				Job:                job,
				LastConsumedOffset: lastConsumedOffset,
			}); err != nil {
				level.Error(i.logger).Log("msg", "failed to mark the job as complete", "err", err)
			}
			return
		},
	); err != nil {
		return err
	}

	i.jobsMtx.Lock()
	delete(i.inflightJobs, job.ID)
	i.metrics.inflightJobs.Set(float64(len(i.inflightJobs)))
	i.jobsMtx.Unlock()

	return err
}

func (i *BlockBuilder) processJob(ctx context.Context, job *types.Job, logger log.Logger) (lastOffsetConsumed int64, err error) {
	level.Debug(logger).Log("msg", "beginning job")

	indexer := newTsdbCreator()
	appender := newAppender(i.id,
		i.cfg,
		i.periodicConfigs,
		i.store,
		i.objStore,
		logger,
		i.metrics,
	)

	var lastOffset int64
	p := newPipeline(ctx)

	// Pipeline stage 1: Process the job offsets and write records to inputCh
	// This stage reads from the partition and feeds records into the input channel
	// When complete, it stores the last processed offset and closes the channel
	inputCh := make(chan []AppendInput)
	p.AddStageWithCleanup(
		"load records",
		1,
		func(ctx context.Context) error {
			lastOffset, err = i.loadRecords(ctx, int32(job.Partition), job.Offsets, inputCh)
			return err
		},
		func(ctx context.Context) error {
			level.Debug(logger).Log(
				"msg", "finished loading records",
				"ctx_error", ctx.Err(),
				"last_offset", lastOffset,
				"total_records", lastOffset-job.Offsets.Min,
			)
			close(inputCh)
			return nil
		},
	)

	// Stage 2: Process input records and generate chunks
	// This stage receives AppendInput batches, appends them to appropriate instances,
	// and forwards any cut chunks to the chunks channel for flushing.
	// ConcurrentWriters workers process inputs in parallel to maximize throughput.
	flush := make(chan *chunk.Chunk)
	p.AddStageWithCleanup(
		"appender",
		i.cfg.ConcurrentWriters,
		func(ctx context.Context) error {

			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case inputs, ok := <-inputCh:
					// inputs are finished; we're done
					if !ok {
						return nil
					}

					for _, input := range inputs {
						cut, err := appender.Append(ctx, input)
						if err != nil {
							level.Error(logger).Log("msg", "failed to append records", "err", err)
							return err
						}

						for _, chk := range cut {
							select {
							case <-ctx.Done():
								return ctx.Err()
							case flush <- chk:
							}
						}
					}
				}
			}
		},
		func(ctx context.Context) (err error) {
			defer func() {
				level.Debug(logger).Log(
					"msg", "finished appender",
					"err", err,
					"ctx_error", ctx.Err(),
				)
			}()
			defer close(flush)

			// once we're done appending, cut all remaining chunks.
			chks, err := appender.CutRemainingChunks(ctx)
			if err != nil {
				return err
			}

			for _, chk := range chks {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case flush <- chk:
				}
			}
			return nil
		},
	)

	// Stage 3: Flush chunks to storage
	// This stage receives chunks from the chunks channel and flushes them to storage
	// using ConcurrentFlushes workers for parallel processing
	p.AddStage(
		"flusher",
		i.cfg.ConcurrentFlushes,
		func(ctx context.Context) error {
			for {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case chk, ok := <-flush:
					if !ok {
						return nil
					}
					if _, err := withBackoff(
						ctx,
						i.cfg.Backoff, // retry forever
						func() (res struct{}, err error) {
							err = i.store.PutOne(ctx, chk.From, chk.Through, *chk)
							if err != nil {
								level.Error(logger).Log("msg", "failed to flush chunk", "err", err)
								i.metrics.chunksFlushFailures.Inc()
								return
							}
							appender.reportFlushedChunkStatistics(chk)

							// write flushed chunk to index
							approxKB := math.Round(float64(chk.Data.UncompressedSize()) / float64(1<<10))
							meta := index.ChunkMeta{
								Checksum: chk.ChunkRef.Checksum,
								MinTime:  int64(chk.ChunkRef.From),
								MaxTime:  int64(chk.ChunkRef.Through),
								KB:       uint32(approxKB),
								Entries:  uint32(chk.Data.Entries()),
							}
							err = indexer.Append(chk.UserID, chk.Metric, chk.ChunkRef.Fingerprint, index.ChunkMetas{meta})
							if err != nil {
								level.Error(logger).Log("msg", "failed to append chunk to index", "err", err)
							}

							return
						},
					); err != nil {
						return err
					}
				}
			}
		},
	)

	err = p.Run()
	level.Debug(logger).Log(
		"msg", "finished chunk creation",
		"err", err,
	)
	if err != nil {
		return 0, err
	}

	var (
		nodeName    = i.id
		tableRanges = config.GetIndexStoreTableRanges(storagetypes.TSDBType, i.periodicConfigs)
	)

	built, err := indexer.create(ctx, nodeName, tableRanges)
	if err != nil {
		level.Error(logger).Log("msg", "failed to build index", "err", err)
		return 0, err
	}

	u := newUploader(i.objStore)
	for _, db := range built {
		if _, err := withBackoff(ctx, i.cfg.Backoff, func() (res struct{}, err error) {
			err = u.Put(ctx, db)
			if err != nil {
				level.Error(util_log.Logger).Log(
					"msg", "failed to upload tsdb",
					"path", db.id.Path(),
				)
				return
			}

			level.Debug(logger).Log(
				"msg", "uploaded tsdb",
				"name", db.id.Name(),
			)
			return
		}); err != nil {
			return 0, err
		}
	}

	if lastOffset <= job.Offsets.Min {
		return lastOffset, nil
	}

	// log success
	level.Info(logger).Log(
		"msg", "successfully processed job",
		"last_offset", lastOffset,
	)

	return lastOffset, nil
}

func (i *BlockBuilder) loadRecords(ctx context.Context, partitionID int32, offsets types.Offsets, ch chan<- []AppendInput) (int64, error) {
	f, err := i.readerFactory(partitionID)
	if err != nil {
		return 0, err
	}

	f.SetOffsetForConsumption(offsets.Min)

	var (
		lastOffset = offsets.Min - 1
		boff       = backoff.New(ctx, i.cfg.Backoff)
	)

	for lastOffset < offsets.Max && boff.Ongoing() {
		var records []partition.Record
		records, err = f.Poll(ctx, int(offsets.Max-lastOffset))
		if err != nil {
			boff.Wait()
			continue
		}

		if len(records) == 0 {
			// No more records available
			break
		}

		// Reset backoff on successful poll
		boff.Reset()

		converted := make([]AppendInput, 0, len(records))
		for _, record := range records {
			if record.Offset >= offsets.Max {
				level.Debug(i.logger).Log("msg", "record offset exceeds job max offset. stop processing", "record offset", record.Offset, "max offset", offsets.Max)
				break
			}
			lastOffset = record.Offset

			stream, labels, err := i.decoder.Decode(record.Content)
			if err != nil {
				return 0, fmt.Errorf("failed to decode record: %w", err)
			}
			if len(stream.Entries) == 0 {
				continue
			}

			converted = append(converted, AppendInput{
				tenant:    record.TenantID,
				labels:    labels,
				labelsStr: stream.Labels,
				entries:   stream.Entries,
			})
		}

		if len(converted) > 0 {
			select {
			case ch <- converted:
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}
	}

	return lastOffset, err
}

func withBackoff[T any](
	ctx context.Context,
	config backoff.Config,
	fn func() (T, error),
) (T, error) {
	var zero T

	var boff = backoff.New(ctx, config)
	for boff.Ongoing() {
		res, err := fn()
		if err != nil {
			boff.Wait()
			continue
		}
		return res, nil
	}

	return zero, boff.ErrCause()
}
