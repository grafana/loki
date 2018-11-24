package ingester

import (
	"sort"
	"strings"
	"sync"

	"github.com/prometheus/prometheus/pkg/labels"
)

// Taken from Cortex, update to use labels and string ids.

type invertedIndex struct {
	mtx sync.RWMutex
	idx map[string]map[string][]string
}

func newInvertedIndex() *invertedIndex {
	return &invertedIndex{
		idx: map[string]map[string][]string{},
	}
}

func (i *invertedIndex) add(ls labels.Labels, id string) {
	i.mtx.Lock()
	defer i.mtx.Unlock()

	for _, l := range ls {
		values, ok := i.idx[l.Name]
		if !ok {
			values = map[string][]string{}
		}
		existing := values[l.Value]
		j := sort.Search(len(existing), func(i int) bool {
			return strings.Compare(existing[i], id) > 0
		})
		existing = append(existing, "")
		copy(existing[j+1:], existing[j:])
		existing[j] = id
		values[l.Value] = existing
		i.idx[l.Name] = values
	}
}

func (i *invertedIndex) lookup(matchers []*labels.Matcher) []string {
	if len(matchers) == 0 {
		return nil
	}
	i.mtx.RLock()
	defer i.mtx.RUnlock()

	// intersection is initially nil, which is a special case.
	var intersection []string
	for _, matcher := range matchers {
		values, ok := i.idx[matcher.Name]
		if !ok {
			return nil
		}
		var toIntersect []string
		for value, fps := range values {
			if matcher.Matches(value) {
				toIntersect = merge(toIntersect, fps)
			}
		}
		intersection = intersect(intersection, toIntersect)
		if len(intersection) == 0 {
			return nil
		}
	}

	return intersection
}

func (i *invertedIndex) labelNames() []string {
	i.mtx.RLock()
	defer i.mtx.RUnlock()

	res := make([]string, 0, len(i.idx))
	for name := range i.idx {
		res = append(res, name)
	}
	return res
}

func (i *invertedIndex) lookupLabelValues(name string) []string {
	i.mtx.RLock()
	defer i.mtx.RUnlock()

	values, ok := i.idx[name]
	if !ok {
		return nil
	}
	res := make([]string, 0, len(values))
	for val := range values {
		res = append(res, val)
	}
	return res
}

// nolint
func (i *invertedIndex) delete(ls labels.Labels, id string) {
	i.mtx.Lock()
	defer i.mtx.Unlock()

	for _, l := range ls {
		values, ok := i.idx[l.Name]
		if !ok {
			continue
		}
		ids, ok := values[l.Value]
		if !ok {
			continue
		}

		j := sort.Search(len(ids), func(i int) bool {
			return ids[i] >= id
		})
		ids = ids[:j+copy(ids[j:], ids[j+1:])]

		if len(ids) == 0 {
			delete(values, l.Value)
		} else {
			values[l.Value] = ids
		}

		if len(values) == 0 {
			delete(i.idx, l.Name)
		} else {
			i.idx[l.Name] = values
		}
	}
}

// intersect two sorted lists of fingerprints.  Assumes there are no duplicate
// fingerprints within the input lists.
func intersect(a, b []string) []string {
	if a == nil {
		return b
	}
	result := []string{}
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

// merge two sorted lists of fingerprints.  Assumes there are no duplicate
// fingerprints between or within the input lists.
func merge(a, b []string) []string {
	result := make([]string, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i] < b[j] {
			result = append(result, a[i])
			i++
		} else {
			result = append(result, b[j])
			j++
		}
	}
	for ; i < len(a); i++ {
		result = append(result, a[i])
	}
	for ; j < len(b); j++ {
		result = append(result, b[j])
	}
	return result
}
