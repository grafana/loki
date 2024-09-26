// SPDX-License-Identifier: AGPL-3.0-only

package kafka

import (
	"time"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/plugin/kprom"
)

// NewReaderClient returns the kgo.Client that should be used by the Reader.
func NewReaderClient(cfg Config, metrics *kprom.Metrics, logger log.Logger, opts ...kgo.Opt) (*kgo.Client, error) {
	const fetchMaxBytes = 100_000_000

	opts = append(opts, commonKafkaClientOptions(cfg, metrics, logger)...)
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
		return nil, errors.Wrap(err, "creating kafka client")
	}

	return client, nil
}

func NewReaderClientMetrics(component string, reg prometheus.Registerer) *kprom.Metrics {
	return kprom.NewMetrics("loki_ingest_storage_reader",
		kprom.Registerer(prometheus.WrapRegistererWith(prometheus.Labels{"component": component}, reg)),
		// Do not export the client ID, because we use it to specify options to the backend.
		kprom.FetchAndProduceDetail(kprom.Batches, kprom.Records, kprom.CompressedBytes, kprom.UncompressedBytes))
}
