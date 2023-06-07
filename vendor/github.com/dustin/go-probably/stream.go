package probably

import (
	"sort"
)

const initialThresh = 0

// StreamTop tracks the top-n items in a stream.
type StreamTop struct {
	sk       *Sketch
	thresh   uint32
	trimTo   int
	maxItems int

	keys map[string]uint32
}

// ItemCount represents an item with its count
type ItemCount struct {
	Key   string
	Count uint32
}

// NewStreamTop returns an estimator for the 'maxItems' in the stream.  It uses
// a count-min sketch, which is created with width w and depth d.
func NewStreamTop(w, d, maxItems int) *StreamTop {
	return &StreamTop{
		NewSketch(w, d),
		initialThresh,
		int(float64(maxItems) * 0.75),
		maxItems,
		make(map[string]uint32),
	}
}

type trimSlice struct {
	keys []string
	st   *StreamTop
}

func (p trimSlice) Len() int {
	return len(p.keys)
}

func (p trimSlice) Less(i, j int) bool {
	return p.st.keys[p.keys[i]] > p.st.keys[p.keys[j]]
}

func (p trimSlice) Swap(i, j int) {
	p.keys[i], p.keys[j] = p.keys[j], p.keys[i]
}

func (s *StreamTop) getTrimSlice() *trimSlice {
	ts := trimSlice{make([]string, 0, len(s.keys)), s}
	for k := range s.keys {
		ts.keys = append(ts.keys, k)
	}
	sort.Sort(&ts)
	return &ts
}

func (s *StreamTop) trim() {
	ts := s.getTrimSlice()

	s.thresh = s.keys[ts.keys[s.trimTo]]

	did := 0
	for k, v := range s.keys {
		if v <= s.thresh {
			did++
			delete(s.keys, k)
		}
	}
}

// Add an item to the stream counter.
func (s *StreamTop) Add(v string) {
	count := s.sk.ConservativeIncrement(v)
	if count > s.thresh {
		s.keys[v] = count
	}
	if len(s.keys) > s.maxItems {
		s.trim()
	}
}

// GetTop returns the top items from the stream
func (s StreamTop) GetTop() []ItemCount {
	ts := s.getTrimSlice()
	rv := make([]ItemCount, 0, len(s.keys))
	for _, k := range ts.keys {
		rv = append(rv, ItemCount{k, s.keys[k]})
	}
	return rv
}

// Merge the given stream into this one.
func (s *StreamTop) Merge(from *StreamTop) {
	s.sk.Merge(from.sk)

	d := map[string]uint32{}

	for k := range s.keys {
		d[k] = s.sk.Count(k)
	}
	for k := range from.keys {
		d[k] = s.sk.Count(k)
	}

	s.keys = d

	if len(s.keys) > s.maxItems {
		s.trim()
	}
}
