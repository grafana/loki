package instrument

// Source: https://github.com/weaveworks/common/blob/master/instrument/instrument_test.go

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/grafana/dskit/instrument"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHistogramCollector(t *testing.T) {
	m := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "test",
		Subsystem: "instrumentation",
		Name:      "foo",
		Help:      "",
		Buckets:   prometheus.DefBuckets,
	}, instrument.HistogramCollectorBuckets)
	c := instrument.NewHistogramCollector(m)
	assert.NotNil(t, c)
}

type spyCollector struct {
	before    bool
	after     bool
	afterCode string
}

func (c *spyCollector) Register() {
}

// Before collects for the upcoming request.
func (c *spyCollector) Before(_ context.Context, _ string, _ time.Time) {
	c.before = true
}

// After collects when the request is done.
func (c *spyCollector) After(_ context.Context, _, statusCode string, _ time.Time) {
	c.after = true
	c.afterCode = statusCode
}

func TestCollectedRequest(t *testing.T) {
	c := &spyCollector{}
	fcalled := false
	require.NoError(t, instrument.CollectedRequest(context.Background(), "test", c, nil, func(_ context.Context) error {
		fcalled = true
		return nil
	}))
	assert.True(t, fcalled)
	assert.True(t, c.before)
	assert.True(t, c.after)
	assert.Equal(t, "200", c.afterCode)
}

func TestCollectedRequest_Error(t *testing.T) {
	c := &spyCollector{}
	require.Error(t, instrument.CollectedRequest(context.Background(), "test", c, nil, func(_ context.Context) error {
		return errors.New("boom")
	}))
	assert.True(t, c.before)
	assert.True(t, c.after)
	assert.Equal(t, "500", c.afterCode)
}
