package ruler

import (
	"github.com/cortexproject/cortex/pkg/cortexpb"
	"github.com/prometheus/prometheus/pkg/labels"
)

type RemoteWriteQueue struct {
	labels  []labels.Labels
	samples []cortexpb.Sample

	capacity int
}

func NewRemoteWriteQueue(capacity int) *RemoteWriteQueue {
	return &RemoteWriteQueue{
		capacity: capacity,
	}
}

func (q *RemoteWriteQueue) Append(labels labels.Labels, sample cortexpb.Sample) {
	if len(q.samples) >= q.capacity {
		// capacity exceeded, delete oldest sample
		q.samples = append(q.samples[:0], q.samples[1:]...)
		q.labels = append(q.labels[:0], q.labels[1:]...)
	}

	q.labels = append(q.labels, labels)
	q.samples = append(q.samples, sample)
}

func (q *RemoteWriteQueue) Length() int {
	return len(q.samples)
}

func (q *RemoteWriteQueue) Clear() {
	q.samples = nil
	q.labels = nil
}
