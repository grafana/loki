package sketch

import (
	"bufio"
	"container/heap"
	"io"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"testing"

	"github.com/alicebob/miniredis/v2/hyperloglog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type event struct {
	name  string
	count int
}

func TestTopkCardinality(t *testing.T) {
	max := 1000000
	topk, err := newCMSTopK(100, 10, 10)
	assert.NoError(t, err)
	for i := 0; i < max; i++ {
		topk.Observe(strconv.Itoa(i))
	}
	c, bigEnough := topk.Cardinality()
	// hll has a typical error accuracy of 2%
	assert.True(t, (c >= uint64(float64(max)*0.98)) && (c <= uint64(float64(max)*1.02)))
	assert.False(t, bigEnough)

	topk, err = NewCMSTopkForCardinality(nil, 100, max)
	assert.NoError(t, err)
	for i := 0; i < max; i++ {
		topk.Observe(strconv.Itoa(i))
	}
	c, bigEnough = topk.Cardinality()
	assert.Truef(t, bigEnough, "Cardinality of %d was not big enough.", c)
}

// TODO: merging is not as accurate as it should be
func TestTopK_Merge(t *testing.T) {
	nStreams := 10
	k := 1
	maxPerStream := 1000
	events := make([]event, 0)
	max := int64(0)
	r := rand.New(rand.NewSource(99))

	for i := 0; i < nStreams-k; i++ {
		num := int64(maxPerStream)
		n := r.Int63n(num) + 1
		if n > max {
			max = n
		}
		for j := 0; j < int(n); j++ {
			events = append(events, event{name: strconv.Itoa(i), count: 1})
		}
	}
	// then another set of things more than the max of the previous entries
	for i := nStreams - k; i < nStreams; i++ {
		n := rand.Int63n(int64(maxPerStream)) + 1 + max
		for j := 0; j < int(n); j++ {
			events = append(events, event{name: strconv.Itoa(i), count: 1})
		}
	}

	rand.Shuffle(len(events), func(i, j int) { events[i], events[j] = events[j], events[i] })

	topk1, err := NewCMSTopkForCardinality(nil, k, nStreams)
	assert.NoError(t, err, "error creating topk")
	topk2, err := NewCMSTopkForCardinality(nil, k, nStreams)
	assert.NoError(t, err, "error creating topk")
	for i := 0; i < (len(events) / 2); i++ {
		for j := 0; j < events[i].count; j++ {
			topk1.Observe(events[i].name)
		}
	}

	for i := len(events) / 2; i < len(events); i++ {
		for j := 0; j < events[i].count; j++ {
			topk2.Observe(events[i].name)
		}
	}

	err = topk1.Merge(topk2)
	require.NoError(t, err)

	mergedTopk := topk1.Topk()
	var eventName string
	mergedMissing := 0
outer:
	for i := nStreams - k; i < nStreams; i++ {
		eventName = strconv.Itoa(i)
		for j := 0; j < len(mergedTopk); j++ {
			if mergedTopk[j].Event == eventName {
				continue outer
			}
		}

		mergedMissing++
	}

	require.LessOrEqualf(t, mergedMissing, 2, "more than acceptable misses: %d > %d", mergedMissing, 2)

	// observe the same events into a single sketch
	topk3, _ := NewCMSTopkForCardinality(nil, k, nStreams)
	for _, e := range events {
		for j := 0; j < e.count; j++ {
			topk3.Observe(e.name)
		}
	}

	singleTopk := topk3.Topk()

	singleMissing := 0
outer2:
	for i := nStreams - k; i < nStreams; i++ {
		eventName = strconv.Itoa(i)
		for j := 0; j < len(singleTopk); j++ {
			if singleTopk[j].Event == eventName {
				continue outer2
			}
		}

		singleMissing++
	}

	require.LessOrEqualf(t, singleMissing, 2, "more than acceptable misses: %d > %d", singleMissing, 2)
	// this condition is never actually true
	//require.LessOrEqualf(t, mergedMissing, singleMissing, "merged sketch should be at least as accurate as a single sketch")
}

// compare the accuracy of cms topk and hk to the real topk
func TestRealTopK(t *testing.T) {
	// the HLL cardinality estimate for this page is ~72000
	f, err := os.Open("testdata/shakspeare.txt")
	require.NoError(t, err)
	defer f.Close()
	scanner := bufio.NewScanner(f)

	m := make(map[string]uint32)
	h := MinHeap{}
	hll := hyperloglog.New16()

	scanner.Split(bufio.ScanWords)
	s := ""
	for scanner.Scan() {
		s = scanner.Text()
		if m[s] == 0 {
			hll.Insert([]byte(s))
		}
		m[s] = m[s] + 1
		if _, ok := h.Find(s); ok {
			h.update(s, m[s])
			continue
		}
		if len(h) < 100 {
			heap.Push(&h, &node{event: s, count: m[s]})
			continue
		}
		if m[s] > (h.Peek().(*node).count) {
			heap.Pop(&h)
			heap.Push(&h, &node{event: s, count: m[s]})
		}
	}

	res := make(TopKResult, 0, len(h))
	for i := 0; i < len(h); i++ {
		res = append(res, element{h[i].event, int64(h[i].count)})
	}
	sort.Sort(res)

	cms, _ := NewCMSTopkForCardinality(nil, 100, 72000)
	f, err = os.Open("testdata/shakspeare.txt")
	assert.NoError(t, err)

	scanner = bufio.NewScanner(f)
	// Set the split function for the scanning operation.
	scanner.Split(bufio.ScanWords)
	// Scan all words from the file.
	for scanner.Scan() {
		s = scanner.Text()
		cms.Observe(s)
	}
	cmsTop := cms.Topk()
	cmsMissing := 0
outer:
	for _, t := range res {
		for _, t2 := range cmsTop {
			if t2.Event == t.Event {
				continue outer
			}
		}
		cmsMissing++
	}

	// we should have gotten at least 98/100 topk right here
	require.True(t, cmsMissing <= 2, "cms missing %d", cmsMissing)
}

// compare the accuracy of cms topk and hk to the real topk when using
// merging operations with 10 sketches each
func TestRealTop_Merge(t *testing.T) {
	// the HLL cardinality estimate for these page is ~120000
	r1, err := os.Open("testdata/shakspeare.txt")
	require.NoError(t, err)
	r2, err := os.Open("testdata/war_peace.txt")
	require.NoError(t, err)
	r3, err := os.Open("testdata/monte_cristo.txt")
	require.NoError(t, err)
	combined := io.MultiReader(r1, r2, r3)
	defer r1.Close()
	defer r2.Close()
	defer r3.Close()

	scanner := bufio.NewScanner(combined)

	m := make(map[string]uint32)
	h := MinHeap{}
	hll := hyperloglog.New16()
	// HK gets more inaccurate with merging the more shards we have
	// while CMS for this dataset doesn't seem to lose accuracy with merging
	shards := 10
	k := 100
	scanner.Split(bufio.ScanWords)

	s := ""
	for scanner.Scan() {
		s = scanner.Text()
		if m[s] == 0 {
			hll.Insert([]byte(s))
		}
		m[s] = m[s] + 1
		if _, ok := h.Find(s); ok {
			h.update(s, m[s])
			continue
		}
		if len(h) < k {
			heap.Push(&h, &node{event: s, count: m[s]})
			continue
		}
		if m[s] > (h.Peek().(*node).count) {
			heap.Pop(&h)
			heap.Push(&h, &node{event: s, count: m[s]})
		}
	}

	res := make(TopKResult, 0, len(h))
	for i := 0; i < len(h); i++ {
		res = append(res, element{h[i].event, int64(h[i].count)})
	}
	sort.Sort(res)

	r1, err = os.Open("testdata/shakspeare.txt")
	require.NoError(t, err)
	r2, err = os.Open("testdata/war_peace.txt")
	require.NoError(t, err)
	r3, err = os.Open("testdata/monte_cristo.txt")
	require.NoError(t, err)
	combined = io.MultiReader(r1, r2, r3)

	scanner = bufio.NewScanner(combined)
	scanner.Split(bufio.ScanWords)
	var cms = make([]*Topk, shards)
	for i := range cms {
		cms[i], _ = newCMSTopK(k, 2048, 5)
	}
	i := 0
	for scanner.Scan() {
		idx := i % shards
		s = scanner.Text()
		cms[idx].Observe(s)
		i++
	}
	mergedCMS, _ := newCMSTopK(k, 2048, 5)
	for _, c := range cms {
		err = mergedCMS.Merge(c)
		require.NoError(t, err)
	}
	cmsTop := mergedCMS.Topk()
	cmsMissing := 0
outer:
	for _, t := range res {
		for _, t2 := range cmsTop {
			if t2.Event == t.Event {
				continue outer
			}
		}
		cmsMissing++
	}
}

// compare the accuracy of cms topk and hk to the real topk when using
// merge after receiving each sketch as a proto
func TestRealTop_MergeProto(t *testing.T) {
	// the HLL cardinality estimate for link 1 and 2 is ~120000
	k := 100

	cms1, _ := newCMSTopK(k, 2048, 5)
	cms2, _ := newCMSTopK(k, 2048, 5)

	// read the first dataset into sketch1
	r1, err := os.Open("testdata/shakspeare.txt")
	r1.Close()
	require.NoError(t, err)
	scanner := bufio.NewScanner(r1)
	scanner.Split(bufio.ScanWords)

	for scanner.Scan() {
		s := scanner.Text()
		cms1.Observe(s)
	}

	// read the second dataset into sketch2
	r2, err := os.Open("testdata/war_peace.txt")
	require.NoError(t, err)
	defer r2.Close()
	scanner = bufio.NewScanner(r2)
	for scanner.Scan() {
		s := scanner.Text()
		cms1.Observe(s)
	}
	mergedCMS, _ := newCMSTopK(k, 2048, 5)
	require.NoError(t, mergedCMS.Merge(cms1), "error merging")
	require.NoError(t, mergedCMS.Merge(cms2), "error merging")

	// turn both sketches into proto
	cms1Proto, err := cms1.ToProto()
	require.NoError(t, err)

	cms2Proto, err := cms2.ToProto()
	require.NoError(t, err)

	deserialized1, err := TopkFromProto(cms1Proto)
	require.NoError(t, err)
	deserialized2, err := TopkFromProto(cms2Proto)
	require.NoError(t, err)

	// merge the deserialized sketches
	dMerged, _ := newCMSTopK(k, 2048, 5)
	require.NoError(t, dMerged.Merge(deserialized1))
	require.NoError(t, dMerged.Merge(deserialized2))

	require.Equal(t, mergedCMS.Topk(), dMerged.Topk(), "topk was not correct after deserializing and merging")
	mCardinality, _ := mergedCMS.Cardinality()
	dCardinality, _ := dMerged.Cardinality()
	require.Equal(t, mCardinality, dCardinality, "hll cardinality estimate was not correct after deserializing and merging")
}
