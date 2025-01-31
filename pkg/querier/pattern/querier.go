package pattern

import (
	"context"

	"github.com/grafana/loki/v3/pkg/logproto"
)

type PatterQuerier interface {
	Patterns(ctx context.Context, req *logproto.QueryPatternsRequest) (*logproto.QueryPatternsResponse, error)
}
