package pattern

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
)

func TestPartitionLagTracker_RunOnce(t *testing.T) {
	type observation struct {
		partition int32
		lag       time.Duration
		offset    int64
	}

	tests := []struct {
		name         string
		observations []observation
		expected     []string
	}{{
		name:         "nothing is logged",
		observations: nil,
	}, {
		name: "logs the max and current lag",
		observations: []observation{{
			partition: 3,
			lag:       2 * time.Second,
			offset:    20,
		}, {
			partition: 3,
			lag:       time.Second,
			offset:    21,
		}},
		expected: []string{
			`level=info msg="consumer lag" partition=3 max_lag=2s max_lag_offset=20 current_lag=1s current_lag_offset=21`,
		},
	}, {
		name: "logs one line for each partition",
		observations: []observation{{
			partition: 0,
			lag:       time.Second,
			offset:    1,
		}, {
			partition: 1,
			lag:       2 * time.Second,
			offset:    2,
		}},
		expected: []string{
			`level=info msg="consumer lag" partition=0 max_lag=1s max_lag_offset=1 current_lag=1s current_lag_offset=1`,
			`level=info msg="consumer lag" partition=1 max_lag=2s max_lag_offset=2 current_lag=2s current_lag_offset=2`,
		},
	}}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var buf bytes.Buffer
			tracker := newPartitionLagTracker(log.NewLogfmtLogger(&buf))
			for _, o := range test.observations {
				tracker.Observe(o.partition, o.lag, o.offset)
			}
			require.NoError(t, tracker.runOnce(t.Context()))
			var lines []string
			if out := strings.TrimSpace(buf.String()); out != "" {
				lines = strings.Split(out, "\n")
			}
			// The order partitions are logged in is not defined.
			require.ElementsMatch(t, test.expected, lines)
		})
	}
}
