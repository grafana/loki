package index

import (
	"sort"
	"sync"
	"unsafe"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/cortexpb"
	"github.com/cortexproject/cortex/pkg/util"
)

const indexShards = 32

// InvertedIndex implements a in-memory inverter index from label pairs to fingerprints.
// It is sharded to reduce lock contention on writes.
type InvertedIndex struct {
	shards []indexShard
}

// New returns a new InvertedIndex.
func New() *InvertedIndex {
	shards := make([]indexShard, indexShards)
	for i := 0; i < indexShards; i++ {
		shards[i].idx = map[string]indexEntry{}
	}
	return &InvertedIndex{
		shards: shards,
	}
}

// Add a fingerprint under the specified labels.
// NOTE: memory for `labels` is unsafe; anything retained beyond the
// life of this function must be copied
func (ii *InvertedIndex) Add(labels []cortexpb.LabelAdapter, fp model.Fingerprint) labels.Labels {
	shard := &ii.shards[util.HashFP(fp)%indexShards]
	return shard.add(labels, fp) // add() returns 'interned' values so the original labels are not retained
}

// Lookup all fingerprints for the provided matchers.
func (ii *InvertedIndex) Lookup(matchers []*labels.Matcher) []model.Fingerprint {
	if len(matchers) == 0 {
		return nil
	}

	result := []model.Fingerprint{}
	for i := range ii.shards {
		fps := ii.shards[i].lookup(matchers)
		result = append(result, fps...)
	}

	return result
}

// LabelNames returns all label names.
func (ii *InvertedIndex) LabelNames() []string {
	results := make([][]string, 0, indexShards)

	for i := range ii.shards {
		shardResult := ii.shards[i].labelNames()
		results = append(results, shardResult)
	}

	return mergeStringSlices(results)
}

// LabelValues returns the values for the given label.
func (ii *InvertedIndex) LabelValues(name string) []string {
	results := make([][]string, 0, indexShards)

	for i := range ii.shards {
		shardResult := ii.shards[i].labelValues(name)
		results = append(results, shardResult)
	}

	return mergeStringSlices(results)
}

// Delete a fingerprint with the given label pairs.
func (ii *InvertedIndex) Delete(labels labels.Labels, fp model.Fingerprint) {
	shard := &ii.shards[util.HashFP(fp)%indexShards]
	shard.delete(labels, fp)
}

// NB slice entries are sorted in fp order.
type indexEntry struct {
	name string
	fps  map[string]indexValueEntry
}

type indexValueEntry struct {
	value string
	fps   []model.Fingerprint
}

type unlockIndex map[string]indexEntry

// This is the prevalent value for Intel and AMD CPUs as-at 2018.
const cacheLineSize = 64

type indexShard struct {
	mtx sync.RWMutex
	idx unlockIndex
	//nolint:structcheck,unused
	pad [cacheLineSize - unsafe.Sizeof(sync.Mutex{}) - unsafe.Sizeof(unlockIndex{})]byte
}

func copyString(s string) string {
	return string([]byte(s))
}

// add metric to the index; return all the name/value pairs as a fresh
// sorted slice, referencing 'interned' strings from the index so that
// no references are retained to the memory of `metric`.
func (shard *indexShard) add(metric []cortexpb.LabelAdapter, fp model.Fingerprint) labels.Labels {
	shard.mtx.Lock()
	defer shard.mtx.Unlock()

	internedLabels := make(labels.Labels, len(metric))

	for i, pair := range metric {
		values, ok := shard.idx[pair.Name]
		if !ok {
			values = indexEntry{
				name: copyString(pair.Name),
				fps:  map[string]indexValueEntry{},
			}
			shard.idx[values.name] = values
		}
		fingerprints, ok := values.fps[pair.Value]
		if !ok {
			fingerprints = indexValueEntry{
				value: copyString(pair.Value),
			}
		}
		// Insert into the right position to keep fingerprints sorted
		j := sort.Search(len(fingerprints.fps), func(i int) bool {
			return fingerprints.fps[i] >= fp
		})
		fingerprints.fps = append(fingerprints.fps, 0)
		copy(fingerprints.fps[j+1:], fingerprints.fps[j:])
		fingerprints.fps[j] = fp
		values.fps[fingerprints.value] = fingerprints
		internedLabels[i] = labels.Label{Name: values.name, Value: fingerprints.value}
	}
	sort.Sort(internedLabels)
	return internedLabels
}

func (shard *indexShard) lookup(matchers []*labels.Matcher) []model.Fingerprint {
	// index slice values must only be accessed under lock, so all
	// code paths must take a copy before returning
	shard.mtx.RLock()
	defer shard.mtx.RUnlock()

	// per-shard intersection is initially nil, which is a special case
	// meaning "everything" when passed to intersect()
	// loop invariant: result is sorted
	var result []model.Fingerprint
	for _, matcher := range matchers {
		values, ok := shard.idx[matcher.Name]
		if !ok {
			return nil
		}
		var toIntersect model.Fingerprints
		if matcher.Type == labels.MatchEqual {
			fps := values.fps[matcher.Value]
			toIntersect = append(toIntersect, fps.fps...) // deliberate copy
		} else if matcher.Type == labels.MatchRegexp && len(chunk.FindSetMatches(matcher.Value)) > 0 {
			// The lookup is of the form `=~"a|b|c|d"`
			set := chunk.FindSetMatches(matcher.Value)
			for _, value := range set {
				toIntersect = append(toIntersect, values.fps[value].fps...)
			}
			sort.Sort(toIntersect)
		} else {
			// accumulate the matching fingerprints (which are all distinct)
			// then sort to maintain the invariant
			for value, fps := range values.fps {
				if matcher.Matches(value) {
					toIntersect = append(toIntersect, fps.fps...)
				}
			}
			sort.Sort(toIntersect)
		}
		result = intersect(result, toIntersect)
		if len(result) == 0 {
			return nil
		}
	}

	return result
}

func (shard *indexShard) labelNames() []string {
	shard.mtx.RLock()
	defer shard.mtx.RUnlock()

	results := make([]string, 0, len(shard.idx))
	for name := range shard.idx {
		results = append(results, name)
	}

	sort.Strings(results)
	return results
}

func (shard *indexShard) labelValues(name string) []string {
	shard.mtx.RLock()
	defer shard.mtx.RUnlock()

	values, ok := shard.idx[name]
	if !ok {
		return nil
	}

	results := make([]string, 0, len(values.fps))
	for val := range values.fps {
		results = append(results, val)
	}

	sort.Strings(results)
	return results
}

func (shard *indexShard) delete(labels labels.Labels, fp model.Fingerprint) {
	shard.mtx.Lock()
	defer shard.mtx.Unlock()

	for _, pair := range labels {
		name, value := pair.Name, pair.Value
		values, ok := shard.idx[name]
		if !ok {
			continue
		}
		fingerprints, ok := values.fps[value]
		if !ok {
			continue
		}

		j := sort.Search(len(fingerprints.fps), func(i int) bool {
			return fingerprints.fps[i] >= fp
		})

		// see if search didn't find fp which matches the condition which means we don't have to do anything.
		if j >= len(fingerprints.fps) || fingerprints.fps[j] != fp {
			continue
		}
		fingerprints.fps = fingerprints.fps[:j+copy(fingerprints.fps[j:], fingerprints.fps[j+1:])]

		if len(fingerprints.fps) == 0 {
			delete(values.fps, value)
		} else {
			values.fps[value] = fingerprints
		}

		if len(values.fps) == 0 {
			delete(shard.idx, name)
		} else {
			shard.idx[name] = values
		}
	}
}

// intersect two sorted lists of fingerprints.  Assumes there are no duplicate
// fingerprints within the input lists.
func intersect(a, b []model.Fingerprint) []model.Fingerprint {
	if a == nil {
		return b
	}
	result := []model.Fingerprint{}
	for i, j := 0, 0; i < len(a) && j < len(b); {
		if a[i] == b[j] {
			result = append(result, a[i])
		}
		if a[i] < b[j] {
			i++
		} else {
			j++
		}
	}
	return result
}

func mergeStringSlices(ss [][]string) []string {
	switch len(ss) {
	case 0:
		return nil
	case 1:
		return ss[0]
	case 2:
		return mergeTwoStringSlices(ss[0], ss[1])
	default:
		halfway := len(ss) / 2
		return mergeTwoStringSlices(
			mergeStringSlices(ss[:halfway]),
			mergeStringSlices(ss[halfway:]),
		)
	}
}

func mergeTwoStringSlices(a, b []string) []string {
	result := make([]string, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] < b[j] {
			result = append(result, a[i])
			i++
		} else if a[i] > b[j] {
			result = append(result, b[j])
			j++
		} else {
			result = append(result, a[i])
			i++
			j++
		}
	}
	result = append(result, a[i:]...)
	result = append(result, b[j:]...)
	return result
}
