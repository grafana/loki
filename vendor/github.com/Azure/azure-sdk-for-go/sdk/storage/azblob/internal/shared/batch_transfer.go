// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.

package shared

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
)

const (
	// DefaultConcurrency is the legacy default concurrency value.
	//
	// Deprecated: Use DefaultConcurrencyValue() instead, which returns a value based on CPU core count.
	DefaultConcurrency = 5
)

// DefaultConcurrencyValue returns the default concurrency based on CPU count,
// clamped between 8 and 96. Set the AZURE_STORAGE_USE_LEGACY_DEFAULT_CONCURRENCY
// environment variable to "true" to revert to the previous default of 5.
func DefaultConcurrencyValue() uint16 {
	if strings.EqualFold(os.Getenv("AZURE_STORAGE_USE_LEGACY_DEFAULT_CONCURRENCY"), "true") {
		return DefaultConcurrency
	}
	return uint16(max(8, min(96, runtime.NumCPU())))
}

// DefaultStreamConcurrencyValue returns the default concurrency for streaming
// uploads. Set the AZURE_STORAGE_USE_LEGACY_DEFAULT_CONCURRENCY environment
// variable to "true" to revert to the previous default of 1.
func DefaultStreamConcurrencyValue() uint16 {
	if strings.EqualFold(os.Getenv("AZURE_STORAGE_USE_LEGACY_DEFAULT_CONCURRENCY"), "true") {
		return 1
	}
	return uint16(max(8, min(96, runtime.NumCPU())))
}

// BatchTransferOptions identifies options used by doBatchTransfer.
type BatchTransferOptions struct {
	TransferSize  int64
	ChunkSize     int64
	NumChunks     uint64
	Concurrency   uint16
	Operation     func(ctx context.Context, offset int64, chunkSize int64) error
	OperationName string
}

// DoBatchTransfer helps to execute operations in a batch manner.
// Can be used by users to customize batch works (for other scenarios that the SDK does not provide)
func DoBatchTransfer(ctx context.Context, o *BatchTransferOptions) error {
	if o.ChunkSize == 0 {
		return errors.New("ChunkSize cannot be 0")
	}

	if o.Concurrency == 0 {
		o.Concurrency = DefaultConcurrencyValue()
	}

	// Prepare and do parallel operations.
	operationChannel := make(chan func() error, o.Concurrency) // Create the channel that release 'concurrency' goroutines concurrently
	operationResponseChannel := make(chan error, o.NumChunks)  // Holds each response
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Create the goroutines that process each operation (in parallel).
	for g := uint16(0); g < o.Concurrency; g++ {
		go func() {
			for f := range operationChannel {
				err := f()
				operationResponseChannel <- err
			}
		}()
	}

	// Add each chunk's operation to the channel.
	for chunkNum := uint64(0); chunkNum < o.NumChunks; chunkNum++ {
		curChunkSize := o.ChunkSize

		if chunkNum == o.NumChunks-1 { // Last chunk
			curChunkSize = o.TransferSize - (int64(chunkNum) * o.ChunkSize) // Remove size of all transferred chunks from total
		}
		offset := int64(chunkNum) * o.ChunkSize
		operationChannel <- func() error {
			return o.Operation(ctx, offset, curChunkSize)
		}
	}
	close(operationChannel)

	// Wait for the operations to complete.
	var firstErr error = nil
	for chunkNum := uint64(0); chunkNum < o.NumChunks; chunkNum++ {
		responseError := <-operationResponseChannel
		// record the first error (the original error which should cause the other chunks to fail with canceled context)
		if responseError != nil && firstErr == nil {
			cancel() // As soon as any operation fails, cancel all remaining operation calls
			firstErr = responseError
		}
	}
	return firstErr
}
