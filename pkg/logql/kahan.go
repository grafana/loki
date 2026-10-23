package logql

import (
	"math"

	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/util/kahansum"
)

// kahanSum is a float64 sum with a Kahan-Neumaier compensation term. The zero value is a sum of
// zero.
type kahanSum struct {
	sum, compensation float64
}

// addValue adds v to the sum.
func (s *kahanSum) addValue(v float64) {
	s.sum, s.compensation = kahansum.Inc(v, s.sum, s.compensation)
}

// addPoints adds the value of each point to the sum. It gives the same result as calling
// addValue for each point.
func (s *kahanSum) addPoints(points []promql.FPoint) {
	sum, c := s.sum, s.compensation
	for _, p := range points {
		sum, c = kahansum.Inc(p.F, sum, c)
	}
	s.sum, s.compensation = sum, c
}

// result returns the sum with its compensation.
func (s kahanSum) result() float64 {
	return kahanResult(s.sum, s.compensation)
}

// kahanResult returns sum with its compensation c. It returns an infinite sum as it is.
func kahanResult(sum, c float64) float64 {
	if math.IsInf(sum, 0) {
		return sum
	}
	return sum + c
}

// kahanAvg is the mean of float64 values. The zero value is a mean of no values.
type kahanAvg struct {
	// In direct mode, sum is the running sum. In incremental mode, mean is the running mean and
	// sum is not used. compensation is the compensation of the running sum in direct mode and of
	// the running mean in incremental mode.
	sum, compensation float64
	mean              float64
	count             float64
	incremental       bool
}

// addValue adds v to the mean.
func (m *kahanAvg) addValue(v float64) {
	m.count++
	if m.count == 1 {
		m.sum = v
		return
	}
	// Dividing a compensated sum by the count is more accurate in practice than the incremental
	// mean, so the sum comes first. Prometheus does the same for the avg_over_time function and the
	// avg aggregation.
	if !m.incremental {
		newSum, newC := kahansum.Inc(v, m.sum, m.compensation)
		if !math.IsInf(newSum, 0) {
			m.sum, m.compensation = newSum, newC
			return
		}
		// An infinite sum means that v overflowed the sum or that v is infinite.
		m.incremental = true
		m.mean = m.sum / (m.count - 1)
		m.compensation /= m.count - 1
	}
	// The update scales the old mean and adds v/count. It never subtracts the mean from v, so an
	// infinite mean stays infinite for finite values and for infinities of the same sign. It becomes
	// NaN only for an infinity of the opposite sign.
	q := (m.count - 1) / m.count
	m.mean, m.compensation = kahansum.Inc(v/m.count, q*m.mean, q*m.compensation)
}

// addPoints adds the value of each point to the mean. It gives the same result as calling
// addValue for each point.
func (m *kahanAvg) addPoints(points []promql.FPoint) {
	if m.count == 0 && len(points) > 0 {
		m.sum, m.count = points[0].F, 1
		points = points[1:]
	}

	// While the sum does not overflow, sum on local variables.
	if !m.incremental {
		sum, c := m.sum, m.compensation
		n := 0
		for ; n < len(points); n++ {
			newSum, newC := kahansum.Inc(points[n].F, sum, c)
			if math.IsInf(newSum, 0) {
				break
			}
			sum, c = newSum, newC
		}
		m.sum, m.compensation = sum, c
		m.count += float64(n)
		points = points[n:]
	}

	// From the first point that makes the sum infinite, pass the points to addValue.
	for _, p := range points {
		m.addValue(p.F)
	}
}

// result returns the mean of the added values. It returns NaN when no value was added.
func (m *kahanAvg) result() float64 {
	if m.incremental {
		return m.mean + m.compensation
	}
	return m.sum/m.count + m.compensation/m.count
}
