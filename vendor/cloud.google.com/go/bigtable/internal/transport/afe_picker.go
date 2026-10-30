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
	"math/rand/v2"
)

// defaultAfeRandomSubsetSize is the default K for K-choice random draws
// in LeastInFlightAfePicker / LeastLatencyAfePicker when the caller
// doesn't specify one. Two candidates ("power of two choices") is the
// standard K-choice draw size.
const defaultAfeRandomSubsetSize = 2

// PickCandidate is one AFE the picker considered during a K-choice draw,
// with the cost value the picker's decision rule used to score it.
// Cost's interpretation depends on the picker in play: NumOutstanding
// (int as float) for LeastInFlight, e2e PeakEwma nanos for LeastLatency,
// 0 for Simple (which ignores cost).
type PickCandidate struct {
	AfeID AfeID
	Cost  float64
}

// PickDecision captures what candidates a picker sampled, which one won,
// and why. Populated on every PickAfe call so operators can trace picker
// reasoning through the debug surface without re-running the pick.
type PickDecision struct {
	// Candidates is the K sampled AFEs (K == 1 for SimplePicker,
	// otherwise K == min(RandomSubsetSize, len(ready)) via partial
	// Fisher-Yates). Empty when no ready AFE existed.
	Candidates []PickCandidate
	// Winner is the AFE the picker returned. Zero when ready was empty.
	Winner AfeID
	// Reason is a short lower-kebab tag identifying the decision rule
	// ("uniform-random" / "min-inflight" / "min-latency" /
	// "no-candidates"). Machine-readable; consumers map to prose.
	Reason string
}

// AfePicker picks one AFE from a snapshot of ready buckets AND returns
// the decision metadata for the debug surface. The winner travels back
// into the producer's Checkout by id — no producer-owned pointer ever
// leaves the producer's lock. The PickDecision is passed to the pool's
// pick-decision recorder.
//
// picked == false means "no AFE eligible" — the pool treats that the
// same as len(ready) == 0 (park the caller, kick scale-up).
//
// Implementations MAY mutate ready in place; callers must pass a
// throwaway slice.
type AfePicker interface {
	PickAfe(ready []AfeSnapshot) (winner AfeID, picked bool, decision PickDecision)
	Name() string
}

// SimpleAfePicker chooses an AFE uniformly at random from the ready set.
type SimpleAfePicker struct {
	// recordCandidates gates PickDecision.Candidates construction.
	// Owned by SessionPoolImpl at construction; matches
	// SessionPoolImpl.debugEnabled. When false, PickDecision returns
	// Winner+Reason only — Candidates stays nil so the per-pick slice
	// allocation is skipped.
	recordCandidates bool
}

// NewSimpleAfePicker constructs a SimpleAfePicker.
func NewSimpleAfePicker(recordCandidates bool) *SimpleAfePicker {
	return &SimpleAfePicker{recordCandidates: recordCandidates}
}

// Name returns "simple".
func (SimpleAfePicker) Name() string { return "simple" }

// PickAfe uniformly-at-random picks one bucket from ready.
func (p SimpleAfePicker) PickAfe(ready []AfeSnapshot) (AfeID, bool, PickDecision) {
	if len(ready) == 0 {
		return 0, false, PickDecision{Reason: "no-candidates"}
	}
	winner := ready[rand.IntN(len(ready))]
	d := PickDecision{Winner: winner.ID, Reason: "uniform-random"}
	if p.recordCandidates {
		d.Candidates = []PickCandidate{{AfeID: winner.ID, Cost: 0}}
	}
	return winner.ID, true, d
}

// LeastInFlightAfePicker picks the AFE with the smallest in-flight count.
// Draws K distinct candidates via partial Fisher-Yates over the ready
// snapshot and returns the min-cost one. RandomSubsetSize caps K; when
// it's <=0 or >= len(ready) every candidate is considered.
type LeastInFlightAfePicker struct {
	// RandomSubsetSize caps the K-choice draw. 0 or negative means
	// "consider all candidates".
	RandomSubsetSize int
	// recordCandidates: see SimpleAfePicker.recordCandidates.
	recordCandidates bool
}

// NewLeastInFlightAfePicker constructs a LeastInFlightAfePicker.
func NewLeastInFlightAfePicker(randomSubsetSize int, recordCandidates bool) *LeastInFlightAfePicker {
	return &LeastInFlightAfePicker{RandomSubsetSize: randomSubsetSize, recordCandidates: recordCandidates}
}

// Name returns "least-inflight".
func (LeastInFlightAfePicker) Name() string { return "least-inflight" }

// PickAfe returns the AFE with the fewest NumOutstanding among K
// randomly-drawn ready candidates.
func (p LeastInFlightAfePicker) PickAfe(ready []AfeSnapshot) (AfeID, bool, PickDecision) {
	winner, picked, cands := kChoiceMinCost(ready, p.RandomSubsetSize, p.recordCandidates, func(s AfeSnapshot) float64 {
		return float64(s.NumOutstanding)
	})
	return decisionFor(winner, picked, cands, "min-inflight")
}

// LeastLatencyAfePicker picks the AFE with the lowest per-AFE e2e
// PeakEwma cost. Same K-choice partial Fisher-Yates as
// LeastInFlightAfePicker.
type LeastLatencyAfePicker struct {
	RandomSubsetSize int
	// recordCandidates: see SimpleAfePicker.recordCandidates.
	recordCandidates bool
}

// NewLeastLatencyAfePicker constructs a LeastLatencyAfePicker.
func NewLeastLatencyAfePicker(randomSubsetSize int, recordCandidates bool) *LeastLatencyAfePicker {
	return &LeastLatencyAfePicker{RandomSubsetSize: randomSubsetSize, recordCandidates: recordCandidates}
}

// Name returns "least-latency".
func (LeastLatencyAfePicker) Name() string { return "least-latency" }

// PickAfe returns the AFE with the smallest E2eCost among K randomly-
// drawn ready candidates.
func (p LeastLatencyAfePicker) PickAfe(ready []AfeSnapshot) (AfeID, bool, PickDecision) {
	winner, picked, cands := kChoiceMinCost(ready, p.RandomSubsetSize, p.recordCandidates, func(s AfeSnapshot) float64 {
		return s.E2eCost
	})
	return decisionFor(winner, picked, cands, "min-latency")
}

// decisionFor packages kChoiceMinCost's return into a PickDecision.
func decisionFor(winner AfeID, picked bool, cands []PickCandidate, reason string) (AfeID, bool, PickDecision) {
	if !picked {
		return 0, false, PickDecision{Reason: "no-candidates"}
	}
	return winner, true, PickDecision{
		Candidates: cands,
		Winner:     winner,
		Reason:     reason,
	}
}

// kChoiceMinCost implements partial-Fisher-Yates + min-cost selection
// over a snapshot slice. K is clamped to len(ready); K<=0 means scan
// every candidate.
//
// The algorithm mutates ready in place (swap-to-front). Callers must
// pass a throwaway slice — every production call site produces one via
// the producer's snapshot method, which allocates a fresh copy per call.
// cost is invoked at most K times. Returns the winner plus the list of
// sampled candidates (with their costs) so callers can build a
// PickDecision for the debug surface.
//
// The previous implementation allocated a defensive copy of ready per
// call; profiling showed it costing ~4µs at the workload's steady-state
// QPS since the picker runs on every CheckoutSession. Removed because
// the caller doesn't need ready preserved.
func kChoiceMinCost(ready []AfeSnapshot, k int, recordCandidates bool, cost func(AfeSnapshot) float64) (AfeID, bool, []PickCandidate) {
	n := len(ready)
	if n == 0 {
		return 0, false, nil
	}
	if k <= 0 || k > n {
		k = n
	}

	// Only allocate the sampled slice when the caller (via
	// recordCandidates) will actually keep it. When debug is off this
	// per-pick allocation is the biggest single win — pickAfe runs on
	// every CheckoutSession.
	var sampled []PickCandidate
	if recordCandidates {
		sampled = make([]PickCandidate, 0, k)
	}
	var best AfeID
	haveBest := false
	bestCost := -1.0
	for i := 0; i < k; i++ {
		j := i + rand.IntN(n-i)
		s := ready[j]
		c := cost(s)
		if recordCandidates {
			sampled = append(sampled, PickCandidate{AfeID: s.ID, Cost: c})
		}
		if !haveBest || c < bestCost {
			bestCost = c
			best = s.ID
			haveBest = true
		}
		ready[i], ready[j] = ready[j], ready[i]
	}
	return best, haveBest, sampled
}
