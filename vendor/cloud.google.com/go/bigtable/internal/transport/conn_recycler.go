// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package internal

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"

	btopt "cloud.google.com/go/bigtable/internal/option"
)

// maxRecyclePerBatch limits the number of connections we replace in a single pass.
const maxRecyclePerBatch = 2

// ConnectionRecycler monitors connection age and recycles them to prevent long-lived connections.
type ConnectionRecycler struct {
	pool     *BigtableChannelPool
	config   btopt.ConnectionRecycleConfig
	ticker   *time.Ticker
	done     chan struct{}
	stopOnce sync.Once
}

// NewConnectionRecycler creates a new recycler with the provided configuration.
func NewConnectionRecycler(config btopt.ConnectionRecycleConfig, pool *BigtableChannelPool) *ConnectionRecycler {
	return &ConnectionRecycler{
		pool:   pool,
		config: config,
		done:   make(chan struct{}),
	}
}

// Start begins the periodic monitoring.
func (cr *ConnectionRecycler) Start(ctx context.Context) {
	btopt.Debugf(cr.pool.logger, "bigtable_connpool: ConnectionRecyler starting...")

	// default to 1 minute
	freq := cr.config.RunFrequency
	if freq < 1*time.Minute {
		freq = 1 * time.Minute
	}

	// at least once per MaxAge interval.
	if cr.config.MaxAge > 0 && freq > cr.config.MaxAge {
		freq = cr.config.MaxAge
	}

	cr.ticker = time.NewTicker(freq)
	go func() {
		defer cr.ticker.Stop()
		for {
			select {
			case <-cr.ticker.C:
				cr.checkRecycle()
			case <-cr.done:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop terminates the ConnectionRecycler.
func (cr *ConnectionRecycler) Stop() {
	cr.stopOnce.Do(func() {
		close(cr.done)
	})
}

// background period task
func (cr *ConnectionRecycler) checkRecycle() {
	conns := cr.pool.getConns()
	recycledCount := 0

	hasJitter := cr.config.MaxJitter > 0
	jitterVal := int64(cr.config.MaxJitter)

	for _, entry := range conns {
		if recycledCount >= maxRecyclePerBatch {
			btopt.Debugf(cr.pool.logger, "bigtable_connpool: Hit max recycle cap (%d) for this round", maxRecyclePerBatch)
			break
		}

		createdAt := time.UnixMilli(entry.createdAt())
		age := time.Since(createdAt)

		var currentJitter time.Duration
		if hasJitter {
			currentJitter = time.Duration(rand.Int64N(jitterVal))
		}

		if age > cr.config.MaxAge+currentJitter {
			btopt.Debugf(cr.pool.logger, "bigtable_connpool: Recycling connection age %v > %v + %v", age, cr.config.MaxAge, currentJitter)
			cr.pool.replaceConnection(entry)
			recycledCount++
		}
	}
}
