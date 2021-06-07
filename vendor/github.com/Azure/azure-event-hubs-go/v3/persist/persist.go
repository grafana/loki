// Package persist provides abstract structures for checkpoint persistence.
package persist

//	MIT License
//
//	Copyright (c) Microsoft Corporation. All rights reserved.
//
//	Permission is hereby granted, free of charge, to any person obtaining a copy
//	of this software and associated documentation files (the "Software"), to deal
//	in the Software without restriction, including without limitation the rights
//	to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
//	copies of the Software, and to permit persons to whom the Software is
//	furnished to do so, subject to the following conditions:
//
//	The above copyright notice and this permission notice shall be included in all
//	copies or substantial portions of the Software.
//
//	THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
//	IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
//	FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
//	AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
//	LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
//	OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
//	SOFTWARE

import (
	"fmt"
	"path"
	"sync"
)

type (
	// CheckpointPersister provides persistence for the received offset for a given namespace, hub name, consumer group, partition Id and
	// offset so that if a receiver where to be interrupted, it could resume after the last consumed event.
	CheckpointPersister interface {
		Write(namespace, name, consumerGroup, partitionID string, checkpoint Checkpoint) error
		Read(namespace, name, consumerGroup, partitionID string) (Checkpoint, error)
	}

	// MemoryPersister is a default implementation of a Hub CheckpointPersister, which will persist offset information in
	// memory.
	MemoryPersister struct {
		values map[string]Checkpoint
		mu     sync.Mutex
	}
)

// NewMemoryPersister creates a new in-memory storage for checkpoints
//
// MemoryPersister is only intended to be shared with EventProcessorHosts within the same process. This implementation
// is a toy. You should probably use the Azure Storage implementation or any other that provides durable storage for
// checkpoints.
func NewMemoryPersister() *MemoryPersister {
	return &MemoryPersister{
		values: make(map[string]Checkpoint),
	}
}

func (p *MemoryPersister) Write(namespace, name, consumerGroup, partitionID string, checkpoint Checkpoint) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := getPersistenceKey(namespace, name, consumerGroup, partitionID)
	p.values[key] = checkpoint
	return nil
}

func (p *MemoryPersister) Read(namespace, name, consumerGroup, partitionID string) (Checkpoint, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := getPersistenceKey(namespace, name, consumerGroup, partitionID)
	if offset, ok := p.values[key]; ok {
		return offset, nil
	}
	return NewCheckpointFromStartOfStream(), fmt.Errorf("could not read the offset for the key %s", key)
}

func getPersistenceKey(namespace, name, consumerGroup, partitionID string) string {
	return path.Join(namespace, name, consumerGroup, partitionID)
}
