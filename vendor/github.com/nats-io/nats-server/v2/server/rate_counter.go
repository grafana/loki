// Copyright 2021-2022 The NATS Authors
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

package server

import (
	"sync"
	"time"
)

type rateCounter struct {
	limit    int64
	count    int64
	blocked  uint64
	end      time.Time
	interval time.Duration
	mu       sync.Mutex
}

func newRateCounter(limit int64) *rateCounter {
	return &rateCounter{
		limit:    limit,
		interval: time.Second,
	}
}

func (r *rateCounter) allow() bool {
	now := time.Now()

	r.mu.Lock()

	if now.After(r.end) {
		r.count = 0
		r.end = now.Add(r.interval)
	} else {
		r.count++
	}
	allow := r.count < r.limit
	if !allow {
		r.blocked++
	}

	r.mu.Unlock()

	return allow
}

func (r *rateCounter) countBlocked() uint64 {
	r.mu.Lock()
	blocked := r.blocked
	r.blocked = 0
	r.mu.Unlock()

	return blocked
}
