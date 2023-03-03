package coprocessor

import (
	"context"

	"github.com/grafana/loki/pkg/loghttp"
)

type QuerierObserver interface {
	PreQuery(ctx context.Context, request loghttp.RangeQuery) (pass bool, err error)
}
