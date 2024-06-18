package ingester_rf1

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/distributor/writefailures"
	"github.com/grafana/loki/v3/pkg/loghttp/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

type instance struct {
	buf        []byte // buffer used to compute fps.
	streams    *streamsMap
	instanceID string
}

func (i *instance) Push(ctx context.Context, req *logproto.PushRequest) error {
	//fmt.Println("pushing for instance", i.instanceID, "req", req)
	return nil
}

func newInstance(
	cfg *Config,
	periodConfigs []config.PeriodConfig,
	instanceID string,
	limiter *Limiter,
	configs *runtime.TenantConfigs,
	metrics *ingesterMetrics,
	chunkFilter chunk.RequestChunkFilterer,
	pipelineWrapper log.PipelineWrapper,
	extractorWrapper log.SampleExtractorWrapper,
	//streamRateCalculator *StreamRateCalculator,
	writeFailures *writefailures.Manager,
	customStreamsTracker push.UsageTracker,
) (*instance, error) {
	fmt.Println("new instance for", instanceID)
	//invertedIndex, err := index.NewMultiInvertedIndex(periodConfigs, uint32(cfg.IndexShards))
	//if err != nil {
	//	return nil, err
	//}
	streams := newStreamsMap()
	//ownedStreamsSvc := newOwnedStreamService(instanceID, limiter)
	//c := config.SchemaConfig{Configs: periodConfigs}
	i := &instance{
		//cfg:        cfg,
		streams: streams,
		buf:     make([]byte, 0, 1024),
		//index:      invertedIndex,
		instanceID: instanceID,
		//
		//streamsCreatedTotal: streamsCreatedTotal.WithLabelValues(instanceID),
		//streamsRemovedTotal: streamsRemovedTotal.WithLabelValues(instanceID),
		//
		//tailers:            map[uint32]*tailer{},
		//limiter:            limiter,
		//streamCountLimiter: newStreamCountLimiter(instanceID, streams.Len, limiter, ownedStreamsSvc),
		//ownedStreamsSvc:    ownedStreamsSvc,
		//configs:            configs,
		//
		//wal:                   wal,
		//metrics:               metrics,
		//flushOnShutdownSwitch: flushOnShutdownSwitch,
		//
		//chunkFilter:      chunkFilter,
		//pipelineWrapper:  pipelineWrapper,
		//extractorWrapper: extractorWrapper,
		//
		//streamRateCalculator: streamRateCalculator,
		//
		//writeFailures: writeFailures,
		//schemaconfig:  &c,
		//
		//customStreamsTracker: customStreamsTracker,
	}
	//i.mapper = NewFPMapper(i.getLabelsFromFingerprint)

	return i, nil
}
