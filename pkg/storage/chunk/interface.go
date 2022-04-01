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
	"io"

	"github.com/prometheus/common/model"
	errs "github.com/weaveworks/common/errors"
)

const (
	// ChunkLen is the length of a chunk in bytes.
	ChunkLen = 1024

	ErrSliceNoDataInRange = errs.Error("chunk has no data for given range to slice")
	ErrSliceChunkOverflow = errs.Error("slicing should not overflow a chunk")
)

// ChunkData is the interface for all chunks. Chunks are generally not
// goroutine-safe.
type ChunkData interface {
	// Add adds a SamplePair to the chunks, performs any necessary
	// re-encoding, and creates any necessary overflow chunk.
	// The returned Chunk is the overflow chunk if it was created.
	// The returned Chunk is nil if the sample got appended to the same chunk.
	Add(sample model.SamplePair) (ChunkData, error)
	Marshal(io.Writer) error
	UnmarshalFromBuf([]byte) error
	Encoding() Encoding
	// Rebound returns a smaller chunk that includes all samples between start and end (inclusive).
	// We do not want to change existing Slice implementations because
	// it is built specifically for query optimization and is a noop for some of the encodings.
	Rebound(start, end model.Time) (ChunkData, error)
	// Size returns the approximate length of the chunk in bytes.
	Size() int
}
