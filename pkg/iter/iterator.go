package iter

import (
	"errors"

	v2 "github.com/grafana/loki/v3/pkg/iter/v2"
	"github.com/grafana/loki/v3/pkg/logproto"
)

type logprotoType interface {
	logproto.Entry | logproto.Sample
}

type StreamIterator[T logprotoType] interface {
	v2.CloseIterator[T]
	// Labels returns the labels for the current entry.
	// The labels can be mutated by the query engine and not reflect the original stream.
	Labels() string
	// StreamHash returns the hash of the original stream for the current entry.
	StreamHash() uint64
}

type EntryIterator StreamIterator[logproto.Entry]
type SampleIterator StreamIterator[logproto.Sample]

// noOpIterator implements StreamIterator
type noOpIterator[T logprotoType] struct{}

func (noOpIterator[T]) Next() bool         { return false }
func (noOpIterator[T]) Err() error         { return nil }
func (noOpIterator[T]) At() (zero T)       { return zero }
func (noOpIterator[T]) Labels() string     { return "" }
func (noOpIterator[T]) StreamHash() uint64 { return 0 }
func (noOpIterator[T]) Close() error       { return nil }

var NoopEntryIterator = noOpIterator[logproto.Entry]{}
var NoopSampleIterator = noOpIterator[logproto.Sample]{}

// errorIterator implements StreamIterator
type errorIterator[T logprotoType] struct{}

func (errorIterator[T]) Next() bool         { return false }
func (errorIterator[T]) Err() error         { return errors.New("error") }
func (errorIterator[T]) At() (zero T)       { return zero }
func (errorIterator[T]) Labels() string     { return "" }
func (errorIterator[T]) StreamHash() uint64 { return 0 }
func (errorIterator[T]) Close() error       { return errors.New("close") }

var ErrorEntryIterator = errorIterator[logproto.Entry]{}
var ErrorSampleIterator = errorIterator[logproto.Sample]{}
