package physical

import (
	"iter"
	"slices"
	"time"

	"github.com/oklog/ulid/v2"
)

// DefaultDummyLoadLabels is the default set of label names emitted by a
// DummyLoad node when no Labels are specified.
var DefaultDummyLoadLabels = []string{
	"cluster",
	"namespace",
	"container",
	"pod",
	"level",
	"service_name",
	"bytes_processed",
}

// DummyLoad is a physical plan node that generates synthetic Arrow data for
// testing and benchmarking purposes.
//
// DummyLoad implements [ShardableNode] and its
// [Shards] method returns Parallelism individual DummyLoad nodes that each
// produce a fraction of the total batches.
type DummyLoad struct {
	NodeID ulid.ULID

	// NumBatches is the number of batches this node (or shard) should emit.
	NumBatches int
	// BatchSize is the number of rows per emitted batch.
	BatchSize int
	// SleepPerBatch is how long to sleep before emitting each batch.
	SleepPerBatch time.Duration
	// Parallelism is the number of shards produced by [Shards].
	Parallelism int
	// Labels is the list of label names to include as columns in each emitted
	// record batch. If empty, [DefaultDummyLoadLabels] is used.
	Labels []string
}

var _ ShardableNode = (*DummyLoad)(nil)

// ID implements [Node].
func (d *DummyLoad) ID() ulid.ULID { return d.NodeID }

// Type implements [Node].
func (*DummyLoad) Type() NodeType { return NodeTypeDummyLoad }

// Clone returns a deep copy with a new unique ID.
func (d *DummyLoad) Clone() Node {
	return &DummyLoad{
		NodeID:        ulid.Make(),
		NumBatches:    d.NumBatches,
		BatchSize:     d.BatchSize,
		SleepPerBatch: d.SleepPerBatch,
		Parallelism:   d.Parallelism,
		Labels:        slices.Clone(d.Labels),
	}
}

// Shards divides NumBatches evenly across Parallelism shard nodes. Each
// returned node is a DummyLoad with Parallelism=1 (leaf shards are not
// further divided).
//
// If Parallelism <= 1, Shards yields a single clone of the receiver.
func (d *DummyLoad) Shards() iter.Seq[Node] {
	parallelism := d.Parallelism
	if parallelism <= 1 {
		parallelism = 1
	}

	return func(yield func(Node) bool) {
		batchesPerShard := d.NumBatches / parallelism
		remainder := d.NumBatches % parallelism

		for i := 0; i < parallelism; i++ {
			n := batchesPerShard
			if i < remainder {
				n++
			}
			shard := &DummyLoad{
				NodeID:        ulid.Make(),
				NumBatches:    n,
				BatchSize:     d.BatchSize,
				SleepPerBatch: d.SleepPerBatch,
				Parallelism:   1,
				Labels:        slices.Clone(d.Labels),
			}
			if !yield(shard) {
				return
			}
		}
	}
}
