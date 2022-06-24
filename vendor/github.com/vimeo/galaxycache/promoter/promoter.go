/*
Copyright 2019 Vimeo Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

     http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package promoter

import "math/rand"

// HCStats keeps track of the size, capacity, and coldest/hottest
// elements in the hot cache
type HCStats struct {
	MostRecentQPS  float64
	LeastRecentQPS float64
	HCSize         int64
	HCCapacity     int64
}

// Stats contains both the KeyQPS and a pointer to the galaxy-wide
// HCStats
type Stats struct {
	KeyQPS  float64
	HCStats *HCStats
}

// Interface is the interface for determining whether a key/value pair should be
// added to the hot cache
type Interface interface {
	ShouldPromote(key string, data []byte, stats Stats) bool
}

// Func implements Promoter with a function.
type Func func(key string, data []byte, stats Stats) bool

// ShouldPromote returns true if the given key/data pair has been chosen to
// add to the hotcache
func (f Func) ShouldPromote(key string, data []byte, stats Stats) bool {
	return f(key, data, stats)
}

// ProbabilisticPromoter promotes based on a 1/ProbDenominator chance
type ProbabilisticPromoter struct {
	ProbDenominator int
}

// ShouldPromote for a ProbabilisticPromoter promotes based on a
// 1/ProbDenominator chance
func (p *ProbabilisticPromoter) ShouldPromote(key string, data []byte, stats Stats) bool {
	return rand.Intn(p.ProbDenominator) == 0
}

// DefaultPromoter promotes if the given key QPS is higher than the QPS
// of the least recently accessed element in the hotcache
type DefaultPromoter struct{}

// ShouldPromote for a DefaultPromoter promotes if the given key QPS
// is higher than the QPS of the least recently accessed element in
// the hotcache
func (p *DefaultPromoter) ShouldPromote(key string, data []byte, stats Stats) bool {
	return stats.KeyQPS >= stats.HCStats.LeastRecentQPS
}
