package indexgateway

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/stores/series"
)

// mockGatewayClient implements series.GatewayClient for testing.
type mockGatewayClient struct {
	// Overrides default no-op behaviour if set
	getChunkRefFn func(context.Context, *logproto.GetChunkRefRequest) (*logproto.GetChunkRefResponse, error)
}

var _ series.GatewayClient = (*mockGatewayClient)(nil)

func (m *mockGatewayClient) GetChunkRef(ctx context.Context, in *logproto.GetChunkRefRequest) (*logproto.GetChunkRefResponse, error) {
	if m.getChunkRefFn != nil {
		return m.getChunkRefFn(ctx, in)
	}
	return &logproto.GetChunkRefResponse{}, nil
}

func (m *mockGatewayClient) GetSeries(context.Context, *logproto.GetSeriesRequest) (*logproto.GetSeriesResponse, error) {
	return &logproto.GetSeriesResponse{}, nil
}

func (m *mockGatewayClient) LabelNamesForMetricName(context.Context, *logproto.LabelNamesForMetricNameRequest) (*logproto.LabelResponse, error) {
	return &logproto.LabelResponse{}, nil
}

func (m *mockGatewayClient) LabelValuesForMetricName(context.Context, *logproto.LabelValuesForMetricNameRequest) (*logproto.LabelResponse, error) {
	return &logproto.LabelResponse{}, nil
}

func (m *mockGatewayClient) GetStats(context.Context, *logproto.IndexStatsRequest) (*logproto.IndexStatsResponse, error) {
	return &logproto.IndexStatsResponse{}, nil
}

func (m *mockGatewayClient) GetVolume(context.Context, *logproto.VolumeRequest) (*logproto.VolumeResponse, error) {
	return &logproto.VolumeResponse{}, nil
}

func (m *mockGatewayClient) GetShards(context.Context, *logproto.ShardsRequest) (*logproto.ShardsResponse, error) {
	return &logproto.ShardsResponse{}, nil
}

// histogramCount returns the sample count for the histogram time series matching
// the given operation/client/status label combination.
func histogramCount(t *testing.T, reg prometheus.Gatherer, operation, client, status string) uint64 {
	t.Helper()
	mfs, err := reg.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != "loki_index_gateway_tee_request_duration_seconds" {
			continue
		}
		for _, m := range mf.GetMetric() {
			if labelsMatch(m.GetLabel(), map[string]string{
				"operation": operation,
				"client":    client,
				"status":    status,
			}) {
				return m.GetHistogram().GetSampleCount()
			}
		}
	}
	return 0
}

// shadowDroppedCount returns the value of the shadow-requests-dropped counter for
// the given operation label, or 0 if the series has not yet been created.
func shadowDroppedCount(t *testing.T, reg prometheus.Gatherer, operation string) float64 {
	t.Helper()
	mfs, err := reg.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != "loki_index_gateway_tee_shadow_requests_dropped_total" {
			continue
		}
		for _, m := range mf.GetMetric() {
			if labelsMatch(m.GetLabel(), map[string]string{"operation": operation}) {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}

func labelsMatch(actual []*dto.LabelPair, expected map[string]string) bool {
	if len(actual) != len(expected) {
		return false
	}
	for _, lp := range actual {
		if expected[lp.GetName()] != lp.GetValue() {
			return false
		}
	}
	return true
}

// newTestTee builds a TeeGatewayClient backed by the two mocks using a fresh registry.
func newTestTee(t *testing.T, primary, secondary series.GatewayClient) (*TeeGatewayClient, *prometheus.Registry) {
	t.Helper()
	reg := prometheus.NewRegistry()
	tee, err := NewTeeGatewayClient(primary, secondary, reg, log.NewNopLogger())
	require.NoError(t, err)
	return tee, reg
}

// waitSecondary waits until the secondary goroutine has been observed in the metric,
// timing out after one second.
func waitSecondary(t *testing.T, reg prometheus.Gatherer, operation, status string) {
	t.Helper()
	require.Eventually(t, func() bool {
		return histogramCount(t, reg, operation, clientLabelSecondary, status) == 1
	}, time.Second, time.Millisecond)
}

func TestRunTee_BothSucceed(t *testing.T) {
	want := &logproto.GetChunkRefResponse{Refs: []*logproto.ChunkRef{{Fingerprint: 42}}}
	primary := &mockGatewayClient{getChunkRefFn: func(_ context.Context, _ *logproto.GetChunkRefRequest) (*logproto.GetChunkRefResponse, error) {
		return want, nil
	}}
	secondary := &mockGatewayClient{}

	tee, reg := newTestTee(t, primary, secondary)

	got, err := tee.GetChunkRef(context.Background(), &logproto.GetChunkRefRequest{})
	require.NoError(t, err)
	require.Equal(t, want, got)

	require.Equal(t, uint64(1), histogramCount(t, reg, "GetChunkRef", clientLabelPrimary, statusSuccess))
	waitSecondary(t, reg, "GetChunkRef", statusSuccess)
}

func TestRunTee_PrimaryError(t *testing.T) {
	primaryErr := errors.New("primary failure")
	primary := &mockGatewayClient{getChunkRefFn: func(_ context.Context, _ *logproto.GetChunkRefRequest) (*logproto.GetChunkRefResponse, error) {
		return nil, primaryErr
	}}
	secondary := &mockGatewayClient{}

	tee, reg := newTestTee(t, primary, secondary)

	_, err := tee.GetChunkRef(context.Background(), &logproto.GetChunkRefRequest{})
	require.ErrorIs(t, err, primaryErr)

	require.Equal(t, uint64(1), histogramCount(t, reg, "GetChunkRef", clientLabelPrimary, statusError))
	waitSecondary(t, reg, "GetChunkRef", statusSuccess)
}

func TestRunTee_SecondaryError(t *testing.T) {
	want := &logproto.GetChunkRefResponse{Refs: []*logproto.ChunkRef{{Fingerprint: 7}}}
	primary := &mockGatewayClient{getChunkRefFn: func(_ context.Context, _ *logproto.GetChunkRefRequest) (*logproto.GetChunkRefResponse, error) {
		return want, nil
	}}
	secondary := &mockGatewayClient{getChunkRefFn: func(_ context.Context, _ *logproto.GetChunkRefRequest) (*logproto.GetChunkRefResponse, error) {
		return nil, errors.New("secondary failure")
	}}

	tee, reg := newTestTee(t, primary, secondary)

	got, err := tee.GetChunkRef(context.Background(), &logproto.GetChunkRefRequest{})
	require.NoError(t, err)
	require.Equal(t, want, got)

	require.Equal(t, uint64(1), histogramCount(t, reg, "GetChunkRef", clientLabelPrimary, statusSuccess))
	waitSecondary(t, reg, "GetChunkRef", statusError)
}

func TestRunTee_BothError(t *testing.T) {
	primaryErr := errors.New("primary failure")
	primary := &mockGatewayClient{getChunkRefFn: func(_ context.Context, _ *logproto.GetChunkRefRequest) (*logproto.GetChunkRefResponse, error) {
		return nil, primaryErr
	}}
	secondary := &mockGatewayClient{getChunkRefFn: func(_ context.Context, _ *logproto.GetChunkRefRequest) (*logproto.GetChunkRefResponse, error) {
		return nil, errors.New("secondary failure")
	}}

	tee, reg := newTestTee(t, primary, secondary)

	_, err := tee.GetChunkRef(context.Background(), &logproto.GetChunkRefRequest{})
	require.ErrorIs(t, err, primaryErr)

	require.Equal(t, uint64(1), histogramCount(t, reg, "GetChunkRef", clientLabelPrimary, statusError))
	waitSecondary(t, reg, "GetChunkRef", statusError)
}

// TestRunTee_ShadowConcurrencyLimit fills the shadow semaphore with in-flight
// secondary calls and verifies that any further requests bypass the shadow: the
// primary still succeeds, the drop counter increments, and the secondary never
// receives the extra call.
func TestRunTee_ShadowConcurrencyLimit(t *testing.T) {
	// Construct a primary gateway client where requests immediately succeed, and a secondary gateway client where
	// requests hang until we release them.
	var secondaryCalls atomic.Int64
	release := make(chan struct{})
	secondary := &mockGatewayClient{getChunkRefFn: func(_ context.Context, _ *logproto.GetChunkRefRequest) (*logproto.GetChunkRefResponse, error) {
		secondaryCalls.Add(1)
		<-release
		return &logproto.GetChunkRefResponse{}, nil
	}}
	primary := &mockGatewayClient{}

	tee, reg := newTestTee(t, primary, secondary)

	// Saturate the shadow semaphore with 1000 blocked secondary calls.
	for i := 0; i < 1000; i++ {
		_, err := tee.GetChunkRef(context.Background(), &logproto.GetChunkRefRequest{})
		require.NoError(t, err)
	}
	require.Eventually(t, func() bool {
		return secondaryCalls.Load() == int64(1000)
	}, 5*time.Second, time.Millisecond, "expected 1000 secondary calls to be in flight")

	// The next call must still succeed on the primary, but the shadow must be dropped.
	_, err := tee.GetChunkRef(context.Background(), &logproto.GetChunkRefRequest{})
	require.NoError(t, err)
	require.Equal(t, float64(1), shadowDroppedCount(t, reg, "GetChunkRef"))

	// Give any spurious secondary invocation a chance to happen, then confirm it didn't.
	time.Sleep(50 * time.Millisecond)
	require.Equal(t, int64(1000), secondaryCalls.Load(),
		"secondary must not receive calls beyond the concurrency limit")

	// Unblock the secondaries and wait for them to finish so we don't leak goroutines.
	close(release)
	require.Eventually(t, func() bool {
		return histogramCount(t, reg, "GetChunkRef", clientLabelSecondary, statusSuccess) == uint64(1000)
	}, 5*time.Second, time.Millisecond)
}
