// SPDX-License-Identifier: AGPL-3.0-only

package client

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/plugin/kprom"

	"github.com/grafana/loki/v3/pkg/kafka"
)

// NewReaderClient returns the kgo.Client that should be used by the Reader.
func NewReaderClient(kafkaCfg kafka.Config, metrics *kprom.Metrics, logger log.Logger, opts ...kgo.Opt) (*kgo.Client, error) {
	const fetchMaxBytes = 100_000_000

	opts = append(opts, commonKafkaClientOptions(kafkaCfg, metrics, logger)...)
	opts = append(opts,
		kgo.FetchMinBytes(1),
		kgo.FetchMaxBytes(fetchMaxBytes),
		kgo.FetchMaxWait(5*time.Second),
		kgo.FetchMaxPartitionBytes(50_000_000),

		// BrokerMaxReadBytes sets the maximum response size that can be read from
		// Kafka. This is a safety measure to avoid OOMing on invalid responses.
		// franz-go recommendation is to set it 2x FetchMaxBytes.
		kgo.BrokerMaxReadBytes(2*fetchMaxBytes),
	)
	client, err := kgo.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("creating kafka client: %w", err)
	}
	if kafkaCfg.AutoCreateTopicEnabled {
		setDefaultNumberOfPartitionsForAutocreatedTopics(kafkaCfg, client, logger)
	}
	return client, nil
}

func NewReaderClientMetrics(component string, reg prometheus.Registerer) *kprom.Metrics {
	return kprom.NewMetrics("loki_ingest_storage_reader",
		kprom.Registerer(prometheus.WrapRegistererWith(prometheus.Labels{"component": component}, reg)),
		// Do not export the client ID, because we use it to specify options to the backend.
		kprom.FetchAndProduceDetail(kprom.Batches, kprom.Records, kprom.CompressedBytes, kprom.UncompressedBytes))
}

// setDefaultNumberOfPartitionsForAutocreatedTopics tries to set num.partitions config option on brokers.
// This is best-effort, if setting the option fails, error is logged, but not returned.
func setDefaultNumberOfPartitionsForAutocreatedTopics(cfg kafka.Config, cl *kgo.Client, logger log.Logger) {
	if cfg.AutoCreateTopicDefaultPartitions <= 0 {
		return
	}

	// Note: this client doesn't get closed because it is owned by the caller
	adm := kadm.NewClient(cl)

	defaultNumberOfPartitions := fmt.Sprintf("%d", cfg.AutoCreateTopicDefaultPartitions)
	_, err := adm.AlterBrokerConfigsState(context.Background(), []kadm.AlterConfig{
		{
			Op:    kadm.SetConfig,
			Name:  "num.partitions",
			Value: &defaultNumberOfPartitions,
		},
	})
	if err != nil {
		level.Error(logger).Log("msg", "failed to alter default number of partitions", "err", err)
		return
	}

	level.Info(logger).Log("msg", "configured Kafka-wide default number of partitions for auto-created topics (num.partitions)", "value", cfg.AutoCreateTopicDefaultPartitions)
}
