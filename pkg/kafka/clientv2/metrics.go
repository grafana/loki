package clientv2

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/plugin/kprom"
)

// NewMetrics returns a new instance of kprom.Metrics.
func NewMetrics(namespace string, reg prometheus.Registerer, enableKafkaHistograms bool) *kprom.Metrics {
	return kprom.NewMetrics(
		namespace,
		kprom.Registerer(reg),
		kprom.FetchAndProduceDetail(
			kprom.Batches,
			kprom.Records,
			kprom.CompressedBytes,
			kprom.UncompressedBytes,
		),
		enableKafkaHistogramMetrics(enableKafkaHistograms),
	)
}

func enableKafkaHistogramMetrics(enable bool) kprom.Opt {
	histogramOpts := []kprom.HistogramOpts{}
	if enable {
		histogramOpts = append(histogramOpts,
			kprom.HistogramOpts{
				Enable:  kprom.ReadTime,
				Buckets: prometheus.DefBuckets,
			}, kprom.HistogramOpts{
				Enable:  kprom.ReadWait,
				Buckets: prometheus.DefBuckets,
			}, kprom.HistogramOpts{
				Enable:  kprom.WriteTime,
				Buckets: prometheus.DefBuckets,
			}, kprom.HistogramOpts{
				Enable:  kprom.WriteWait,
				Buckets: prometheus.DefBuckets,
			})
	}
	return kprom.HistogramsFromOpts(histogramOpts...)
}
