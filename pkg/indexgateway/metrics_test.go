package indexgateway

import (
	"context"
	"errors"
	"testing"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	shipperindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

// tierRecordingQuerier records file accesses into the request context the way
// the index shipper does, then answers Stats.
type tierRecordingQuerier struct {
	IndexQuerier
	files    []string
	onDemand bool
	err      error
}

func (q tierRecordingQuerier) Stats(ctx context.Context, _ string, _, _ model.Time, _ ...*labels.Matcher) (*logproto.IndexStatsResponse, error) {
	for _, tier := range q.files {
		shipperindex.RecordFileAccess(ctx, tier)
	}
	if q.onDemand {
		shipperindex.RecordOnDemand(ctx)
	}
	if q.err != nil {
		return nil, q.err
	}
	return &logproto.IndexStatsResponse{}, nil
}

func TestGateway_IndexTierMetrics(t *testing.T) {
	for name, tc := range map[string]struct {
		querier              tierRecordingQuerier
		wantTier             string // empty means no observation
		wantMemory, wantDisk float64
	}{
		"memory": {
			querier:    tierRecordingQuerier{files: []string{"memory", "memory"}},
			wantTier:   "memory",
			wantMemory: 2,
		},
		"disk is slower than memory": {
			querier:    tierRecordingQuerier{files: []string{"memory", "disk"}},
			wantTier:   "disk",
			wantMemory: 1,
			wantDisk:   1,
		},
		"on demand": {
			querier:  tierRecordingQuerier{files: []string{"disk"}, onDemand: true},
			wantTier: "on_demand",
			wantDisk: 1,
		},
		"no files": {
			querier:  tierRecordingQuerier{},
			wantTier: "none",
		},
		"error counts accesses but is not observed": {
			querier:  tierRecordingQuerier{files: []string{"disk"}, err: errors.New("boom")},
			wantDisk: 1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			reg := prometheus.NewPedanticRegistry()
			gateway, err := NewIndexGateway(Config{}, mockLimits{}, util_log.Logger, reg, tc.querier, nil, nil)
			require.NoError(t, err)

			ctx := user.InjectOrgID(context.Background(), "test")
			_, err = gateway.GetStats(ctx, &logproto.IndexStatsRequest{Matchers: `{foo="bar"}`})
			require.Equal(t, tc.querier.err, err)

			hist := gateway.metrics.indexRequestDuration
			if tc.wantTier == "" {
				require.Equal(t, 0, testutil.CollectAndCount(hist))
			} else {
				require.Equal(t, 1, testutil.CollectAndCount(hist))
				h, err := writeHistogram(hist.WithLabelValues(opStats, tc.wantTier))
				require.NoError(t, err)
				require.Equal(t, uint64(1), h.GetSampleCount())
				// Native histogram with bucket factor 1.1 is schema 3.
				require.Equal(t, int32(3), h.GetSchema())
			}

			accesses := gateway.metrics.indexFileAccesses
			require.Equal(t, tc.wantMemory, testutil.ToFloat64(accesses.WithLabelValues("memory")))
			require.Equal(t, tc.wantDisk, testutil.ToFloat64(accesses.WithLabelValues("disk")))
		})
	}
}

func writeHistogram(o prometheus.Observer) (*dto.Histogram, error) {
	m, ok := o.(prometheus.Metric)
	if !ok {
		return nil, errors.New("observer is not a metric")
	}
	var pb dto.Metric
	if err := m.Write(&pb); err != nil {
		return nil, err
	}
	return pb.GetHistogram(), nil
}
