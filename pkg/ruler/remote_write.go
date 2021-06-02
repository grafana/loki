package ruler

import (
	"fmt"

	"github.com/cortexproject/cortex/pkg/cortexpb"
	"github.com/golang/snappy"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage/remote"

	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/build"
)

var UserAgent = fmt.Sprintf("loki-remote-write/%s", build.Version)

type queueEntry struct {
	labels labels.Labels
	sample cortexpb.Sample
}

type remoteWriter interface {
	remote.WriteClient

	PrepareRequest(queue *util.EvictingQueue) ([]byte, error)
}

type remoteWriteClient struct {
	remote.WriteClient

	labels  []labels.Labels
	samples []cortexpb.Sample
}

type remoteWriteMetrics struct {
	samplesEvicted       *prometheus.CounterVec
	samplesQueuedTotal   *prometheus.CounterVec
	samplesQueued        *prometheus.GaugeVec
	samplesQueueCapacity *prometheus.GaugeVec
	remoteWriteErrors    *prometheus.CounterVec
}

func newRemoteWriter(cfg Config, userID string) (remoteWriter, error) {
	writeClient, err := remote.NewWriteClient("recording_rules", &remote.ClientConfig{
		URL:              cfg.RemoteWrite.Client.URL,
		Timeout:          cfg.RemoteWrite.Client.RemoteTimeout,
		HTTPClientConfig: cfg.RemoteWrite.Client.HTTPClientConfig,
		Headers: util.MergeMaps(cfg.RemoteWrite.Client.Headers, map[string]string{
			"X-Scope-OrgID": userID,
			"User-Agent":    UserAgent,
		}),
	})
	if err != nil {
		return nil, errors.Wrapf(err, "could not create remote-write client for tenant: %v", userID)
	}

	return &remoteWriteClient{
		WriteClient: writeClient,
	}, nil
}

func (r *remoteWriteClient) prepare(queue *util.EvictingQueue) error {
	// reuse slices, resize if they are not big enough
	if cap(r.labels) < queue.Length() {
		r.labels = make([]labels.Labels, 0, queue.Length())
	}
	if cap(r.samples) < queue.Length() {
		r.samples = make([]cortexpb.Sample, 0, queue.Length())
	}

	r.labels = r.labels[:0]
	r.samples = r.samples[:0]

	for _, entry := range queue.Entries() {
		entry, ok := entry.(queueEntry)
		if !ok {
			return fmt.Errorf("queue contains invalid entry of type: %T", entry)
		}

		r.labels = append(r.labels, entry.labels)
		r.samples = append(r.samples, entry.sample)
	}

	return nil
}

// PrepareRequest takes the given queue and serializes it into a compressed
// proto write request that will be sent to Cortex
func (r *remoteWriteClient) PrepareRequest(queue *util.EvictingQueue) ([]byte, error) {
	// prepare labels and samples from queue
	err := r.prepare(queue)
	if err != nil {
		return nil, err
	}

	req := cortexpb.ToWriteRequest(r.labels, r.samples, nil, cortexpb.RULE)
	defer cortexpb.ReuseSlice(req.Timeseries)

	reqBytes, err := req.Marshal()
	if err != nil {
		return nil, err
	}

	return snappy.Encode(nil, reqBytes), nil
}

func newRemoteWriteMetrics(r prometheus.Registerer) *remoteWriteMetrics {
	return &remoteWriteMetrics{
		samplesEvicted: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "recording_rules_samples_evicted_total",
			Help:      "Number of samples evicted from queue; queue is full!",
		}, []string{"tenant", "group_key"}),
		samplesQueuedTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "recording_rules_samples_queued_total",
			Help:      "Number of samples queued in total.",
		}, []string{"tenant", "group_key"}),
		samplesQueued: promauto.With(r).NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "loki",
			Name:      "recording_rules_samples_queued_current",
			Help:      "Number of samples queued to be remote-written.",
		}, []string{"tenant", "group_key"}),
		samplesQueueCapacity: promauto.With(r).NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "loki",
			Name:      "recording_rules_samples_queue_capacity",
			Help:      "Number of samples that can be queued before eviction of oldest samples occurs.",
		}, []string{"tenant"}),
		remoteWriteErrors: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "recording_rules_remote_write_errors",
			Help:      "Number of samples that failed to be remote-written due to error.",
		}, []string{"tenant", "group_key"}),
	}
}
