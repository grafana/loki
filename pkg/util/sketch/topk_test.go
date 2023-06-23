package sketch

import (
	"fmt"
	"github.com/DmitriyVTitov/size"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"math/rand"
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

func TestTopK(t *testing.T) {
	nStreams := 10000
	k := 100
	maxPerStream := 1000
	events := make([]event, 0)
	max := 0

	for i := 0; i < nStreams-k; i++ {
		n := rand.Intn(maxPerStream) + 1
		if n > max {
			max = n
		}
		events = append(events, event{name: strconv.Itoa(i), count: n})
	}
	// ensure there's a topk we can check for
	for i := nStreams - k; i < nStreams; i++ {
		n := rand.Intn(maxPerStream) + 1 + max
		events = append(events, event{name: strconv.Itoa(i), count: n})
	}

	topk, err := NewCMSTopkForCardinality(nil, k, nStreams)
	assert.NoError(t, err, "error creating topk")
	for _, e := range events {
		for i := 0; i < e.count; i++ {
			topk.Observe(e.name)
		}
	}

	missing := 0
	for i := nStreams - k; i < nStreams; i++ {
		if !topk.InTopk(strconv.Itoa(i)) {
			missing++
		}
	}
	// we accept 98 out of 100 to be correct
	assert.False(t, missing > int(float64(k)*float64(.02)), "more than 2% of topk were missing")
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

			//fmt.Println("avg missing per test: ", float64(missing)/float64(tc.it))
			//fmt.Println("worst case missing: ", worst)
			//fmt.Println("sketch size per test : ", sketchSize/it)
		})

	}
}

func TestTopkCardinality(t *testing.T) {
	for i := 0; i < 10; i++ {
		max := 1000000
		topk, err := NewCMSTopK(100, 10, 10)
		assert.NoError(t, err)
		for i := 0; i < max; i++ {
			topk.Observe(strconv.Itoa(i))
		}
		// hll has a typical error accuracy of 2%
		_, bigEnough := topk.Cardinality()
		assert.True(t, bigEnough)
	}
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

	// merge
	topk1.Merge(&topk2)

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

	require.LessOrEqualf(t, singleMissing, 2, "more than acceptable misses: %d > %d", mergedMissing, 2)
	require.LessOrEqualf(t, mergedMissing, singleMissing, "merged sketch should be at least as accurate as a single sketch")
}
