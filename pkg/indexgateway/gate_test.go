package indexgateway

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"testing/synctest"
	"time"

	"github.com/grafana/dskit/gate"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

func TestNewQueryGate_DisabledAdmitsEverything(t *testing.T) {
	g := newQueryGate(Config{MaxConcurrent: 0}, prometheus.NewRegistry())

	for range 100 {
		require.NoError(t, g.Start(context.Background()))
	}
}

func TestNewQueryGate_RejectsAfterQueueTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := newQueryGate(Config{
			MaxConcurrent:             1,
			MaxConcurrentQueueTimeout: 10 * time.Millisecond,
		}, prometheus.NewRegistry())

		require.NoError(t, g.Start(context.Background()))

		err := g.Start(context.Background())
		require.ErrorIs(t, err, gate.ErrGateTimeout)

		g.Done()
		require.NoError(t, g.Start(context.Background()))
	})
}

func TestMapGateError(t *testing.T) {
	requireShedError(t, mapGateError(gate.ErrGateTimeout))

	sentinel := errors.New("boom")
	require.Equal(t, sentinel, mapGateError(sentinel))
}

// requireShedError asserts that err is the gate's load-shed error, which
// carries an HTTP 503 status through gRPC.
func requireShedError(t *testing.T, err error) {
	t.Helper()
	resp, ok := httpgrpc.HTTPResponseFromError(err)
	require.True(t, ok, "expected an httpgrpc error, got %v", err)
	require.Equal(t, int32(http.StatusServiceUnavailable), resp.Code)
}

// gatedRPCs invokes each IndexGateway RPC with a minimal valid request. Every
// RPC in the service descriptor must have an entry here (enforced by
// TestQueryGate_AllRPCsCovered).
var gatedRPCs = map[string]func(ctx context.Context, g *Gateway) error{
	"GetChunkRef": func(ctx context.Context, g *Gateway) error {
		_, err := g.GetChunkRef(ctx, &logproto.GetChunkRefRequest{Matchers: `{app="foo"}`})
		return err
	},
	"GetSeries": func(ctx context.Context, g *Gateway) error {
		_, err := g.GetSeries(ctx, &logproto.GetSeriesRequest{Matchers: `{app="foo"}`})
		return err
	},
	"LabelNamesForMetricName": func(ctx context.Context, g *Gateway) error {
		_, err := g.LabelNamesForMetricName(ctx, &logproto.LabelNamesForMetricNameRequest{Matchers: `{app="foo"}`})
		return err
	},
	"LabelValuesForMetricName": func(ctx context.Context, g *Gateway) error {
		_, err := g.LabelValuesForMetricName(ctx, &logproto.LabelValuesForMetricNameRequest{Matchers: `{app="foo"}`})
		return err
	},
	"GetStats": func(ctx context.Context, g *Gateway) error {
		_, err := g.GetStats(ctx, &logproto.IndexStatsRequest{Matchers: `{app="foo"}`})
		return err
	},
	"GetVolume": func(ctx context.Context, g *Gateway) error {
		_, err := g.GetVolume(ctx, &logproto.VolumeRequest{Matchers: `{app="foo"}`})
		return err
	},
	"GetShards": func(ctx context.Context, g *Gateway) error {
		return g.GetShards(&logproto.ShardsRequest{Query: `{app="foo"}`}, &fakeShardsServer{ctx: ctx})
	},
}

type fakeShardsServer struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *fakeShardsServer) Context() context.Context            { return s.ctx }
func (s *fakeShardsServer) Send(*logproto.ShardsResponse) error { return nil }

func newGatedGateway(t *testing.T, cfg Config) *Gateway {
	t.Helper()
	gw, err := NewIndexGateway(cfg, mockLimits{}, util_log.Logger, prometheus.NewRegistry(), nil, nil, nil)
	require.NoError(t, err)
	return gw
}

func TestQueryGate_SaturatedRPCsShedLoad(t *testing.T) {
	for name, call := range gatedRPCs {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				gw := newGatedGateway(t, Config{
					MaxConcurrent:             1,
					MaxConcurrentQueueTimeout: 50 * time.Millisecond,
				})

				ctx := user.InjectOrgID(context.Background(), "test")
				require.NoError(t, gw.queryGate.Start(ctx))
				defer gw.queryGate.Done()

				requireShedError(t, call(ctx, gw))
			})
		})
	}
}

func TestQueryGate_AllRPCsCovered(t *testing.T) {
	srv := grpc.NewServer()
	logproto.RegisterIndexGatewayServer(srv, &Gateway{})

	info, ok := srv.GetServiceInfo()["indexgatewaypb.IndexGateway"]
	require.True(t, ok)

	for _, m := range info.Methods {
		require.Contains(t, gatedRPCs, m.Name,
			"RPC %s is not covered by the query gate: new IndexGateway RPCs must acquire queryGate after validation (see gate.go) and be added to gatedRPCs", m.Name)
	}
}

func TestQueryGate_ValidationRejectsBeforeGate(t *testing.T) {
	// Validation rejects malformed matchers without taking a gate slot
	gw := newGatedGateway(t, Config{
		MaxConcurrent:             1,
		MaxConcurrentQueueTimeout: 5 * time.Second,
	})

	ctx := user.InjectOrgID(context.Background(), "test")
	require.NoError(t, gw.queryGate.Start(ctx))
	defer gw.queryGate.Done()

	// GetVolume is omitted: it swallows parse errors for MatchAny requests by
	// design, so a malformed request may legitimately acquire a slot.
	for name, call := range map[string]func() error{
		"GetChunkRef": func() error {
			_, err := gw.GetChunkRef(ctx, &logproto.GetChunkRefRequest{Matchers: `{invalid`})
			return err
		},
		"GetSeries": func() error {
			_, err := gw.GetSeries(ctx, &logproto.GetSeriesRequest{Matchers: `{invalid`})
			return err
		},
		"GetStats": func() error {
			_, err := gw.GetStats(ctx, &logproto.IndexStatsRequest{Matchers: `{invalid`})
			return err
		},
		"LabelNamesForMetricName": func() error {
			_, err := gw.LabelNamesForMetricName(ctx, &logproto.LabelNamesForMetricNameRequest{Matchers: `{invalid`})
			return err
		},
		"LabelValuesForMetricName": func() error {
			_, err := gw.LabelValuesForMetricName(ctx, &logproto.LabelValuesForMetricNameRequest{Matchers: `{invalid`})
			return err
		},
		"GetShards": func() error {
			return gw.GetShards(&logproto.ShardsRequest{Query: `{invalid`}, &fakeShardsServer{ctx: ctx})
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := call()
			require.ErrorIs(t, err, logqlmodel.ErrParse)
		})
	}
}

func TestQueryGate_CanceledWhileQueuedReturnsContextError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gw := newGatedGateway(t, Config{
			MaxConcurrent:             1,
			MaxConcurrentQueueTimeout: time.Minute,
		})

		ctx := user.InjectOrgID(context.Background(), "test")
		require.NoError(t, gw.queryGate.Start(ctx))

		cctx, cancel := context.WithCancel(ctx)
		defer cancel()

		errCh := make(chan error, 1)
		go func() {
			_, err := gw.GetStats(cctx, &logproto.IndexStatsRequest{Matchers: `{app="foo"}`})
			errCh <- err
		}()

		// Returns once GetStats is durably blocked waiting for a slot.
		synctest.Wait()
		cancel()

		require.ErrorIs(t, <-errCh, context.Canceled)

		gw.queryGate.Done()
		require.NoError(t, gw.queryGate.Start(ctx))
	})
}

type panickingQuerier struct {
	IndexQuerier
}

func (panickingQuerier) GetSeries(context.Context, string, model.Time, model.Time, ...*labels.Matcher) ([]labels.Labels, error) {
	panic("boom")
}

func TestQueryGate_SlotReleasedOnHandlerPanic(t *testing.T) {
	gw, err := NewIndexGateway(Config{
		MaxConcurrent:             1,
		MaxConcurrentQueueTimeout: 50 * time.Millisecond,
	}, mockLimits{}, util_log.Logger, prometheus.NewRegistry(), panickingQuerier{}, nil, nil)
	require.NoError(t, err)

	ctx := user.InjectOrgID(context.Background(), "test")

	func() {
		defer func() { require.NotNil(t, recover()) }()
		_, _ = gw.GetSeries(ctx, &logproto.GetSeriesRequest{Matchers: `{app="foo"}`})
	}()

	require.NoError(t, gw.queryGate.Start(ctx))
	gw.queryGate.Done()
}
