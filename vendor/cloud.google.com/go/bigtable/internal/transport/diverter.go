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
	"math/rand/v2"
	"sync/atomic"
)

// Diverter decides whether to use the session-based protocol or classic protocol.
type Diverter struct {
	sessionLoadBits atomic.Uint64
	// sessionPicks / classicPicks tally every UseSession() outcome so the
	// debug UI can report the actual ratio of traffic landing on each path
	// — useful when the configured SessionLoad doesn't match what shipped
	// (e.g. mid-rollout or after a control-plane override).
	sessionPicks atomic.Int64
	classicPicks atomic.Int64
}

// NewDiverter creates a new Diverter with the given session load ratio (0.0 to 1.0).
func NewDiverter(sessionLoad float64) *Diverter {
	d := &Diverter{}
	d.sessionLoadBits.Store(math.Float64bits(sessionLoad))
	return d
}

// SetSessionLoad updates the session load ratio.
func (d *Diverter) SetSessionLoad(load float64) {
	d.sessionLoadBits.Store(math.Float64bits(load))
}

// SessionLoad returns the current target session-protocol fraction.
func (d *Diverter) SessionLoad() float64 {
	return math.Float64frombits(d.sessionLoadBits.Load())
}

// UseSession returns true if the next call should use the session protocol.
func (d *Diverter) UseSession() bool {
	load := math.Float64frombits(d.sessionLoadBits.Load())
	var use bool
	switch {
	case load <= 0:
		use = false
	case load >= 1:
		use = true
	default:
		use = rand.Float64() <= load
	}
	if use {
		d.sessionPicks.Add(1)
	} else {
		d.classicPicks.Add(1)
	}
	return use
}

// DiverterSnapshot is what the debug UI surfaces for the diverter.
type DiverterSnapshot struct {
	SessionLoad  float64
	SessionPicks int64
	ClassicPicks int64
}

// Snapshot returns the diverter's current target SessionLoad plus the
// running totals of pick outcomes.
func (d *Diverter) Snapshot() DiverterSnapshot {
	return DiverterSnapshot{
		SessionLoad:  d.SessionLoad(),
		SessionPicks: d.sessionPicks.Load(),
		ClassicPicks: d.classicPicks.Load(),
	}
}
