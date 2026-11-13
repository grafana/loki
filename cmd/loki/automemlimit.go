package main

import (
	"context"
	"time"

	"github.com/KimMachineGun/automemlimit/memlimit"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	"github.com/grafana/loki/v3/pkg/loki"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

// setAutoMemLimit sets GOMEMLIMIT from the memory limit and keeps it in step with
// limit changes, such as an in-place pod resize. An explicit GOMEMLIMIT env var wins.
func setAutoMemLimit(cfg loki.Config, logger log.Logger) {
	if !cfg.AutoMemLimitEnabled {
		return
	}
	limit, err := memlimit.Set(
		memlimit.WithRatio(cfg.AutoMemLimitRatio),
		memlimit.WithProvider(memlimit.ApplyFallback(memlimit.FromCgroup, memlimit.FromSystem)),
		memlimit.WithRefreshInterval(context.Background(), 15*time.Second),
		memlimit.WithLogger(util_log.SlogFromGoKit(logger)),
	)
	if err != nil {
		level.Warn(logger).Log("msg", "failed to set GOMEMLIMIT automatically", "err", err)
		return
	}
	level.Info(logger).Log("msg", "set GOMEMLIMIT automatically", "bytes", limit, "ratio", cfg.AutoMemLimitRatio)
}
