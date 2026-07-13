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

// UseSession returns true if the next call should use the session protocol.
func (d *Diverter) UseSession() bool {
	load := math.Float64frombits(d.sessionLoadBits.Load())
	if load <= 0 {
		return false
	}
	if load >= 1 {
		return true
	}
	return rand.Float64() <= load
}
