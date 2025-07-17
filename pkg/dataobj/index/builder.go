package index

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/multierror"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/kafka/client"
)

type Config struct {
	indexobj.BuilderConfig `yaml:",inline"`
	EventsPerIndex         int                    `yaml:"events_per_index" experimental:"true"`
	IndexStoragePrefix     string                 `yaml:"index_storage_prefix" experimental:"true"`
	EnabledTenantIDs       flagext.StringSliceCSV `yaml:"enabled_tenant_ids" experimental:"true"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("dataobj-index-builder.", f)
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.BuilderConfig.RegisterFlagsWithPrefix(prefix, f)
	f.IntVar(&cfg.EventsPerIndex, prefix+"events-per-index", 32, "Experimental: The number of events to batch before building an index")
	f.StringVar(&cfg.IndexStoragePrefix, prefix+"storage-prefix", "index/v0/", "Experimental: A prefix to use for storing indexes in object storage. Used to separate the metastore & index files during initial testing.")
	f.Var(&cfg.EnabledTenantIDs, prefix+"enabled-tenant-ids", "Experimental: A list of tenant IDs to enable index building for. If empty, all tenants will be enabled.")
}

type downloadedObject struct {
	event       metastore.ObjectWrittenEvent
	objectBytes *[]byte
	err         error
}

const (
	indexConsumerGroup = "metastore-event-reader"
)

type Builder struct {
	services.Service

	cfg  Config
	mCfg metastore.Config

	// Kafka client and topic/partition info
	client *kgo.Client
	topic  string

	// Processing pipeline
	downloadQueue     chan metastore.ObjectWrittenEvent
	downloadedObjects chan downloadedObject
	calculator        *Calculator

	bufferedEvents map[string][]metastore.ObjectWrittenEvent

	// Builder initialization
	builderCfg  indexobj.BuilderConfig
	bucket      objstore.Bucket
	flushBuffer *bytes.Buffer

	// Metrics
	metrics *indexBuilderMetrics

	// Control and coordination
	ctx    context.Context
	cancel context.CancelCauseFunc
	wg     sync.WaitGroup
	logger log.Logger
}

func NewIndexBuilder(
	cfg Config,
	mCfg metastore.Config,
	kafkaCfg kafka.Config,
	logger log.Logger,
	instanceID string,
	bucket objstore.Bucket,
	reg prometheus.Registerer,
) (*Builder, error) {
	kafkaCfg.AutoCreateTopicEnabled = true
	kafkaCfg.AutoCreateTopicDefaultPartitions = 64
	eventConsumerClient, err := client.NewReaderClient(
		"index_builder",
		kafkaCfg,
		logger,
		reg,
		kgo.ConsumeTopics(kafkaCfg.Topic),
		kgo.InstanceID(instanceID),
		kgo.SessionTimeout(3*time.Minute),
		kgo.ConsumerGroup(indexConsumerGroup),
		kgo.RebalanceTimeout(5*time.Minute),
		kgo.DisableAutoCommit(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka consumer client: %w", err)
	}

	reg = prometheus.WrapRegistererWith(prometheus.Labels{
		"topic":     kafkaCfg.Topic,
		"component": "index_builder",
	}, reg)

	metrics := newIndexBuilderMetrics()
	if err := metrics.register(reg); err != nil {
		return nil, fmt.Errorf("failed to register metrics for index builder: %w", err)
	}

	builder, err := indexobj.NewBuilder(cfg.BuilderConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create index builder: %w", err)
	}
	calculator := NewCalculator(builder)

	if err := builder.RegisterMetrics(reg); err != nil {
		return nil, fmt.Errorf("failed to register metrics for index builder: %w", err)
	}

	// Allocate a single buffer
	flushBuffer := bytes.NewBuffer(make([]byte, int(float64(cfg.BuilderConfig.TargetObjectSize)*1.2)))

	// Set up queues to download the next object (I/O bound) while processing the current one (CPU bound) in order to maximize throughput.
	// Setting the channel buffer sizes caps the total memory usage by only keeping up to 3 objects in memory at a time: One being processed, one fully downloaded and one being downloaded from the queue.
	downloadQueue := make(chan metastore.ObjectWrittenEvent, cfg.EventsPerIndex)
	downloadedObjects := make(chan downloadedObject, 1)

	s := &Builder{
		cfg:               cfg,
		mCfg:              mCfg,
		client:            eventConsumerClient,
		logger:            logger,
		bucket:            bucket,
		flushBuffer:       flushBuffer,
		downloadedObjects: downloadedObjects,
		downloadQueue:     downloadQueue,
		metrics:           metrics,
		calculator:        calculator,
		bufferedEvents:    make(map[string][]metastore.ObjectWrittenEvent),
	}

	s.Service = services.NewBasicService(nil, s.run, s.stopping)

	return s, nil
}

func (p *Builder) run(ctx context.Context) error {
	p.ctx, p.cancel = context.WithCancelCause(ctx)

	p.wg.Add(1)
	go func() {
		// Download worker
		defer p.wg.Done()
		for event := range p.downloadQueue {
			objLogger := log.With(p.logger, "object_path", event.ObjectPath)
			downloadStart := time.Now()

			objectReader, err := p.bucket.Get(p.ctx, event.ObjectPath)
			if err != nil {
				p.downloadedObjects <- downloadedObject{
					event: event,
					err:   fmt.Errorf("failed to fetch object from storage: %w", err),
				}
				continue
			}

			object, err := io.ReadAll(objectReader)
			_ = objectReader.Close()
			if err != nil {
				p.downloadedObjects <- downloadedObject{
					event: event,
					err:   fmt.Errorf("failed to read object: %w", err),
				}
				continue
			}
			level.Info(objLogger).Log("msg", "downloaded object", "duration", time.Since(downloadStart), "size_mb", float64(len(object))/1024/1024, "avg_speed_mbps", float64(len(object))/time.Since(downloadStart).Seconds()/1024/1024)
			p.downloadedObjects <- downloadedObject{
				event:       event,
				objectBytes: &object,
			}
		}
	}()

	level.Info(p.logger).Log("msg", "started index builder service")
	for {
		fetches := p.client.PollRecords(ctx, -1)
		if fetches.IsClientClosed() || ctx.Err() != nil {
			return ctx.Err()
		}
		if errs := fetches.Errors(); len(errs) > 0 {
			level.Error(p.logger).Log("msg", "error fetching records", "err", errs)
			continue
		}
		if fetches.Empty() {
			continue
		}
		fetches.EachPartition(func(ftp kgo.FetchTopicPartition) {
			// TODO(benclive): Verify if we need to return re-poll ASAP or if sequential processing is good enough.
			for _, record := range ftp.Records {
				p.processRecord(record)
			}
		})
	}
}

func (p *Builder) stopping(failureCase error) error {
	close(p.downloadQueue)
	p.cancel(failureCase)
	p.wg.Wait()
	close(p.downloadedObjects)
	p.client.Close()
	return nil
}

func (p *Builder) processRecord(record *kgo.Record) {
	event := &metastore.ObjectWrittenEvent{}
	if err := event.Unmarshal(record.Value); err != nil {
		level.Error(p.logger).Log("msg", "failed to unmarshal metastore event", "err", err)
		return
	}
	p.bufferedEvents[event.Tenant] = append(p.bufferedEvents[event.Tenant], *event)
	level.Info(p.logger).Log("msg", "buffered new event for tenant", "count", len(p.bufferedEvents[event.Tenant]), "tenant", event.Tenant)

	if len(p.bufferedEvents[event.Tenant]) >= p.cfg.EventsPerIndex {
		if !slices.Contains(p.cfg.EnabledTenantIDs, event.Tenant) {
			// TODO(benclive): Remove this check once builders handle multi-tenancy when building indexes.
			level.Info(p.logger).Log("msg", "skipping index build for disabled tenant", "tenant", event.Tenant)
			p.bufferedEvents[event.Tenant] = p.bufferedEvents[event.Tenant][:0]
			return
		}
		err := p.BuildIndex(p.bufferedEvents[event.Tenant][:len(p.bufferedEvents[event.Tenant])])
		if err != nil {
			// TODO(benclive): Improve error handling for failed index builds.
			panic(err)
		}

		if err := p.commitRecords(record); err != nil {
			level.Warn(p.logger).Log("msg", "failed to commit records", "err", err)
			return
		}
		p.bufferedEvents[event.Tenant] = p.bufferedEvents[event.Tenant][:0]
	}
}

func (p *Builder) BuildIndex(events []metastore.ObjectWrittenEvent) error {
	indexStorageBucket := objstore.NewPrefixedBucket(p.bucket, p.cfg.IndexStoragePrefix)
	level.Info(p.logger).Log("msg", "building index", "events", len(events), "tenant", events[0].Tenant)
	start := time.Now()

	// Observe processing delay
	writeTime, err := time.Parse(time.RFC3339, events[0].WriteTime)
	if err != nil {
		level.Error(p.logger).Log("msg", "failed to parse write time", "err", err)
		return err
	}
	p.metrics.observeProcessingDelay(writeTime)

	// Trigger the downloads
	for _, event := range events {
		p.downloadQueue <- event
	}

	// Process the results as they are downloaded
	processingErrors := multierror.New()
	for i := 0; i < len(events); i++ {
		obj := <-p.downloadedObjects
		objLogger := log.With(p.logger, "object_path", obj.event.ObjectPath)
		level.Info(objLogger).Log("msg", "processing object")

		if obj.err != nil {
			processingErrors.Add(fmt.Errorf("failed to download object: %w", obj.err))
			continue
		}

		reader, err := dataobj.FromReaderAt(bytes.NewReader(*obj.objectBytes), int64(len(*obj.objectBytes)))
		if err != nil {
			processingErrors.Add(fmt.Errorf("failed to read object: %w", err))
			continue
		}

		if err := p.calculator.Calculate(p.ctx, objLogger, reader, obj.event.ObjectPath); err != nil {
			processingErrors.Add(fmt.Errorf("failed to calculate index: %w", err))
			continue
		}
	}

	if processingErrors.Err() != nil {
		return processingErrors.Err()
	}

	p.flushBuffer.Reset()
	stats, err := p.calculator.Flush(p.flushBuffer)
	if err != nil {
		return fmt.Errorf("failed to flush builder: %w", err)
	}

	size := p.flushBuffer.Len()

	key := IndexObjectKey(events[0].Tenant, p.flushBuffer)
	if err := indexStorageBucket.Upload(p.ctx, key, p.flushBuffer); err != nil {
		return fmt.Errorf("failed to upload index: %w", err)
	}

	metastoreUpdater := metastore.NewUpdater(p.mCfg.Updater, indexStorageBucket, events[0].Tenant, p.logger)
	if stats.MinTimestamp.IsZero() || stats.MaxTimestamp.IsZero() {
		return errors.New("failed to get min/max timestamps")
	}
	if err := metastoreUpdater.Update(p.ctx, key, stats.MinTimestamp, stats.MaxTimestamp); err != nil {
		return fmt.Errorf("failed to update metastore: %w", err)
	}

	level.Info(p.logger).Log("msg", "finished building index", "tenant", events[0].Tenant, "events", len(events), "size", size, "duration", time.Since(start))
	return nil
}

// getKey determines the key in object storage to upload the object to, based on our path scheme.
func IndexObjectKey(tenantID string, object *bytes.Buffer) string {
	sum := sha256.Sum224(object.Bytes())
	sumStr := hex.EncodeToString(sum[:])

	return fmt.Sprintf("tenant-%s/indexes/%s/%s", tenantID, sumStr[:2], sumStr[2:])
}

func (p *Builder) commitRecords(record *kgo.Record) error {
	backoff := backoff.New(p.ctx, backoff.Config{
		MinBackoff: 100 * time.Millisecond,
		MaxBackoff: 10 * time.Second,
		MaxRetries: 20,
	})

	var lastErr error
	backoff.Reset()
	for backoff.Ongoing() {
		p.metrics.incCommitsTotal()
		err := p.client.CommitRecords(p.ctx, record)
		if err == nil {
			return nil
		}
		level.Error(p.logger).Log("msg", "failed to commit records", "err", err)
		p.metrics.incCommitFailures()
		lastErr = err
		backoff.Wait()
	}
	return lastErr
}
