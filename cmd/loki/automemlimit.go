package main

import (
	"context"
	"time"

	"github.com/KimMachineGun/automemlimit/memlimit"
	"github.com/go-kit/log"

	"github.com/grafana/loki/v3/pkg/loki"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

// setAutoMemLimit sets GOMEMLIMIT from the memory limit and keeps it in step with
// limit changes, such as an in-place pod resize, until ctx is done. An explicit
// GOMEMLIMIT env var wins, and AUTOMEMLIMIT overrides the default 90% ratio.
// memlimit logs the outcome, including errors.
func setAutoMemLimit(ctx context.Context, cfg loki.Config, logger log.Logger) {
	if !cfg.AutoMemLimitEnabled {
		return
	}
	_, _ = memlimit.Set(
		memlimit.WithProvider(memlimit.ApplyFallback(memlimit.FromCgroup, memlimit.FromSystem)),
		memlimit.WithRefreshInterval(ctx, 15*time.Second),
		memlimit.WithLogger(util_log.SlogFromGoKit(logger)),
	)
}
