package ruler

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

type mockEval struct{}

func (m mockEval) Eval(context.Context, string, time.Time) (*logqlmodel.Result, error) {
	return nil, nil
}

type fakeHasher struct {
	buf []byte

	responses map[string]uint32
	err       error
}

func (f *fakeHasher) Write(p []byte) (n int, err error) {
	f.buf = p
	return 0, f.err
}

func (f *fakeHasher) Sum(_ []byte) []byte { return nil }
func (f *fakeHasher) Reset()              {}
func (f *fakeHasher) Size() int           { return 32 }
func (f *fakeHasher) BlockSize() int      { return 32 }
func (f *fakeHasher) Sum32() uint32 {
	h, ok := f.responses[string(f.buf)]
	if !ok {
		panic(fmt.Sprintf("unexpected buffer: %b", f.buf))
	}

	return h
}

func TestEvaluationWithJitter(t *testing.T) {
	const maxJitter = 2 * time.Second
	const ratio = 0.5 // we expect a sleep of half of maxJitter: 1s
	const query = "some logql query..."

	logger := log.NewNopLogger()
	eval := NewEvaluatorWithJitter(mockEval{}, maxJitter, &fakeHasher{
		responses: map[string]uint32{
			query: uint32(math.Ceil(ratio * math.MaxUint32)),
		},
	}, logger)

	then := time.Now()
	_, _ = eval.Eval(context.Background(), query, time.Now())
	since := time.Since(then)

	actual := ratio * maxJitter.Seconds()
	require.GreaterOrEqualf(t, since.Seconds(), actual, "expected the eval to sleep for at least %.2fs", actual)
}

func TestConsistentJitter(t *testing.T) {
	const maxJitter = 2 * time.Second

	logger := log.NewNopLogger()

	eval := NewEvaluatorWithJitter(mockEval{}, maxJitter, &fakeHasher{
		responses: map[string]uint32{
			"query A": uint32(math.Ceil(0.75 * math.MaxUint32)),
			"query B": uint32(math.Ceil(0.25 * math.MaxUint32)),
			"query C": uint32(math.Ceil(0.5 * math.MaxUint32)),
		},
	}, logger)

	require.IsType(t, &EvaluatorWithJitter{}, eval)

	// no matter how many times we calculate the jitter, it will always be the same
	require.EqualValues(t, 1.5, eval.(*EvaluatorWithJitter).calculateJitter("query A", logger).Seconds())
	require.EqualValues(t, 1.5, eval.(*EvaluatorWithJitter).calculateJitter("query A", logger).Seconds())
	require.EqualValues(t, 0.5, eval.(*EvaluatorWithJitter).calculateJitter("query B", logger).Seconds())
	require.EqualValues(t, 0.5, eval.(*EvaluatorWithJitter).calculateJitter("query B", logger).Seconds())
	require.EqualValues(t, 1, eval.(*EvaluatorWithJitter).calculateJitter("query C", logger).Seconds())
	require.EqualValues(t, 1, eval.(*EvaluatorWithJitter).calculateJitter("query C", logger).Seconds())
}

func TestEvaluationWithNoJitter(t *testing.T) {
	const jitter = 0

	inner := mockEval{}
	eval := NewEvaluatorWithJitter(inner, jitter, nil, nil)

	// return the inner evaluator if jitter is disabled
	require.Exactly(t, inner, eval)
}

func TestEvaluationWithHashError(t *testing.T) {
	const maxJitter = 3 * time.Second

	inner := mockEval{}
	logger := log.NewNopLogger()

	eval := NewEvaluatorWithJitter(inner, maxJitter,
		// provide a hasher that will error when hashing
		&fakeHasher{err: errors.New("some hash error")},
		logger)
	require.EqualValues(t, 0, eval.(*EvaluatorWithJitter).calculateJitter("query C", logger).Seconds())
}
