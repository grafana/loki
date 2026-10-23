// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package experimental

import (
	"context"
)

// BufferPool is not supported at the moment.
type BufferPool interface {
	// Get retrieves a single chunk of memory. It returns a slice that is
	// optimally sized by the pool, guaranteeing that len(buf) <= maxSize.
	// It blocks until the request is satisfied.
	// It returns an error if the context is cancelled (e.g., context.Canceled)
	// or if the underlying allocation encounters a fatal failure.
	Get(ctx context.Context, maxSize int) ([]byte, error)

	// TryGet retrieves a chunk of memory up to maxSize bytes.
	// It is non-blocking and either returns a memory chunk or fails instantly
	// by throwing an error.
	TryGet(maxSize int) ([]byte, error)

	// Put returns a previously acquired buffer to the pool.
	// After calling Put, the caller must not retain, read, or write to the buffer.
	// The exact slice returned by Get or TryGet must be passed back.
	Put(buf []byte)
}
