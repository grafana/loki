package ruler

import (
	"github.com/cortexproject/cortex/pkg/cortexpb"
	"github.com/prometheus/prometheus/pkg/labels"
)

// DefaultCapacity defines the default size of the samples buffer which will hold samples
// while the remote-write endpoint is unavailable
const DefaultCapacity = 10000

type remoteWriteQueue struct {
	labels  []labels.Labels
	samples []cortexpb.Sample

	capacity int
	userID   string
	key      string
}

func newRemoteWriteQueue(capacity int, userID, key string) *remoteWriteQueue {
	if capacity == 0 {
		capacity = DefaultCapacity
	}

	return &remoteWriteQueue{
		capacity: capacity,
		key:      key,
		userID:   userID,
	}
}

func (q *remoteWriteQueue) append(labels labels.Labels, sample cortexpb.Sample) {
	if len(q.samples) >= q.capacity {
		samplesDiscarded.WithLabelValues(q.userID, q.key).Inc()

		// capacity exceeded, delete oldest sample
		q.samples = append(q.samples[:0], q.samples[1:]...)
		q.labels = append(q.labels[:0], q.labels[1:]...)
	}

	q.labels = append(q.labels, labels)
	q.samples = append(q.samples, sample)

	samplesBuffered.WithLabelValues(q.userID, q.key).Inc()
}

func (q *remoteWriteQueue) length() int {
	return len(q.samples)
}

func (q *remoteWriteQueue) clear() {
	q.samples = nil
	q.labels = nil
}
