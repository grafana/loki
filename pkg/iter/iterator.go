package iter

import (
	"github.com/apache/arrow/go/v14/arrow"
	"github.com/grafana/loki/pkg/logproto"
)

type baseIterator interface {
	// Returns true if there is more data to iterate.
	Next() bool
	StreamHash() uint64
	Error() error
	Close() error
}

// Iterator iterates over data in time-order.
type Iterator interface {
	baseIterator
	// Labels returns the labels for the current entry.
	// The labels can be mutated by the query engine and not reflect the original stream.
	Labels() string
	// StreamHash returns the hash of the original stream for the current entry.
}

type BatchIterator interface {
	baseIterator
	Labels() []string
}

type noOpIterator struct{}

var NoopIterator = noOpIterator{}

func (noOpIterator) Next() bool              { return false }
func (noOpIterator) Error() error            { return nil }
func (noOpIterator) Labels() string          { return "" }
func (noOpIterator) StreamHash() uint64      { return 0 }
func (noOpIterator) Entry() logproto.Entry   { return logproto.Entry{} }
func (noOpIterator) Sample() logproto.Sample { return logproto.Sample{} }
func (noOpIterator) Close() error            { return nil }

type noOpBatchIterator struct{}

var NoopBatchIterator = noOpBatchIterator{}

func (noOpBatchIterator) Next() bool                { return false }
func (noOpBatchIterator) Error() error              { return nil }
func (noOpBatchIterator) Labels() []string          { return nil }
func (noOpBatchIterator) StreamHash() uint64        { return 0 }
func (noOpBatchIterator) Entries() []logproto.Entry { return nil }
func (noOpBatchIterator) Samples() arrow.Record     { return nil }
func (noOpBatchIterator) Close() error              { return nil }
