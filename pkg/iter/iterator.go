package iter

import "github.com/grafana/loki/pkg/logproto"

// Iterator iterates over data in time-order.
type Iterator interface {
	// Returns true if there is more data to iterate.
	Next() bool
	// Labels returns the labels for the current entry.
	// The labels can be mutated by the query engine and not reflect the original stream.
	Labels() string
	// StreamHash returns the hash of the original stream for the current entry.
	StreamHash() uint64
	Error() error
	Close() error
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
