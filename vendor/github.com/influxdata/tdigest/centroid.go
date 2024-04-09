package tdigest

import (
	"fmt"
	"sort"
)

// ErrWeightLessThanZero is used when the weight is not able to be processed.
const ErrWeightLessThanZero = Error("centroid weight cannot be less than zero")

// Error is a domain error encountered while processing tdigests
type Error string

func (e Error) Error() string {
	return string(e)
}

// Centroid average position of all points in a shape
type Centroid struct {
	Mean   float64
	Weight float64
}

func (c *Centroid) String() string {
	return fmt.Sprintf("{mean: %f weight: %f}", c.Mean, c.Weight)
}

// Add averages the two centroids together and update this centroid
func (c *Centroid) Add(r Centroid) error {
	if r.Weight < 0 {
		return ErrWeightLessThanZero
	}
	if c.Weight != 0 {
		c.Weight += r.Weight
		c.Mean += r.Weight * (r.Mean - c.Mean) / c.Weight
	} else {
		c.Weight = r.Weight
		c.Mean = r.Mean
	}
	return nil
}

// CentroidList is sorted by the Mean of the centroid, ascending.
type CentroidList []Centroid

// Clear clears the list.
func (l *CentroidList) Clear() {
	*l = (*l)[:0]
}

func (l CentroidList) Len() int           { return len(l) }
func (l CentroidList) Less(i, j int) bool { return l[i].Mean < l[j].Mean }
func (l CentroidList) Swap(i, j int)      { l[i], l[j] = l[j], l[i] }

// NewCentroidList creates a priority queue for the centroids
func NewCentroidList(centroids []Centroid) CentroidList {
	l := CentroidList(centroids)
	sort.Sort(l)
	return l
}
