//go:build sketch_slow
// +build sketch_slow

package sketch

import (
	"fmt"
	"math/rand"
	"strconv"
	"testing"
	"time"

	"github.com/DmitriyVTitov/size"
	"github.com/stretchr/testify/require"
)

// TODO update this test based on new sizes
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
				topk, _ := newCMSTopK(tc.k, tc.w, tc.d)
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

// this test was supposed to be CMS vs sketchbf, my sketch BF implementation for some reason
// was never better than 50% slower than cms, but applying some of the sketchbf principles
// and measuring via this benchmark I managed to make our CMS Topk implementation almost 3x faster
func BenchmarkCMSTopk(b *testing.B) {
	nStreams := 100000
	maxPerStream := 1000
	k := 100
	w := 2719
	d := 7
	oTotal := 0
	cms, _ := newCMSTopK(k, w, d)
	//bf, _ := NewSketchBF(k, w, d)
	max := int64(0)
	events := make([]event, 0)
	total := int64(0)
	cmsDur := time.Duration(0)
	//bfDur := time.Duration(0)

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		for j := 0; j < nStreams-k; j++ {
			num := int64(maxPerStream)
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
		for z := nStreams - k; z < nStreams; z++ {
			n := int64(rand.Int63n(int64(maxPerStream)) + 1 + max)
			for x := 0; x < int(n); x++ {
				events = append(events, event{name: strconv.Itoa(z), count: 1})
			}
			total += n
			oTotal += int(n)
		}

		rand.Shuffle(len(events), func(i, j int) { events[i], events[j] = events[j], events[i] })
		b.StartTimer()

		cmsStart := time.Now()
		for _, e := range events {
			for i := 0; i < e.count; i++ {
				cms.Observe(e.name)
			}
		}
		cmsDur += time.Since(cmsStart)

		//bfStart := time.Now()
		//for _, e := range events {
		//	for i := 0; i < e.count; i++ {
		//		bf.Observe(e.name)
		//	}
		//}
		//bfDur += time.Since(bfStart)
	}
	b.ReportMetric(cmsDur.Seconds()/float64(b.N), "cms_time/op")
	//b.ReportMetric(bfDur.Seconds()/float64(b.N), "bf_time/op")
	//require.LessOrEqualf(b, bfDur.Seconds()/float64(b.N), cmsDur.Seconds()/float64(b.N), "sketchbf duration should be less than cms duration because of the reduction in heap")
}

func TestBFTopK(t *testing.T) {
	nStreams := 100000
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
		for z := 0; z < int(n); z++ {
			events = append(events, event{name: strconv.Itoa(i), count: 1})
		}
	}
	// then another set of things more than the max of the previous entries
	for i := nStreams - k; i < nStreams; i++ {
		n := int64(rand.Int63n(int64(maxPerStream)) + 1 + max)
		for x := 0; x < int(n); x++ {
			events = append(events, event{name: strconv.Itoa(i), count: 1})
		}
	}

	rand.Shuffle(len(events), func(i, j int) { events[i], events[j] = events[j], events[i] })

	topk, _ := NewSketchBF(100, 27189, 7)
	for _, e := range events {
		for i := 0; i < e.count; i++ {
			topk.Observe(e.name)
		}
	}

	cms, _ := newCMSTopK(100, 27189, 7)
	for _, e := range events {
		for i := 0; i < e.count; i++ {
			cms.Observe(e.name)
		}
	}

	bftopk := topk.Topk()
	cmsTopk := cms.Topk()

	bfMissing := 0
	cmsMissing := 0
	var eventName string

	for i := nStreams - k; i < nStreams; i++ {
		eventName = strconv.Itoa(i)
		m := false
		for j := 0; j < len(bftopk); j++ {
			if bftopk[j].Event == eventName {
				m = true
			}
		}
		if !m {
			bfMissing++
		}
		m = false
		for j := 0; j < len(cmsTopk); j++ {
			if cmsTopk[j].Event == eventName {
				m = true
			}
		}
		if !m {
			cmsMissing++
		}
	}
	// should have the same accuracy since sketchbf is just using a cms under the hood
	require.LessOrEqualf(t, bfMissing, cmsMissing, "sketchbf should be as accurate or better than cms since it uses cms under the hood, bf missing: %d, cms missing: %d", bfMissing, cmsMissing)
}
