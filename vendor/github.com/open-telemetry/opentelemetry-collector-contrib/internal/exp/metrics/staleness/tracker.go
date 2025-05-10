// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package staleness // import "github.com/open-telemetry/opentelemetry-collector-contrib/internal/exp/metrics/staleness"

import (
	"time"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/exp/metrics/identity"
)

type Tracker struct {
	pq PriorityQueue
}

func NewTracker() Tracker {
	return Tracker{pq: NewPriorityQueue()}
}

func (tr Tracker) Refresh(ts time.Time, ids ...identity.Stream) {
	for _, id := range ids {
		tr.pq.Update(id, ts)
	}
}

func (tr Tracker) Collect(maxDuration time.Duration) []identity.Stream {
	now := time.Now()

	var ids []identity.Stream
	for tr.pq.Len() > 0 {
		_, ts := tr.pq.Peek()
		if now.Sub(ts) < maxDuration {
			break
		}
		id, _ := tr.pq.Pop()
		ids = append(ids, id)
	}

	return ids
}
