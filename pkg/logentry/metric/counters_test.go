package metric

import (
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
)

var (
	counterTestTrue  = true
	counterTestFalse = false
	counterTestVal   = "some val"
)

func Test_validateCounterConfig(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		config CounterConfig
		err    error
	}{
		{"invalid action",
			CounterConfig{
				Action: "del",
			},
			errors.Errorf(ErrCounterInvalidAction, "del"),
		},
		{"invalid counter match all",
			CounterConfig{
				MatchAll: &counterTestTrue,
				Value:    &counterTestVal,
				Action:   "inc",
			},
			errors.New(ErrCounterInvalidMatchAll),
		},
		{"invalid counter match bytes",
			CounterConfig{
				MatchAll:   nil,
				CountBytes: &counterTestTrue,
				Action:     "add",
			},
			errors.New(ErrCounterInvalidCountBytes),
		},
		{"invalid counter match bytes action",
			CounterConfig{
				MatchAll:   &counterTestTrue,
				CountBytes: &counterTestTrue,
				Action:     "inc",
			},
			errors.New(ErrCounterInvalidCountBytesAction),
		},
		{"valid counter match bytes",
			CounterConfig{
				MatchAll:   &counterTestTrue,
				CountBytes: &counterTestTrue,
				Action:     "add",
			},
			nil,
		},
		{"valid",
			CounterConfig{
				Value:  &counterTestVal,
				Action: "inc",
			},
			nil,
		},
		{"valid match all is false",
			CounterConfig{
				MatchAll: &counterTestFalse,
				Value:    &counterTestVal,
				Action:   "inc",
			},
			nil,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateCounterConfig(&tt.config)
			if ((err != nil) && (err.Error() != tt.err.Error())) || (err == nil && tt.err != nil) {
				t.Errorf("Metrics stage validation error, expected error = %v, actual error = %v", tt.err, err)
				return
			}
		})
	}
}

func TestCounterExpiration(t *testing.T) {
	t.Parallel()
	cfg := CounterConfig{
		Action: "inc",
	}

	cnt, err := NewCounters("test1", "HELP ME!!!!!", cfg, 1)
	assert.Nil(t, err)

	// Create a label and increment the counter
	lbl1 := model.LabelSet{}
	lbl1["test"] = "i don't wanna make this a constant"
	cnt.With(lbl1).Inc()

	// Collect the metrics, should still find the metric in the map
	collect(cnt)
	assert.Contains(t, cnt.metrics, lbl1.Fingerprint())

	time.Sleep(1100 * time.Millisecond) // Wait just past our max idle of 1 sec

	//Add another counter with new label val
	lbl2 := model.LabelSet{}
	lbl2["test"] = "eat this linter"
	cnt.With(lbl2).Inc()

	// Collect the metrics, first counter should have expired and removed, second should still be present
	collect(cnt)
	assert.NotContains(t, cnt.metrics, lbl1.Fingerprint())
	assert.Contains(t, cnt.metrics, lbl2.Fingerprint())
}

func collect(c prometheus.Collector) {
	done := make(chan struct{})
	collector := make(chan prometheus.Metric)

	go func() {
		defer close(done)
		c.Collect(collector)
	}()

	for {
		select {
		case <-collector:
		case <-done:
			return
		}
	}
}
