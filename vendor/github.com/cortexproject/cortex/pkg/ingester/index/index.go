package index

import (
	"sort"
	"sync"
	"unsafe"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/cortexproject/cortex/pkg/ingester/client"
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
		shards[i].idx = map[model.LabelName]map[model.LabelValue][]model.Fingerprint{}
	}
	return &InvertedIndex{
		shards: shards,
	}
}

// Add a fingerprint under the specified labels.
func (ii *InvertedIndex) Add(labels []client.LabelPair, fp model.Fingerprint) {
	shard := &ii.shards[util.HashFP(fp)%indexShards]
	shard.add(labels, fp)
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

	sort.Sort(fingerprints(result))
	return result
}

// LabelNames returns all label names.
func (ii *InvertedIndex) LabelNames() model.LabelNames {
	results := make([]model.LabelNames, 0, indexShards)

	for i := range ii.shards {
		shardResult := ii.shards[i].labelNames()
		results = append(results, shardResult)
	}

	return mergeLabelNameLists(results)
}

// LabelValues returns the values for the given label.
func (ii *InvertedIndex) LabelValues(name model.LabelName) model.LabelValues {
	results := make([]model.LabelValues, 0, indexShards)

	for i := range ii.shards {
		shardResult := ii.shards[i].labelValues(name)
		results = append(results, shardResult)
	}

	return mergeLabelValueLists(results)
}

// Delete a fingerprint with the given label pairs.
func (ii *InvertedIndex) Delete(labels []client.LabelPair, fp model.Fingerprint) {
	shard := &ii.shards[util.HashFP(fp)%indexShards]
	shard.delete(labels, fp)
}

// NB slice entries are sorted in fp order.
type unlockIndex map[model.LabelName]map[model.LabelValue][]model.Fingerprint

// This is the prevalent value for Intel and AMD CPUs as-at 2018.
const cacheLineSize = 64

type indexShard struct {
	mtx sync.RWMutex
	idx unlockIndex
	pad [cacheLineSize - unsafe.Sizeof(sync.Mutex{}) - unsafe.Sizeof(unlockIndex{})]byte
}

func (shard *indexShard) add(metric []client.LabelPair, fp model.Fingerprint) {
	shard.mtx.Lock()
	defer shard.mtx.Unlock()

	for _, pair := range metric {
		name, value := model.LabelName(pair.Name), model.LabelValue(pair.Value)
		values, ok := shard.idx[name]
		if !ok {
			values = map[model.LabelValue][]model.Fingerprint{}
		}
		fingerprints := values[value]
		// Insert into the right position to keep fingerprints sorted
		j := sort.Search(len(fingerprints), func(i int) bool {
			return fingerprints[i] >= fp
		})
		fingerprints = append(fingerprints, 0)
		copy(fingerprints[j+1:], fingerprints[j:])
		fingerprints[j] = fp
		values[value] = fingerprints
		shard.idx[name] = values
	}
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
		values, ok := shard.idx[model.LabelName(matcher.Name)]
		if !ok {
			return nil
		}
		var toIntersect model.Fingerprints
		if matcher.Type == labels.MatchEqual {
			fps := values[model.LabelValue(matcher.Value)]
			toIntersect = append(toIntersect, fps...) // deliberate copy
		} else {
			// accumulate the matching fingerprints (which are all distinct)
			// then sort to maintain the invariant
			for value, fps := range values {
				if matcher.Matches(string(value)) {
					toIntersect = append(toIntersect, fps...)
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

func (shard *indexShard) labelNames() model.LabelNames {
	shard.mtx.RLock()
	defer shard.mtx.RUnlock()

	results := make(model.LabelNames, 0, len(shard.idx))
	for name := range shard.idx {
		results = append(results, name)
	}

	sort.Sort(labelNames(results))
	return results
}

func (shard *indexShard) labelValues(name model.LabelName) model.LabelValues {
	shard.mtx.RLock()
	defer shard.mtx.RUnlock()

	values, ok := shard.idx[name]
	if !ok {
		return nil
	}

	results := make(model.LabelValues, 0, len(values))
	for val := range values {
		results = append(results, val)
	}

	sort.Sort(labelValues(results))
	return results
}

func (shard *indexShard) delete(labels []client.LabelPair, fp model.Fingerprint) {
	shard.mtx.Lock()
	defer shard.mtx.Unlock()

	for _, pair := range labels {
		name, value := model.LabelName(pair.Name), model.LabelValue(pair.Value)
		values, ok := shard.idx[name]
		if !ok {
			continue
		}
		fingerprints, ok := values[value]
		if !ok {
			continue
		}

		j := sort.Search(len(fingerprints), func(i int) bool {
			return fingerprints[i] >= fp
		})
		fingerprints = fingerprints[:j+copy(fingerprints[j:], fingerprints[j+1:])]

		if len(fingerprints) == 0 {
			delete(values, value)
		} else {
			values[value] = fingerprints
		}

		if len(values) == 0 {
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

type labelValues model.LabelValues

func (a labelValues) Len() int           { return len(a) }
func (a labelValues) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a labelValues) Less(i, j int) bool { return a[i] < a[j] }

type labelNames model.LabelNames

func (a labelNames) Len() int           { return len(a) }
func (a labelNames) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a labelNames) Less(i, j int) bool { return a[i] < a[j] }

type fingerprints []model.Fingerprint

func (a fingerprints) Len() int           { return len(a) }
func (a fingerprints) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a fingerprints) Less(i, j int) bool { return a[i] < a[j] }

func mergeLabelValueLists(lvss []model.LabelValues) model.LabelValues {
	switch len(lvss) {
	case 0:
		return nil
	case 1:
		return lvss[0]
	case 2:
		return mergeTwoLabelValueLists(lvss[0], lvss[1])
	default:
		n := len(lvss) / 2
		left := mergeLabelValueLists(lvss[:n])
		right := mergeLabelValueLists(lvss[n:])
		return mergeTwoLabelValueLists(left, right)
	}
}

func mergeTwoLabelValueLists(a, b model.LabelValues) model.LabelValues {
	result := make(model.LabelValues, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] < b[j] {
			result = append(result, a[i])
			i++
		} else if a[i] > b[j] {
			result = append(result, b[j])
			j++
		} else {
			result = append(result, b[j])
			i++
			j++
		}
	}
	result = append(result, a[i:]...)
	result = append(result, b[j:]...)
	return result
}

func mergeLabelNameLists(lnss []model.LabelNames) model.LabelNames {
	switch len(lnss) {
	case 0:
		return nil
	case 1:
		return lnss[0]
	case 2:
		return mergeTwoLabelNameLists(lnss[0], lnss[1])
	default:
		n := len(lnss) / 2
		left := mergeLabelNameLists(lnss[:n])
		right := mergeLabelNameLists(lnss[n:])
		return mergeTwoLabelNameLists(left, right)
	}
}

func mergeTwoLabelNameLists(a, b model.LabelNames) model.LabelNames {
	result := make(model.LabelNames, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] < b[j] {
			result = append(result, a[i])
			i++
		} else if a[i] > b[j] {
			result = append(result, b[j])
			j++
		} else {
			result = append(result, b[j])
			i++
			j++
		}
	}
	result = append(result, a[i:]...)
	result = append(result, b[j:]...)
	return result
}
