package instrument

import (
	"context"
	"time"

	"github.com/grafana/dskit/instrument"
)

// TimeRequest reports how much time was spent on the given function  `f`.
//
// It is a thinner version of weaveworks/common/instrument.CollectedRequest that doesn't emit spans.
func TimeRequest(ctx context.Context, method string, col instrument.Collector, toStatusCode func(error) string, f func(context.Context) error) error {
	if toStatusCode == nil {
		toStatusCode = instrument.ErrorCode
	}

	start := time.Now()
	col.Before(ctx, method, start)
	err := f(ctx)
	col.After(ctx, method, toStatusCode(err), start)

	return err
}
