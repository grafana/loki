// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2021 Datadog, Inc.

package stat

import (
	"fmt"
	"math"
)

// SummaryStatistics keeps track of the count, the sum, the min and the max of
// recorded values. We use a compensated sum to avoid accumulating rounding
// errors (see https://en.wikipedia.org/wiki/Kahan_summation_algorithm).
type SummaryStatistics struct {
	count           float64
	sum             float64
	sumCompensation float64
	simpleSum       float64
	min             float64
	max             float64
}

func NewSummaryStatistics() *SummaryStatistics {
	return &SummaryStatistics{
		count:           0,
		sum:             0,
		sumCompensation: 0,
		simpleSum:       0,
		min:             math.Inf(1),
		max:             math.Inf(-1),
	}
}

// NewSummaryStatisticsFromData constructs SummaryStatistics from the provided data.
func NewSummaryStatisticsFromData(count, sum, min, max float64) (*SummaryStatistics, error) {
	if !(count >= 0) {
		return nil, fmt.Errorf("count (%g) must be positive or zero", count)
	}
	if count > 0 && min > max {
		return nil, fmt.Errorf("min (%g) cannot be greater than max (%g) if count (%g) is positive", min, max, count)
	}
	if count == 0 && (min != math.Inf(1) || max != math.Inf(-1)) {
		return nil, fmt.Errorf("empty summary statistics must have min (%g) and max (%g) equal to positive and negative infinities respectively", min, max)
	}
	return &SummaryStatistics{
		count:           count,
		sum:             sum,
		sumCompensation: 0,
		simpleSum:       sum,
		min:             min,
		max:             max,
	}, nil
}

func (s *SummaryStatistics) Count() float64 {
	return s.count
}

func (s *SummaryStatistics) Sum() float64 {
	// Better error bounds to add both terms as the final sum
	tmp := s.sum + s.sumCompensation
	if math.IsNaN(tmp) && math.IsInf(s.simpleSum, 0) {
		// If the compensated sum is spuriously NaN from accumulating one or more same-signed infinite
		// values, return the correctly-signed infinity stored in simpleSum.
		return s.simpleSum
	} else {
		return tmp
	}
}

func (s *SummaryStatistics) Min() float64 {
	return s.min
}

func (s *SummaryStatistics) Max() float64 {
	return s.max
}

func (s *SummaryStatistics) Add(value, count float64) {
	s.AddToCount(count)
	s.AddToSum(value * count)
	if value < s.min {
		s.min = value
	}
	if value > s.max {
		s.max = value
	}
}

func (s *SummaryStatistics) AddToCount(addend float64) {
	s.count += addend
}

func (s *SummaryStatistics) AddToSum(addend float64) {
	s.sumWithCompensation(addend)
	s.simpleSum += addend
}

func (s *SummaryStatistics) MergeWith(o *SummaryStatistics) {
	s.count += o.count
	s.sumWithCompensation(o.sum)
	s.sumWithCompensation(o.sumCompensation)
	s.simpleSum += o.simpleSum
	if o.min < s.min {
		s.min = o.min
	}
	if o.max > s.max {
		s.max = o.max
	}
}

func (s *SummaryStatistics) sumWithCompensation(value float64) {
	tmp := value - s.sumCompensation
	velvel := s.sum + tmp // little wolf of rounding error
	s.sumCompensation = velvel - s.sum - tmp
	s.sum = velvel
}

// Reweight adjusts the statistics so that they are equal to what they would
// have been if AddWithCount had been called with counts multiplied by factor.
func (s *SummaryStatistics) Reweight(factor float64) {
	s.count *= factor
	s.sum *= factor
	s.sumCompensation *= factor
	s.simpleSum *= factor
	if factor == 0 {
		s.min = math.Inf(1)
		s.max = math.Inf(-1)
	}
}

// Rescale adjusts the statistics so that they are equal to what they would have
// been if AddWithCount had been called with values multiplied by factor.
func (s *SummaryStatistics) Rescale(factor float64) {
	s.sum *= factor
	s.sumCompensation *= factor
	s.simpleSum *= factor
	if factor > 0 {
		s.min *= factor
		s.max *= factor
	} else if factor < 0 {
		tmp := s.max * factor
		s.max = s.min * factor
		s.min = tmp
	} else if s.count != 0 {
		s.min = 0
		s.max = 0
	}
}

func (s *SummaryStatistics) Clear() {
	s.count = 0
	s.sum = 0
	s.sumCompensation = 0
	s.simpleSum = 0
	s.min = math.Inf(1)
	s.max = math.Inf(-1)
}

func (s *SummaryStatistics) Copy() *SummaryStatistics {
	return &SummaryStatistics{
		count:           s.count,
		sum:             s.sum,
		sumCompensation: s.sumCompensation,
		simpleSum:       s.simpleSum,
		min:             s.min,
		max:             s.max,
	}
}
