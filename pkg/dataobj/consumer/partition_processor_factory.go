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

// partitionProcessorFactory is a factory for partition processors.
type partitionProcessorFactory struct {
	cfg Config
	// TODO(grobinson): We should see if we can move metastore.Config inside
	// Config instead of having a separate field just for the metastore.
	metastoreCfg    metastore.Config
	metastoreEvents *kgo.Client
	bucket          objstore.Bucket
	scratchStore    scratch.Store
	logger          log.Logger
	reg             prometheus.Registerer
}

// newPartitionProcessorFactory returns a new partitionProcessorFactory.
func newPartitionProcessorFactory(
	cfg Config,
	metastoreCfg metastore.Config,
	metastoreEvents *kgo.Client,
	bucket objstore.Bucket,
	scratchStore scratch.Store,
	logger log.Logger,
	reg prometheus.Registerer,
) *partitionProcessorFactory {
	return &partitionProcessorFactory{
		cfg:             cfg,
		metastoreCfg:    metastoreCfg,
		metastoreEvents: metastoreEvents,
		bucket:          bucket,
		scratchStore:    scratchStore,
		logger:          logger,
		reg:             reg,
	}
}

// New creates a new processor for the per-tenant topic partition.
//
// New requires the caller to provide the [kgo.Client] as an argument. This
// is due to a circular dependency that occurs when creating a [kgo.Client]
// where the partition event handlers, such as [kgo.OnPartitionsAssigned] and
// [kgo.OnPartitionsRevoked] must be registered when the client is created.
// However, the lifecycler cannot be created without the factory, and the
// factory cannot be created with a [kgo.Client]. This is why New requires a
// [kgo.Client] as an argument.
func (f *partitionProcessorFactory) New(
	ctx context.Context,
	client *kgo.Client,
	topic string,
	partition int32,
) processor {
	return newPartitionProcessor(
		ctx,
		client,
		f.cfg.BuilderConfig,
		f.cfg.UploaderConfig,
		f.metastoreCfg,
		f.bucket,
		f.scratchStore,
		topic,
		partition,
		f.logger,
		f.reg,
		f.cfg.IdleFlushTimeout,
		f.metastoreEvents,
	)
}
