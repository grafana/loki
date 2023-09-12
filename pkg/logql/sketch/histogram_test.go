package sketch

import (
	"bufio"
	"fmt"
	"github.com/DmitriyVTitov/size"
	"github.com/montanaflynn/stats"
	"github.com/stretchr/testify/require"
	"math/rand"
	"os"
	"sort"
	"testing"
)

// I used this test to generate what is essentially CSV data to then import into a google sheet
// for analysis of the accuracy results and sketch sizes.
func TestQuantiles(t *testing.T) {
	seeds := []int64{10, 100, 1000}
	// v controls the distribution of values along the curve, a greater v
	// value means there's a large distance between generated values
	vs := []float64{1.0, 5.0, 10.0}
	// s controls the exponential curve of the distribution
	// the higher the s values the faster the drop off from max value to lesser values
	// s must be > 1.0
	ss := []float64{1.01, 2.0, 3.0, 4.0}
	nEvents := []int64{1000, 10000, 100000, 1000000}
	file, err := os.OpenFile("quantile_test.txt", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	require.NoError(t, err)
	// Create a writer
	w := bufio.NewWriter(file)
	for _, events := range nEvents {
		for _, s := range ss {
			for _, seed := range seeds {
				for _, v := range vs {
					t.Run(fmt.Sprintf("seed_%d,s_%.2f,v_%.2f,max_%d", seed, s, v, events), func(t *testing.T) {
						dd := NewDDSketch()
						td := NewTDigestSketch()

						r := rand.New(rand.NewSource(seed))
						z := rand.NewZipf(r, s, v, uint64(events))
						m := float64(0)
						counts := Float64Slice{}
						for i := float64(0); i < 100000; i++ {

							count := float64(z.Uint64())
							if count > m {
								m = count
							}
							counts = append(counts, count)
							td.Add(count)
							dd.Add(count)
						}
						sort.Sort(counts)

						p, _ := stats.Percentile(stats.Float64Data(counts), 99)
						ddQuantile, _ := dd.Quantile(0.99)
						tdQuantile, _ := td.Quantile(0.99)
						_, err := w.WriteString(fmt.Sprintf("%.2f,%.2f,%d,%d,%.2f,%.2f,%.2f,%d,%d,%.2f,%.2f\n",
							s,
							v,
							seed,
							events,
							p,
							tdQuantile,
							ddQuantile,
							size.Of(td),
							size.Of(dd),
							getPercentDiff(p, tdQuantile),
							getPercentDiff(p, ddQuantile)))
						require.NoError(t, err)
					})
				}

			}
		}
	}
	w.Flush()
	file.Close()
}

func getPercentDiff(base, second float64) float64 {
	diff := base - second
	avg := (base + second) / 2
	return (diff / avg) * 100
}

type Float64Slice []float64

func (s Float64Slice) Len() int {
	return len(s)
}

func (s Float64Slice) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s Float64Slice) Less(i, j int) bool {
	return s[i] > s[j]
}
