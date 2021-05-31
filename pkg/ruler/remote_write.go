package ruler

import (
	"fmt"

	"github.com/cortexproject/cortex/pkg/cortexpb"
	"github.com/golang/snappy"
	"github.com/pkg/errors"
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

// PrepareRequest takes the given queue and serialized it into a compressed
// proto write request that will be sent to Cortex
func (r *remoteWriteClient) PrepareRequest(queue *util.EvictingQueue) ([]byte, error) {
	r.labels = make([]labels.Labels, 0, queue.Length())
	r.samples = make([]cortexpb.Sample, 0, queue.Length())

	for _, entry := range queue.Entries() {
		entry, ok := entry.(queueEntry)
		if !ok {
			return nil, fmt.Errorf("queue contains invalid entry of type: %T", entry)
		}

		r.labels = append(r.labels, entry.labels)
		r.samples = append(r.samples, entry.sample)
	}

	req := cortexpb.ToWriteRequest(r.labels, r.samples, nil, cortexpb.RULE)
	defer cortexpb.ReuseSlice(req.Timeseries)

	reqBytes, err := req.Marshal()
	if err != nil {
		return nil, err
	}

	return snappy.Encode(nil, reqBytes), nil
}
