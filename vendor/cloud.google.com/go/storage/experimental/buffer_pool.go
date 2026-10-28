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

// BufferPool is not supported at the moment.
type BufferPool interface {
	// Get retrieves a chunk of memory up to maxSize bytes, regardless of any
	// pool constraints. It is non-blocking and returns an error only if
	// allocation fails. Callers should use Get for the first buffer of each
	// upload to guarantee progress.
	Get(maxSize int) ([]byte, error)

	// TryGet retrieves a chunk of memory up to maxSize bytes, subject to the
	// pool's constraints. It is non-blocking. It returns false if a buffer
	// cannot be provided at this time, and an error if allocation fails.
	TryGet(maxSize int) ([]byte, bool, error)

	// Put returns a previously acquired buffer to the pool.
	// After calling Put, the caller must not retain, read, or write to the buffer.
	// The exact slice returned by Get or TryGet must be passed back.
	Put(buf []byte)
}
