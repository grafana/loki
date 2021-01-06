package logql

import (
	"time"

	"github.com/prometheus/prometheus/promql"
)

// MatrixStepper exposes a promql.Matrix as a StepEvaluator.
// Ensure that the resulting StepEvaluator maintains
// the same shape that the parameters expect. For example,
// it's possible that a downstream query returns matches no
// log streams and thus returns an empty matrix.
// However, we still need to ensure that it can be merged effectively
// with another leg that may match series.
// Therefore, we determine our steps from the parameters
// and not the underlying Matrix.
type MatrixStepper struct {
	start, end, ts time.Time
	step           time.Duration
	m              promql.Matrix
}

func NewMatrixStepper(start, end time.Time, step time.Duration, m promql.Matrix) *MatrixStepper {
	return &MatrixStepper{
		start: start,
		end:   end,
		ts:    start.Add(-step), // will be corrected on first Next() call
		step:  step,
		m:     m,
	}
}

func (m *MatrixStepper) Next() (bool, int64, promql.Vector) {
	m.ts = m.ts.Add(m.step)
	if !m.ts.Before(m.end) {
		return false, 0, nil
	}

	ts := m.ts.UnixNano() / int64(time.Millisecond)
	vec := make(promql.Vector, 0, len(m.m))

	for i, series := range m.m {
		ln := len(series.Points)

		if ln == 0 || series.Points[0].T != ts {
			vec = append(vec, promql.Sample{
				Point: promql.Point{
					T: ts,
					V: 0,
				},
				Metric: series.Metric,
			})
			continue
		}

		vec = append(vec, promql.Sample{
			Point:  series.Points[0],
			Metric: series.Metric,
		})
		m.m[i].Points = m.m[i].Points[1:]
	}

	return true, ts, vec
}

func (m *MatrixStepper) Close() error { return nil }

func (m *MatrixStepper) Error() error { return nil }
