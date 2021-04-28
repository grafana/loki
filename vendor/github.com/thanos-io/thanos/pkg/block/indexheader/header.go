// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package indexheader

import (
	"io"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/tsdb/index"
)

// NotFoundRangeErr is an error returned by PostingsOffset when there is no posting for given name and value pairs.
var NotFoundRangeErr = errors.New("range not found")

// Reader is an interface allowing to read essential, minimal number of index fields from the small portion of index file called header.
type Reader interface {
	io.Closer

	// IndexVersion returns version of index.
	IndexVersion() (int, error)

	// PostingsOffset returns start and end offsets of postings for given name and value.
	// The end offset might be bigger than the actual posting ending, but not larger than the whole index file.
	// NotFoundRangeErr is returned when no index can be found for given name and value.
	// TODO(bwplotka): Move to PostingsOffsets(name string, value ...string) []index.Range and benchmark.
	PostingsOffset(name string, value string) (index.Range, error)

	// LookupSymbol returns string based on given reference.
	// Error is return if the symbol can't be found.
	LookupSymbol(o uint32) (string, error)

	// LabelValues returns all label values for given label name or error.
	// If no values are found for label name, or label name does not exists,
	// then empty string is returned and no error.
	LabelValues(name string) ([]string, error)

	// LabelNames returns all label names in sorted order.
	LabelNames() ([]string, error)
}
