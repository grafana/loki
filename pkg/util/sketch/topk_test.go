package sketch

import (
	"bufio"
	"container/heap"
	"fmt"
	"github.com/DmitriyVTitov/size"
	"github.com/alicebob/miniredis/v2/hyperloglog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
	"math/rand"
	"net/http"
	"sort"
	"strconv"
	"testing"
	"time"
)

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func RandStringRunes(n int) string {
	gen := rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[gen.Intn(len(letterRunes))]
	}
	return string(b)
}

type event struct {
	name  string
	count int
}

func TestCMSTopk(t *testing.T) {
	type testcase struct {
		// number of unique streams
		numStreams int
		// roughly the total # of entries we want
		k int
		// max entries per stream
		maxPerStream int
		// max # of K we can accept missing
		acceptableMisses int

		//sketch dimensions
		w, d int

		iterations int
	}

	testcases := []testcase{
		{ // 0.12s per iteration
			numStreams:       100,
			k:                10,
			maxPerStream:     1000,
			w:                56,
			d:                4,
			acceptableMisses: 1,
			iterations:       100,
		},
		{ // ~0.1s per iteration
			numStreams:       1000,
			k:                10,
			maxPerStream:     1000,
			w:                576,
			d:                4,
			acceptableMisses: 1,
			iterations:       100,
		},
		{ // ~0.15s per iteration
			numStreams:       1000,
			k:                100,
			maxPerStream:     1000,
			w:                640,
			d:                4,
			acceptableMisses: 2,
			iterations:       100,
		},
		{ // ~1.25s per iteration
			numStreams:       10000,
			k:                10,
			maxPerStream:     1000,
			w:                5120,
			d:                4,
			acceptableMisses: 1,
			iterations:       10,
		},
		{ // ~1.35s per iteration
			numStreams:       10000,
			k:                100,
			maxPerStream:     1000,
			w:                5120,
			d:                4,
			acceptableMisses: 3,
			iterations:       10,
		},
		//{ // takes ~16s per iteration
		//	numStreams:       100000,
		//	k:                10,
		//	maxPerStream:     1000,
		//	w:                40960,
		//	d:                4,
		//	acceptableMisses: 1,
		//	iterations:       1,
		//},
		//{ // takes ~17s per iteration
		//	numStreams:       100000,
		//	k:                100,
		//	maxPerStream:     1000,
		//	w:                65536,
		//	d:                4,
		//	acceptableMisses: 2,
		//	iterations:       10,
		//},
		//{ // takes ~3m 15s per iteration
		//	numStreams:       1000000,
		//	k:                10,
		//	maxPerStream:     1000,
		//	w:                393216,
		//	d:                4,
		//	acceptableMisses: 2,
		//	iterations:       1,
		//},
		//{ // takes ~3m 30s per iteration
		//	numStreams:       1000000,
		//	k:                100,
		//	maxPerStream:     1000,
		//	w:                409600,
		//	d:                4,
		//	acceptableMisses: 10,
		//	iterations:       1,
		//},
		//{
		//	numStreams:       100000,
		//	k:                100,
		//	maxPerStream:     1000,
		//	w:                27183,
		//	d:                7,
		//	acceptableMisses: 3,
		//	iterations:       10,
		//},
		//{
		//	numStreams:       100000,
		//	k:                100,
		//	maxPerStream:     1000,
		//	w:                65536,
		//	d:                4,
		//	acceptableMisses: 3,
		//	iterations:       10,
		//},
	}

	for _, tc := range testcases {
		tc := tc
		t.Run(fmt.Sprintf("num_streams/%d_k/%d_iterations/%d", tc.numStreams, tc.k, tc.iterations), func(t *testing.T) {
			t.Parallel()
			missing := 0
			sketchSize := 0
			worst := 0
			oTotal := 0
			for i := 0; i < tc.iterations; i++ {
				topk, _ := NewCMSTopK(tc.k, tc.w, tc.d)
				itMissing := 0
				max := int64(0)
				events := make([]event, 0)
				total := int64(0)

				for j := 0; j < tc.numStreams-tc.k; j++ {
					num := int64(tc.maxPerStream)
					n := rand.Int63n(num) + 1
					if n > max {
						max = n
					}
					for z := 0; z < int(n); z++ {
						events = append(events, event{name: strconv.Itoa(j), count: 1})
					}
					total += n
					oTotal += int(n)
				}
				// then another set of things more than the max of the previous entries
				for z := tc.numStreams - tc.k; z < tc.numStreams; z++ {
					n := int64(rand.Int63n(int64(tc.maxPerStream)) + 1 + max)
					for x := 0; x < int(n); x++ {
						events = append(events, event{name: strconv.Itoa(z), count: 1})
					}
					total += n
					oTotal += int(n)
				}

				rand.Seed(time.Now().UnixNano())
				rand.Shuffle(len(events), func(i, j int) { events[i], events[j] = events[j], events[i] })

				for _, e := range events {
					for i := 0; i < e.count; i++ {
						topk.Observe(e.name)
					}
				}

				top := topk.Topk()
				var eventName string
			outer:
				for i := tc.numStreams - tc.k; i < tc.numStreams; i++ {
					eventName = strconv.Itoa(i)
					for j := 0; j < len(top); j++ {
						if top[j].Event == eventName {
							continue outer
						}
					}

					missing++
					itMissing++
				}

				if itMissing > worst {
					worst = itMissing
				}
				require.LessOrEqualf(t, itMissing, tc.acceptableMisses, "more than acceptable misses: %d > %d", itMissing, tc.acceptableMisses)

				sketchSize += size.Of(topk)
			}

			//fmt.Println("avg missing per test: ", float64(missing)/float64(tc.iterations))
			//fmt.Println("worst case missing: ", worst)
			//fmt.Println("sketch size per test : ", sketchSize/tc.iterations)
			//fmt.Println("sketch size per test : ", sketchSize/tc.iterations)
		})

	}
}

func TestTopkCardinality(t *testing.T) {
	max := 1000000
	topk, err := NewCMSTopK(100, 10, 10)
	assert.NoError(t, err)
	for i := 0; i < max; i++ {
		topk.Observe(strconv.Itoa(i))
	}
	// hll has a typical error accuracy of 2%
	_, bigEnough := topk.Cardinality()
	assert.False(t, bigEnough)

	topk, err = NewCMSTopkForCardinality(nil, 100, max)
	assert.NoError(t, err)
	for i := 0; i < max; i++ {
		topk.Observe(strconv.Itoa(i))
	}
	_, bigEnough = topk.Cardinality()
	assert.True(t, bigEnough)
}

func TestTopK_Merge(t *testing.T) {
	nStreams := 1000
	k := 100
	maxPerStream := 1000
	events := make([]event, 0)
	max := int64(0)

	for i := 0; i < nStreams-k; i++ {
		num := int64(maxPerStream)
		n := rand.Int63n(num) + 1
		if n > max {
			max = n
		}
		for j := 0; j < int(n); j++ {
			events = append(events, event{name: strconv.Itoa(i), count: 1})
		}
	}
	// then another set of things more than the max of the previous entries
	for i := nStreams - k; i < nStreams; i++ {
		n := int64(rand.Int63n(int64(maxPerStream)) + 1 + max)
		for j := 0; j < int(n); j++ {
			events = append(events, event{name: strconv.Itoa(i), count: 1})
		}
	}

	rand.Seed(time.Now().UnixNano())
	rand.Shuffle(len(events), func(i, j int) { events[i], events[j] = events[j], events[i] })

	topk1, err := NewCMSTopkForCardinality(nil, k, nStreams)
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

	topk1.Merge(topk2)

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
	link := "https://www.gutenberg.org/cache/epub/100/pg100.txt"

	resp, err := http.Get(link)
	require.NoError(t, err)
	scanner := bufio.NewScanner(resp.Body)

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
	resp.Body.Close()

	cms, _ := NewCMSTopK(100, 2048, 5)
	resp, err = http.Get(link)

	scanner = bufio.NewScanner(resp.Body)
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
	resp.Body.Close()

	hk := NewHeavyKeeperTopK(0.9, 100, 256, 5)
	resp, err = http.Get(link)

	scanner = bufio.NewScanner(resp.Body)
	scanner.Split(bufio.ScanWords)
	for scanner.Scan() {
		s = scanner.Text()
		hk.Observe(s)
	}
	hkTop := hk.Topk()
	hkMissing := 0
outer2:
	for _, t := range res {
		for _, t2 := range hkTop {
			if t2.Event == t.Event {
				continue outer2
			}
		}
		hkMissing++
	}
	resp.Body.Close()

	// we should have gotten at least 98/100 topk right for both these sketches
	require.True(t, cmsMissing <= 2)
	require.True(t, hkMissing <= 2)

	// when we do single sketches, HK should be as accurate or more accurate than CMS
	require.True(t, cmsMissing >= hkMissing)
}

// compare the accuracy of cms topk and hk to the real topk when using
// merging operations with 10 sketches each
func TestRealTop_Merge(t *testing.T) {
	// the HLL cardinality estimate for these page is ~120000
	link1 := "https://www.gutenberg.org/cache/epub/100/pg100.txt"
	link2 := "https://www.gutenberg.org/cache/epub/2600/pg2600.txt"
	link3 := "https://www.gutenberg.org/cache/epub/1184/pg1184.txt"

	r1, err := http.Get(link1)
	require.NoError(t, err)
	r2, err := http.Get(link2)
	require.NoError(t, err)
	r3, err := http.Get(link3)
	require.NoError(t, err)
	combined := io.MultiReader(r1.Body, r2.Body, r3.Body)

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
	r1.Body.Close()
	r2.Body.Close()
	r3.Body.Close()

	r1, err = http.Get(link1)
	require.NoError(t, err)
	r2, err = http.Get(link2)
	require.NoError(t, err)
	r3, err = http.Get(link2)
	require.NoError(t, err)
	combined = io.MultiReader(r1.Body, r2.Body)

	scanner = bufio.NewScanner(combined)
	scanner.Split(bufio.ScanWords)
	var cms = make([]*Topk, shards)
	for i := range cms {
		cms[i], _ = NewCMSTopK(k, 2048, 5)
	}
	i := 0
	for scanner.Scan() {
		idx := i % shards
		s = scanner.Text()
		cms[idx].Observe(s)
		i++
	}
	mergedCMS, _ := NewCMSTopK(k, 2048, 5)
	for _, c := range cms {
		mergedCMS.Merge(c)
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
	r1.Body.Close()
	r2.Body.Close()
	r3.Body.Close()

	r1, err = http.Get(link1)
	require.NoError(t, err)
	r2, err = http.Get(link2)
	require.NoError(t, err)
	r3, err = http.Get(link2)
	require.NoError(t, err)
	combined = io.MultiReader(r1.Body, r2.Body)
	scanner = bufio.NewScanner(combined)
	scanner.Split(bufio.ScanWords)
	var hk = make([]*HeavyKeeperTopK, shards)
	for i := range cms {
		h := NewHeavyKeeperTopK(0.9, k, 256, 5)
		hk[i] = &h
	}
	i = 0
	for scanner.Scan() {
		//fmt.Println(scanner.Text())
		idx := i % shards
		s = scanner.Text()
		hk[idx].Observe(s)
		i++
	}
	mergedHK := NewHeavyKeeperTopK(0.9, k, 256, 5)
	for _, h := range hk {
		mergedHK.Merge(h)
	}
	hkTop := mergedHK.Topk()
	hkMissing := 0
outer2:
	for _, t := range res {
		for _, t2 := range hkTop {
			if t2.Event == t.Event {
				continue outer2
			}
		}
		hkMissing++
	}
	r1.Body.Close()
	r2.Body.Close()
	r3.Body.Close()

	// the CMS using conservative updates will never be as accurate after merging as it
	// would have been if we had used
	assert.True(t, cmsMissing <= hkMissing, "merged CMS should be more accurate than merged HK")
}
