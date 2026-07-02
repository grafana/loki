package indexgateway

import (
	"context"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/stores/series"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

const (
	clientLabelPrimary   = "primary"
	clientLabelSecondary = "secondary"
	statusSuccess        = "success"
	statusError          = "error"
)

// TeeGatewayClient wraps a primary and secondary GatewayClient.
// Each request is sent to both clients, but only the primary's response is returned.
// The secondary receives requests in a fire-and-forget goroutine so it never delays the caller.
// This is intended for testing different configurations of the index gateway client, or index
// gateways themselves.
// To do this, spin up a secondary set of index gateways with your experimental configuration.
type TeeGatewayClient struct {
	primary         series.GatewayClient
	secondary       series.GatewayClient
	logger          log.Logger
	requestDuration *prometheus.HistogramVec
}

// NewTeeGatewayClient creates a TeeGatewayClient that fans out to both primary and secondary.
// Callers are responsible for stopping both clients independently.
func NewTeeGatewayClient(primary, secondary series.GatewayClient, r prometheus.Registerer, logger log.Logger) (*TeeGatewayClient, error) {
	return &TeeGatewayClient{
		primary:   primary,
		secondary: secondary,
		logger:    logger,
		requestDuration: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace:                       constants.Loki,
			Name:                            "index_gateway_tee_request_duration_seconds",
			Help:                            "Duration of index gateway requests issued by the tee client, labelled by operation, client (primary or secondary), and status.",
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}, []string{"operation", "client", "status"}),
	}, nil
}

func (t *TeeGatewayClient) observe(operation, client string, start time.Time, err error) {
	status := statusSuccess
	if err != nil {
		status = statusError
	}
	t.requestDuration.WithLabelValues(operation, client, status).Observe(time.Since(start).Seconds())
}

func runTee[Req, Resp any](
	ctx context.Context,
	t *TeeGatewayClient,
	operation string,
	req Req,
	fn func(series.GatewayClient, context.Context, Req) (Resp, error),
) (Resp, error) {
	go func() {
		start := time.Now()
		_, err := fn(t.secondary, context.WithoutCancel(ctx), req)
		t.observe(operation, clientLabelSecondary, start, err)
		if err != nil {
			level.Warn(t.logger).Log("msg", "tee index gateway request failed", "operation", operation, "err", err)
		}
	}()
	start := time.Now()
	resp, err := fn(t.primary, ctx, req)
	t.observe(operation, clientLabelPrimary, start, err)
	return resp, err
}

func (t *TeeGatewayClient) GetChunkRef(ctx context.Context, in *logproto.GetChunkRefRequest) (*logproto.GetChunkRefResponse, error) {
	return runTee(ctx, t, "GetChunkRef", in, series.GatewayClient.GetChunkRef)
}

func (t *TeeGatewayClient) GetSeries(ctx context.Context, in *logproto.GetSeriesRequest) (*logproto.GetSeriesResponse, error) {
	return runTee(ctx, t, "GetSeries", in, series.GatewayClient.GetSeries)
}

func (t *TeeGatewayClient) LabelNamesForMetricName(ctx context.Context, in *logproto.LabelNamesForMetricNameRequest) (*logproto.LabelResponse, error) {
	return runTee(ctx, t, "LabelNamesForMetricName", in, series.GatewayClient.LabelNamesForMetricName)
}

func (t *TeeGatewayClient) LabelValuesForMetricName(ctx context.Context, in *logproto.LabelValuesForMetricNameRequest) (*logproto.LabelResponse, error) {
	return runTee(ctx, t, "LabelValuesForMetricName", in, series.GatewayClient.LabelValuesForMetricName)
}

func (t *TeeGatewayClient) GetStats(ctx context.Context, in *logproto.IndexStatsRequest) (*logproto.IndexStatsResponse, error) {
	return runTee(ctx, t, "GetStats", in, series.GatewayClient.GetStats)
}

func (t *TeeGatewayClient) GetVolume(ctx context.Context, in *logproto.VolumeRequest) (*logproto.VolumeResponse, error) {
	return runTee(ctx, t, "GetVolume", in, series.GatewayClient.GetVolume)
}

func (t *TeeGatewayClient) GetShards(ctx context.Context, in *logproto.ShardsRequest) (*logproto.ShardsResponse, error) {
	return runTee(ctx, t, "GetShards", in, series.GatewayClient.GetShards)
}
