package consumer

import (
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/kafka/partition"
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
	topic           string
	partition       int32
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
	topic string,
	partition int32,
) *partitionProcessorFactory {
	return &partitionProcessorFactory{
		cfg:             cfg,
		metastoreCfg:    metastoreCfg,
		metastoreEvents: metastoreEvents,
		bucket:          bucket,
		scratchStore:    scratchStore,
		logger:          logger,
		reg:             reg,
		topic:           topic,
		partition:       partition,
	}
}

// New returns a new processor for the partition.
func (f *partitionProcessorFactory) New(committer partition.Committer, logger log.Logger) (partition.Consumer, error) {
	return newPartitionProcessor(
		committer,
		f.cfg.BuilderConfig,
		f.cfg.UploaderConfig,
		f.metastoreCfg,
		f.bucket,
		f.scratchStore,
		f.logger,
		f.reg,
		f.cfg.IdleFlushTimeout,
		f.metastoreEvents,
		f.topic,
		f.partition,
	), nil
}
