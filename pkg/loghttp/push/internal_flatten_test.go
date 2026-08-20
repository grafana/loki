package push

import (
	"context"

	"github.com/go-kit/log"
	"go.opentelemetry.io/collector/pdata/plog"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/runtime"
)

// flattenRequest expands a parsed request back into the flat form.
//
// The parser now builds the nested representation, but what a push ultimately stores is
// unchanged, and that is what the expectations across these tests describe. Flattening here
// lets those expectations keep their meaning and turns each of them into an equivalence
// check: the nested parse, expanded, must equal exactly what the parser used to hand over.
// Tests that care about the nesting itself assert on the nested form directly.
func flattenRequest(req *logproto.InternalPushRequest) *logproto.PushRequest {
	if req == nil {
		return nil
	}
	out := &logproto.PushRequest{}
	if len(req.Streams) == 0 {
		return out
	}
	out.Streams = make([]logproto.Stream, 0, len(req.Streams))
	for i := range req.Streams {
		out.Streams = append(out.Streams, req.Streams[i].ToStream())
	}
	return out
}

// otlpToLokiPushRequestFlat is the OTLP parser followed by that expansion.
func otlpToLokiPushRequestFlat(ctx context.Context, ld plog.Logs, userID string, otlpConfig OTLPConfig, tenantConfigs *runtime.TenantConfigs, discoverServiceName []string, tracker UsageTracker, stats *Stats, logger log.Logger, streamResolver StreamResolver, format string) (*logproto.PushRequest, error) {
	req, err := otlpToLokiPushRequest(ctx, ld, userID, otlpConfig, tenantConfigs, discoverServiceName, tracker, stats, logger, streamResolver, format)
	if err != nil {
		return nil, err
	}
	return flattenRequest(req), nil
}
