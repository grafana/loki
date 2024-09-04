package tee

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/grafana/loki/v3/pkg/distributor"
	"github.com/grafana/loki/v3/pkg/kafka"
)

const writeTimeout = time.Minute

type Tee struct {
	logger        log.Logger
	kafkaClient   *kgo.Client
	partitionRing *ring.PartitionInstanceRing

	ingesterAppends *prometheus.CounterVec
}

func NewTee(
	cfg kafka.Config,
	metricsNamespace string,
	registerer prometheus.Registerer,
	logger log.Logger,
	partitionRing *ring.PartitionInstanceRing,
) (*Tee, error) {
	registerer = prometheus.WrapRegistererWithPrefix(metricsNamespace+"_", registerer)

	kafkaClient, err := kafka.NewWriterClient(cfg, 20, logger, registerer)
	if err != nil {
		panic("failed to start kafka client")
	}

	t := &Tee{
		logger: log.With(logger, "component", "kafka-tee"),
		ingesterAppends: promauto.With(registerer).NewCounterVec(prometheus.CounterOpts{
			Name: "kafka_ingester_appends_total",
			Help: "The total number of appends sent to kafka ingest path.",
		}, []string{"partition", "status"}),
		kafkaClient:   kafkaClient,
		partitionRing: partitionRing,
	}

	return t, nil
}

// Duplicate Implements distributor.Tee which is used to tee distributor requests to pattern ingesters.
func (t *Tee) Duplicate(tenant string, streams []distributor.KeyedStream) {
	for idx := range streams {
		go func(stream distributor.KeyedStream) {
			if err := t.sendStream(tenant, stream); err != nil {
				level.Error(t.logger).Log("msg", "failed to send stream to kafka", "err", err)
			}
		}(streams[idx])
	}
}

var encoderPool = sync.Pool{
	New: func() any {
		return kafka.NewEncoder()
	},
}

func (t *Tee) sendStream(tenant string, stream distributor.KeyedStream) error {
	encoder := encoderPool.Get().(*kafka.Encoder)
	defer encoderPool.Put(encoder)

	partitionID, err := t.partitionRing.PartitionRing().ActivePartitionForKey(stream.HashKey)
	if err != nil {
		t.ingesterAppends.WithLabelValues("partition_unknown", "fail").Inc()
		return fmt.Errorf("failed to find active partition for stream: %w", err)
	}
	records, err := encoder.Encode(partitionID, tenant, stream.Stream)
	if err != nil {
		t.ingesterAppends.WithLabelValues(fmt.Sprintf("partition_%d", partitionID), "fail").Inc()
		return fmt.Errorf("failed to marshal write request to records: %w", err)
	}

	ctx, cancel := context.WithTimeout(user.InjectOrgID(context.Background(), tenant), writeTimeout)
	defer cancel()
	produceResults := t.kafkaClient.ProduceSync(ctx, records...)

	var finalErr error
	for _, result := range produceResults {
		if result.Err != nil {
			t.ingesterAppends.WithLabelValues(fmt.Sprintf("partition_%d", partitionID), "fail").Inc()
			finalErr = err
		} else {
			t.ingesterAppends.WithLabelValues(fmt.Sprintf("partition_%d", partitionID), "success").Inc()
		}
	}

	return finalErr
}
