package sketch

import (
	"fmt"
	"github.com/DmitriyVTitov/size"
	"github.com/stretchr/testify/require"
	"math/rand"
	"strconv"
	"testing"
	"time"
)

func TestHeavyKeeper(t *testing.T) {
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
		{
			numStreams:       100,
			k:                10,
			maxPerStream:     1000,
			w:                56,
			d:                4,
			acceptableMisses: 1,
			iterations:       100,
		},
		{ // ~0.2s per iteration
			numStreams:       1000,
			k:                10,
			maxPerStream:     1000,
			w:                384,
			d:                4,
			acceptableMisses: 1,
			iterations:       100,
		},
		{ // ~1s per iteration
			numStreams:       1000,
			k:                100,
			maxPerStream:     1000,
			w:                512,
			d:                4,
			acceptableMisses: 2,
			iterations:       10,
		},
		{ // ~2.25s per iteration
			numStreams:       10000,
			k:                10,
			maxPerStream:     1000,
			w:                4096,
			d:                4,
			acceptableMisses: 1,
			iterations:       5,
		},
		{ // ~8s per iteration
			numStreams:       10000,
			k:                100,
			maxPerStream:     1000,
			w:                4096,
			d:                4,
			acceptableMisses: 3,
			iterations:       1,
		},
		//{ // takes ~27s per iteration
		//	numStreams:       100000,
		//	k:                10,
		//	maxPerStream:     1000,
		//	w:                40960,
		//	d:                4,
		//	acceptableMisses: 1,
		//},
		//{ // takes ~87s per iteration
		//	numStreams:       100000,
		//	k:                100,
		//	maxPerStream:     1000,
		//	w:                40960,
		//	d:                4,
		//	acceptableMisses: 2,
		//},
		//{ // takes ~5m per iteration
		//	numStreams:       1000000,
		//	k:                10,
		//	maxPerStream:     1000,
		//	w:                204800,
		//	d:                4,
		//	acceptableMisses: 2,
		//},
		//{ // takes ~5m per iteration
		//	numStreams:       1000000,
		//	k:                100,
		//	maxPerStream:     1000,
		//	w:                307200,
		//	d:                4,
		//	acceptableMisses: 10,
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
				// probability decay
				decay := 0.9
				hk, _ := NewHeavyKeeperTopK(decay, tc.k, tc.w, tc.d)

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
						hk.Observe(e.name)
					}
				}

				top := hk.Topk()
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

				sketchSize += size.Of(hk)
			}

			//fmt.Println("avg missing per test: ", float64(missing)/float64(tc.it))
			//fmt.Println("worst case missing: ", worst)
			//fmt.Println("sketch size per test : ", sketchSize/it)
		})

	}
}
