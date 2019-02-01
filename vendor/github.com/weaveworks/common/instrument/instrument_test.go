package instrument_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"

	"github.com/weaveworks/common/instrument"
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
func (c *spyCollector) Before(method string, start time.Time) {
	c.before = true
}

// After collects when the request is done.
func (c *spyCollector) After(method, statusCode string, start time.Time) {
	c.after = true
	c.afterCode = statusCode
}

func TestCollectedRequest(t *testing.T) {
	c := &spyCollector{}
	fcalled := false
	instrument.CollectedRequest(context.Background(), "test", c, nil, func(_ context.Context) error {
		fcalled = true
		return nil
	})
	assert.True(t, fcalled)
	assert.True(t, c.before)
	assert.True(t, c.after)
	assert.Equal(t, "200", c.afterCode)
}

func TestCollectedRequest_Error(t *testing.T) {
	c := &spyCollector{}
	instrument.CollectedRequest(context.Background(), "test", c, nil, func(_ context.Context) error {
		return errors.New("boom")
	})
	assert.True(t, c.before)
	assert.True(t, c.after)
	assert.Equal(t, "500", c.afterCode)
}
