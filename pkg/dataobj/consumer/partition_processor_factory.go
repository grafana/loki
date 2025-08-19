package consumer

import (
	"context"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/scratch"
)

type partitionProcessorFactory struct {
	cfg Config
	// TODO(grobinson): We should see if we can move metastore.Config inside
	// Config instead of having a separate field just for the metastore.
	metastoreCfg         metastore.Config
	client               *kgo.Client
	eventsProducerClient *kgo.Client
	bucket               objstore.Bucket
	scratchStore         scratch.Store
	logger               log.Logger
	reg                  prometheus.Registerer
}

// newPartitionProcessorFactory returns a new partitionProcessorFactory.
func newPartitionProcessorFactory(
	cfg Config,
	metastoreCfg metastore.Config,
	client *kgo.Client,
	eventsProducerClient *kgo.Client,
	bucket objstore.Bucket,
	scratchStore scratch.Store,
	logger log.Logger,
	reg prometheus.Registerer,
) *partitionProcessorFactory {
	return &partitionProcessorFactory{
		cfg:                  cfg,
		metastoreCfg:         metastoreCfg,
		client:               client,
		eventsProducerClient: eventsProducerClient,
		bucket:               bucket,
		scratchStore:         scratchStore,
		logger:               logger,
		reg:                  reg,
	}
}

// New creates a new processor for the per-tenant topic partition.
func (f *partitionProcessorFactory) New(
	ctx context.Context,
	tenant string,
	virtualShard int32,
	topic string,
	partition int32,
) *partitionProcessor {
	return newPartitionProcessor(
		ctx,
		f.client,
		f.cfg.BuilderConfig,
		f.cfg.UploaderConfig,
		f.metastoreCfg,
		f.bucket,
		f.scratchStore,
		tenant,
		virtualShard,
		topic,
		partition,
		f.logger,
		f.reg,
		f.cfg.IdleFlushTimeout,
		f.eventsProducerClient,
	)
}
