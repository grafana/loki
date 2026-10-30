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

package internal

import (
	"math"
	"sync"
	"time"
)

// PeakEwma is a thread-safe latency tracker that rises immediately on
// higher samples (the "peak" step) and decays exponentially toward
// lower ones (the EWMA step). The AFE picker uses one instance per
// bucket to score candidates; see sessionList.
//
// The peak-step is what prevents a new AFE from stealing traffic just
// because its latest vRPC happened to be fast — a plain symmetric
// EWMA would blend a single low sample into the average, briefly
// making the bucket look cheapest and steering pickers at it.
type PeakEwma struct {
	mu         sync.Mutex
	tau        time.Duration
	value      float64
	lastUpdate time.Time
}

// NewPeakEwma creates a PeakEwma with the given decay time constant tau.
// The initial value is zero — the first positive Update peak-snaps to
// its sample. lastUpdate is stamped at construction so subsequent
// updates see a valid dt.
func NewPeakEwma(tau time.Duration) *PeakEwma {
	return &PeakEwma{
		tau:        tau,
		lastUpdate: time.Now(),
	}
}

// NewPeakEwmaSeeded returns a PeakEwma pre-seeded with the given cost.
// The seed participates in the first Update's decay/blend normally —
// it is NOT overwritten on the first sample. SessionList seeds
// transport at 500µs and e2e at 1ms so a brand-new AFE doesn't win
// the least-latency picker by looking free-cost.
func NewPeakEwmaSeeded(tau, seed time.Duration) *PeakEwma {
	return &PeakEwma{
		tau:        tau,
		value:      float64(seed),
		lastUpdate: time.Now(),
	}
}

// Update folds a new latency sample into the tracker. Two distinct
// steps:
//
//   - Peak-step: if the sample is higher than the current value, snap
//     up immediately. No decay, no blend — the higher observation
//     supersedes.
//   - Decay-step: otherwise, apply e^(-dt/tau) time-decay to the
//     current value and blend the sample in proportionally.
//
// Non-positive samples are ignored; a backward clock (dt < 0) is
// clamped to 0 so the blend weight stays in [0,1]; a non-positive tau
// collapses to zero decay (new sample fully replaces) so tau=0/dt=0
// doesn't produce NaN via exp(-0/0).
func (e *PeakEwma) Update(latency time.Duration) {
	if latency <= 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now()
	latencyNs := float64(latency)
	if e.value < latencyNs {
		e.value = latencyNs
		e.lastUpdate = now
		return
	}
	dt := now.Sub(e.lastUpdate)
	if dt < 0 {
		dt = 0
	}
	e.lastUpdate = now
	var decay float64
	if e.tau > 0 {
		decay = math.Exp(-float64(dt) / float64(e.tau))
	}
	e.value = e.value*decay + latencyNs*(1-decay)
}

// Value returns the current tracker value in the same units as the
// samples fed into Update (nanoseconds, as float64 — the picker
// consumes this raw).
func (e *PeakEwma) Value() float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.value
}
