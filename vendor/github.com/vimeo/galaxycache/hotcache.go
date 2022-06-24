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

package galaxycache

import (
	"math"
	"sync"
	"time"

	"github.com/vimeo/galaxycache/promoter"
)

// update the hotcache stats if at least one second has passed since
// last update
func (g *Galaxy) maybeUpdateHotCacheStats() {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	if now.Sub(g.hcStatsWithTime.t) < time.Second {
		return
	}
	mruEleQPS := 0.0
	lruEleQPS := 0.0
	mruEle := g.hotCache.lru.MostRecent()
	lruEle := g.hotCache.lru.LeastRecent()
	if mruEle != nil { // lru contains at least one element
		mruEleQPS = mruEle.(*valWithStat).stats.val()
		lruEleQPS = lruEle.(*valWithStat).stats.val()
	}

	newHCS := &promoter.HCStats{
		MostRecentQPS:  mruEleQPS,
		LeastRecentQPS: lruEleQPS,
		HCSize:         (g.cacheBytes / g.opts.hcRatio) - g.hotCache.bytes(),
		HCCapacity:     g.cacheBytes / g.opts.hcRatio,
	}
	g.hcStatsWithTime.hcs = newHCS
	g.hcStatsWithTime.t = now
}

// keyStats keeps track of the hotness of a key
type keyStats struct {
	dQPS dampedQPS
}

func newValWithStat(data []byte, kStats *keyStats) *valWithStat {
	if kStats == nil {
		kStats = &keyStats{dampedQPS{period: time.Second}}
	}

	return &valWithStat{
		data:  data,
		stats: kStats,
	}
}

func (k *keyStats) val() float64 {
	return k.dQPS.val(time.Now())
}

func (k *keyStats) touch() {
	k.dQPS.touch(time.Now())
}

// dampedQPS is an average that recombines the current state with the previous.
type dampedQPS struct {
	mu      sync.Mutex
	period  time.Duration
	t       time.Time
	curDQPS float64
	count   float64
}

// must be between 0 and 1, the fraction of the new value that comes from
// current rather than previous.
// if `samples` is the number of samples into the damped weighted average you
// want to maximize the fraction of the contribution after; x is the damping
// constant complement (this way we don't have to multiply out (1-x) ^ samples)
// f(x) = (1 - x) * x ^ samples = x ^samples - x ^(samples + 1)
// f'(x) = samples * x ^ (samples - 1) - (samples + 1) * x ^ samples
// this yields a critical point at x = (samples - 1) / samples
const dampingConstant = (1.0 / 30.0) // 30 seconds (30 samples at a 1s interval)
const dampingConstantComplement = 1.0 - dampingConstant

func (d *dampedQPS) touch(now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.maybeFlush(now)
	d.count++
}

// d.mu must be held when calling maybeFlush (otherwise racy)
func (d *dampedQPS) maybeFlush(now time.Time) {
	if d.t.IsZero() {
		d.t = now
		return
	}
	if now.Sub(d.t) < d.period {
		return
	}
	curDQPS, cur := d.curDQPS, d.count
	exponent := math.Floor(float64(now.Sub(d.t))/float64(d.period)) - 1
	d.curDQPS = ((dampingConstant * cur) + (dampingConstantComplement * curDQPS)) * math.Pow(dampingConstantComplement, exponent)
	d.count = 0
	d.t = now
}

func (d *dampedQPS) val(now time.Time) float64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.maybeFlush(now)
	return d.curDQPS
}

func (g *Galaxy) addNewToCandidateCache(key string) *keyStats {
	kStats := &keyStats{
		dQPS: dampedQPS{
			period: time.Second,
		},
	}

	g.candidateCache.addToCandidateCache(key, kStats)
	return kStats
}

func (c *cache) addToCandidateCache(key string, kStats *keyStats) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lru.Add(key, kStats)
}
