package logical

import (
	"fmt"
	"strings"
	"time"
)

// DummyLoad is a source instruction that generates random Arrow data for
// testing and benchmarking. Unlike MakeTable, it requires no catalog and
// produces synthetic data. DummyLoad implements both [Instruction] and [Value].
type DummyLoad struct {
	b baseNode

	// NumBatches is the total number of batches to generate across all shards.
	NumBatches int
	// BatchSize is the number of rows per generated batch.
	BatchSize int
	// SleepPerBatch is how long to sleep before emitting each batch, to
	// simulate slow I/O.
	SleepPerBatch time.Duration
	// Parallelism is the number of parallel tasks to split the load into.
	Parallelism int
	// Labels is the list of label names to include as columns in each emitted
	// record batch. If empty, [physical.DefaultDummyLoadLabels] is used.
	Labels []string
}

var (
	_ Value       = (*DummyLoad)(nil)
	_ Instruction = (*DummyLoad)(nil)
)

// Name returns an identifier for the DummyLoad operation.
func (d *DummyLoad) Name() string { return d.b.Name() }

// String returns the disassembled SSA form of the DummyLoad instruction.
func (d *DummyLoad) String() string {
	return fmt.Sprintf("DUMMYLOAD [num_batches=%d, batch_size=%d, sleep_per_batch=%s, parallelism=%d, labels=[%s]]",
		d.NumBatches, d.BatchSize, d.SleepPerBatch, d.Parallelism, strings.Join(d.Labels, ","))
}

// Operands returns an empty slice; DummyLoad has no input values.
func (d *DummyLoad) Operands(buf []*Value) []*Value { return buf }

// Referrers returns a list of instructions that reference the DummyLoad.
func (d *DummyLoad) Referrers() *[]Instruction { return &d.b.referrers }

func (d *DummyLoad) base() *baseNode { return &d.b }
func (d *DummyLoad) isInstruction()  {}
func (d *DummyLoad) isValue()        {}
