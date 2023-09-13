package logql

import (
	"time"

	"github.com/prometheus/prometheus/promql"
)

// MatrixStepEvaluator exposes a promql.Matrix as a StepEvaluator.
// Ensure that the resulting StepEvaluator maintains
// the same shape that the parameters expect. For example,
// it's possible that a downstream query returns matches no
// log streams and thus returns an empty matrix.
// However, we still need to ensure that it can be merged effectively
// with another leg that may match series.
// Therefore, we determine our steps from the parameters
// and not the underlying Matrix.
type MatrixStepEvaluator struct {
	start, end, ts time.Time
	step           time.Duration
	m              promql.Matrix
}

func NewMatrixStepEvaluator(start, end time.Time, step time.Duration, m promql.Matrix) *MatrixStepEvaluator {
	return &MatrixStepEvaluator{
		start: start,
		end:   end,
		ts:    start.Add(-step), // will be corrected on first Next() call
		step:  step,
		m:     m,
	}
}

func (m *MatrixStepEvaluator) Next() (bool, int64, StepResult) {
	m.ts = m.ts.Add(m.step)
	if m.ts.After(m.end) {
		return false, 0, nil
	}

	ts := m.ts.UnixNano() / int64(time.Millisecond)
	vec := make(promql.Vector, 0, len(m.m))

	for i, series := range m.m {
		ln := len(series.Floats)

		if ln == 0 || series.Floats[0].T != ts {
			continue
		}

		vec = append(vec, promql.Sample{
			Metric: series.Metric,
			T:      series.Floats[0].T,
			F:      series.Floats[0].F,
		})
		m.m[i].Floats = m.m[i].Floats[1:]
	}

	return true, ts, PromVec(vec)
}

func (m *MatrixStepEvaluator) Close() error { return nil }

func (m *MatrixStepEvaluator) Error() error { return nil }
