package ruler

import (
	"fmt"

	"github.com/cortexproject/cortex/pkg/cortexpb"
	"github.com/golang/snappy"
	"github.com/grafana/loki/pkg/util"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage/remote"
)

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
}

// PrepareRequest takes the given queue and serialized it into a compressed
// proto write request that will be sent to Cortex
func (r *remoteWriteClient) PrepareRequest(queue *util.EvictingQueue) ([]byte, error) {
	var labels []labels.Labels
	var samples []cortexpb.Sample

	for _, entry := range queue.Entries() {
		entry, ok := entry.(queueEntry)
		if !ok {
			return nil, fmt.Errorf("queue contains invalid entry of type: %T", entry)
		}

		labels = append(labels, entry.labels)
		samples = append(samples, entry.sample)
	}

	req := cortexpb.ToWriteRequest(labels, samples, nil, cortexpb.RULE)
	defer cortexpb.ReuseSlice(req.Timeseries)

	reqBytes, err := req.Marshal()
	if err != nil {
		return nil, err
	}

	return snappy.Encode(nil, reqBytes), nil
}
