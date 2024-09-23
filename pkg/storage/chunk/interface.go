// This file was taken from Prometheus (https://github.com/prometheus/prometheus).
// The original license header is included below:
//
// Copyright 2014 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package chunk

import (
	"context"
	"errors"
	"io"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/util/filter"
)

// ChunkLen is the length of a chunk in bytes.
const ChunkLen = 1024

var (
	ErrSliceNoDataInRange = errors.New("chunk has no data for given range to slice")
	ErrSliceChunkOverflow = errors.New("slicing should not overflow a chunk")
)

// Data is the interface for all chunks. Chunks are generally not
// goroutine-safe.
type Data interface {
	// Add adds a SamplePair to the chunks, performs any necessary
	// re-encoding, and creates any necessary overflow chunk.
	// The returned Chunk is the overflow chunk if it was created.
	// The returned Chunk is nil if the sample got appended to the same chunk.
	Add(sample model.SamplePair) (Data, error)
	Marshal(io.Writer) error
	UnmarshalFromBuf([]byte) error
	Encoding() Encoding
	// Rebound returns a smaller chunk that includes all samples between start and end (inclusive).
	// We do not want to change existing Slice implementations because
	// it is built specifically for query optimization and is a noop for some of the encodings.
	Rebound(start, end model.Time, filter filter.Func) (Data, error)
	// Size returns the approximate length of the chunk in bytes.
	Size() int
	// UncompressedSize returns the length of uncompressed bytes.
	UncompressedSize() int
	// Entries returns the number of entries in a chunk
	Entries() int
	Utilization() float64
}

// RequestChunkFilterer creates ChunkFilterer for a given request context.
type RequestChunkFilterer interface {
	ForRequest(ctx context.Context) Filterer
}

// Filterer filters chunks based on the metric.
type Filterer interface {
	ShouldFilter(metric labels.Labels) bool
	RequiredLabelNames() []string
}
