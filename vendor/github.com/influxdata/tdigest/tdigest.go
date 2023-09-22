package tdigest

import (
	"math"
	"sort"
)

// TDigest is a data structure for accurate on-line accumulation of
// rank-based statistics such as quantiles and trimmed means.
type TDigest struct {
	Compression float64

	maxProcessed      int
	maxUnprocessed    int
	processed         CentroidList
	unprocessed       CentroidList
	cumulative        []float64
	processedWeight   float64
	unprocessedWeight float64
	min               float64
	max               float64
}

// New initializes a new distribution with a default compression.
func New() *TDigest {
	return NewWithCompression(1000)
}

// NewWithCompression initializes a new distribution with custom compression.
func NewWithCompression(c float64) *TDigest {
	t := &TDigest{
		Compression: c,
	}
	t.maxProcessed = processedSize(0, t.Compression)
	t.maxUnprocessed = unprocessedSize(0, t.Compression)
	t.processed = make(CentroidList, 0, t.maxProcessed)
	t.unprocessed = make(CentroidList, 0, t.maxUnprocessed+1)
	t.Reset()
	return t
}

// Calculate number of bytes needed for a tdigest of size c,
// where c is the compression value
func ByteSizeForCompression(comp float64) int {
	c := int(comp)
	// // A centroid is 2 float64s, so we need 16 bytes for each centroid
	// float_size := 8
	// centroid_size := 2 * float_size

	// // Unprocessed and processed can grow up to length c
	// unprocessed_size := centroid_size * c
	// processed_size := unprocessed_size

	// // the cumulative field can also be of length c, but each item is a single float64
	// cumulative_size := float_size * c // <- this could also be unprocessed_size / 2

	// return unprocessed_size + processed_size + cumulative_size

	// // or, more succinctly:
	// return float_size * c * 5

	// or even more succinctly
	return c * 40
}

// Reset resets the distribution to its initial state.
func (t *TDigest) Reset() {
	t.processed = t.processed[:0]
	t.unprocessed = t.unprocessed[:0]
	t.cumulative = t.cumulative[:0]
	t.processedWeight = 0
	t.unprocessedWeight = 0
	t.min = math.MaxFloat64
	t.max = -math.MaxFloat64
}

// Add adds a value x with a weight w to the distribution.
func (t *TDigest) Add(x, w float64) {
	t.AddCentroid(Centroid{Mean: x, Weight: w})
}

// AddCentroidList can quickly add multiple centroids.
func (t *TDigest) AddCentroidList(c CentroidList) {
	// It's possible to optimize this by bulk-copying the slice, but this
	// yields just a 1-2% speedup (most time is in process()), so not worth
	// the complexity.
	for i := range c {
		t.AddCentroid(c[i])
	}
}

// AddCentroid adds a single centroid.
// Weights which are not a number or are <= 0 are ignored, as are NaN means.
func (t *TDigest) AddCentroid(c Centroid) {
	if math.IsNaN(c.Mean) || c.Weight <= 0 || math.IsNaN(c.Weight) || math.IsInf(c.Weight, 1) {
		return
	}

	t.unprocessed = append(t.unprocessed, c)
	t.unprocessedWeight += c.Weight

	if t.processed.Len() > t.maxProcessed ||
		t.unprocessed.Len() > t.maxUnprocessed {
		t.process()
	}
}

// Merges the supplied digest into this digest. Functionally equivalent to
// calling t.AddCentroidList(t2.Centroids(nil)), but avoids making an extra
// copy of the CentroidList.
func (t *TDigest) Merge(t2 *TDigest) {
	t2.process()
	t.AddCentroidList(t2.processed)
}

func (t *TDigest) process() {
	if t.unprocessed.Len() > 0 ||
		t.processed.Len() > t.maxProcessed {

		// Append all processed centroids to the unprocessed list and sort
		t.unprocessed = append(t.unprocessed, t.processed...)
		sort.Sort(&t.unprocessed)

		// Reset processed list with first centroid
		t.processed.Clear()
		t.processed = append(t.processed, t.unprocessed[0])

		t.processedWeight += t.unprocessedWeight
		t.unprocessedWeight = 0
		soFar := t.unprocessed[0].Weight
		limit := t.processedWeight * t.integratedQ(1.0)
		for _, centroid := range t.unprocessed[1:] {
			projected := soFar + centroid.Weight
			if projected <= limit {
				soFar = projected
				(&t.processed[t.processed.Len()-1]).Add(centroid)
			} else {
				k1 := t.integratedLocation(soFar / t.processedWeight)
				limit = t.processedWeight * t.integratedQ(k1+1.0)
				soFar += centroid.Weight
				t.processed = append(t.processed, centroid)
			}
		}
		t.min = math.Min(t.min, t.processed[0].Mean)
		t.max = math.Max(t.max, t.processed[t.processed.Len()-1].Mean)
		t.unprocessed.Clear()
	}
}

// Centroids returns a copy of processed centroids.
// Useful when aggregating multiple t-digests.
//
// Centroids are appended to the passed CentroidList; if you're re-using a
// buffer, be sure to pass cl[:0].
func (t *TDigest) Centroids(cl CentroidList) CentroidList {
	t.process()
	return append(cl, t.processed...)
}

func (t *TDigest) Count() float64 {
	t.process()

	// t.process always updates t.processedWeight to the total count of all
	// centroids, so we don't need to re-count here.
	return t.processedWeight
}

func (t *TDigest) updateCumulative() {
	// Weight can only increase, so the final cumulative value will always be
	// either equal to, or less than, the total weight. If they are the same,
	// then nothing has changed since the last update.
	if len(t.cumulative) > 0 && t.cumulative[len(t.cumulative)-1] == t.processedWeight {
		return
	}

	if n := t.processed.Len() + 1; n <= cap(t.cumulative) {
		t.cumulative = t.cumulative[:n]
	} else {
		t.cumulative = make([]float64, n)
	}

	prev := 0.0
	for i, centroid := range t.processed {
		cur := centroid.Weight
		t.cumulative[i] = prev + cur/2.0
		prev = prev + cur
	}
	t.cumulative[t.processed.Len()] = prev
}

// Quantile returns the (approximate) quantile of
// the distribution. Accepted values for q are between 0.0 and 1.0.
// Returns NaN if Count is zero or bad inputs.
func (t *TDigest) Quantile(q float64) float64 {
	t.process()
	t.updateCumulative()
	if q < 0 || q > 1 || t.processed.Len() == 0 {
		return math.NaN()
	}
	if t.processed.Len() == 1 {
		return t.processed[0].Mean
	}
	index := q * t.processedWeight
	if index <= t.processed[0].Weight/2.0 {
		return t.min + 2.0*index/t.processed[0].Weight*(t.processed[0].Mean-t.min)
	}

	lower := sort.Search(len(t.cumulative), func(i int) bool {
		return t.cumulative[i] >= index
	})

	if lower+1 != len(t.cumulative) {
		z1 := index - t.cumulative[lower-1]
		z2 := t.cumulative[lower] - index
		return weightedAverage(t.processed[lower-1].Mean, z2, t.processed[lower].Mean, z1)
	}

	z1 := index - t.processedWeight - t.processed[lower-1].Weight/2.0
	z2 := (t.processed[lower-1].Weight / 2.0) - z1
	return weightedAverage(t.processed[t.processed.Len()-1].Mean, z1, t.max, z2)
}

// CDF returns the cumulative distribution function for a given value x.
func (t *TDigest) CDF(x float64) float64 {
	t.process()
	t.updateCumulative()
	switch t.processed.Len() {
	case 0:
		return 0.0
	case 1:
		width := t.max - t.min
		if x <= t.min {
			return 0.0
		}
		if x >= t.max {
			return 1.0
		}
		if (x - t.min) <= width {
			// min and max are too close together to do any viable interpolation
			return 0.5
		}
		return (x - t.min) / width
	}

	if x <= t.min {
		return 0.0
	}
	if x >= t.max {
		return 1.0
	}
	m0 := t.processed[0].Mean
	// Left Tail
	if x <= m0 {
		if m0-t.min > 0 {
			return (x - t.min) / (m0 - t.min) * t.processed[0].Weight / t.processedWeight / 2.0
		}
		return 0.0
	}
	// Right Tail
	mn := t.processed[t.processed.Len()-1].Mean
	if x >= mn {
		if t.max-mn > 0.0 {
			return 1.0 - (t.max-x)/(t.max-mn)*t.processed[t.processed.Len()-1].Weight/t.processedWeight/2.0
		}
		return 1.0
	}

	upper := sort.Search(t.processed.Len(), func(i int) bool {
		return t.processed[i].Mean > x
	})

	z1 := x - t.processed[upper-1].Mean
	z2 := t.processed[upper].Mean - x
	return weightedAverage(t.cumulative[upper-1], z2, t.cumulative[upper], z1) / t.processedWeight
}

func (t *TDigest) integratedQ(k float64) float64 {
	return (math.Sin(math.Min(k, t.Compression)*math.Pi/t.Compression-math.Pi/2.0) + 1.0) / 2.0
}

func (t *TDigest) integratedLocation(q float64) float64 {
	return t.Compression * (math.Asin(2.0*q-1.0) + math.Pi/2.0) / math.Pi
}

func weightedAverage(x1, w1, x2, w2 float64) float64 {
	if x1 <= x2 {
		return weightedAverageSorted(x1, w1, x2, w2)
	}
	return weightedAverageSorted(x2, w2, x1, w1)
}

func weightedAverageSorted(x1, w1, x2, w2 float64) float64 {
	x := (x1*w1 + x2*w2) / (w1 + w2)
	return math.Max(x1, math.Min(x, x2))
}

func processedSize(size int, compression float64) int {
	if size == 0 {
		return int(2 * math.Ceil(compression))
	}
	return size
}

func unprocessedSize(size int, compression float64) int {
	if size == 0 {
		return int(8 * math.Ceil(compression))
	}
	return size
}
