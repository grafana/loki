//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.

package shared

import (
	"fmt"
	"sync"
)

const _1MiB = 1024 * 1024

// TransferManager provides a buffer and thread pool manager for certain transfer options.
// It is undefined behavior if code outside this package call any of these methods.
type TransferManager interface {
	// Get provides a buffer that will be used to read data into and write out to the stream.
	// It is guaranteed by this package to not read or write beyond the size of the slice.
	Get() []byte

	// Put may or may not put the buffer into underlying storage, depending on settings.
	// The buffer must not be touched after this has been called.
	Put(b []byte) // nolint

	// Run will use a goroutine pool entry to run a function. This blocks until a pool
	// goroutine becomes available.
	Run(func())

	// Close shuts down all internal goroutines. This must be called when the TransferManager
	// will no longer be used. Not closing it will cause a goroutine leak.
	Close()
}

// ---------------------------------------------------------------------------------------------------------------------

type staticBuffer struct {
	buffers    chan []byte
	size       int
	threadpool chan func()
}

// NewStaticBuffer creates a TransferManager that will use a channel as a circular buffer
// that can hold "max" buffers of "size". The goroutine pool is also sized at max. This
// can be shared between calls if you wish to control maximum memory and concurrency with
// multiple concurrent calls.
func NewStaticBuffer(size, max int) (TransferManager, error) {
	if size < 1 || max < 1 {
		return nil, fmt.Errorf("cannot be called with size or max set to < 1")
	}

	if size < _1MiB {
		return nil, fmt.Errorf("cannot have size < 1MiB")
	}

	threadpool := make(chan func(), max)
	buffers := make(chan []byte, max)
	for i := 0; i < max; i++ {
		go func() {
			for f := range threadpool {
				f()
			}
		}()

		buffers <- make([]byte, size)
	}
	return staticBuffer{
		buffers:    buffers,
		size:       size,
		threadpool: threadpool,
	}, nil
}

// Get implements TransferManager.Get().
func (s staticBuffer) Get() []byte {
	return <-s.buffers
}

// Put implements TransferManager.Put().
func (s staticBuffer) Put(b []byte) { // nolint
	select {
	case s.buffers <- b:
	default: // This shouldn't happen, but just in case they call Put() with there own buffer.
	}
}

// Run implements TransferManager.Run().
func (s staticBuffer) Run(f func()) {
	s.threadpool <- f
}

// Close implements TransferManager.Close().
func (s staticBuffer) Close() {
	close(s.threadpool)
	close(s.buffers)
}

// ---------------------------------------------------------------------------------------------------------------------

type syncPool struct {
	threadpool chan func()
	pool       sync.Pool
}

// NewSyncPool creates a TransferManager that will use a sync.Pool
// that can hold a non-capped number of buffers constrained by concurrency. This
// can be shared between calls if you wish to share memory and concurrency.
func NewSyncPool(size, concurrency int) (TransferManager, error) {
	if size < 1 || concurrency < 1 {
		return nil, fmt.Errorf("cannot be called with size or max set to < 1")
	}

	if size < _1MiB {
		return nil, fmt.Errorf("cannot have size < 1MiB")
	}

	threadpool := make(chan func(), concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			for f := range threadpool {
				f()
			}
		}()
	}

	return &syncPool{
		threadpool: threadpool,
		pool: sync.Pool{
			New: func() interface{} {
				return make([]byte, size)
			},
		},
	}, nil
}

// Get implements TransferManager.Get().
func (s *syncPool) Get() []byte {
	return s.pool.Get().([]byte)
}

// Put implements TransferManager.Put().
// nolint
func (s *syncPool) Put(b []byte) {
	s.pool.Put(b)
}

// Run implements TransferManager.Run().
func (s *syncPool) Run(f func()) {
	s.threadpool <- f
}

// Close implements TransferManager.Close().
func (s *syncPool) Close() {
	close(s.threadpool)
}
