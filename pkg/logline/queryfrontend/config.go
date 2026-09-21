package queryfrontend

import (
	loglineconfig "github.com/grafana/loki/v3/pkg/logline/config"
)

// The logline config types live in pkg/logline/config so that pkg/loki can
// embed the "logline" section without importing this package, which reads the
// assembled Loki config and would otherwise close an import cycle. These
// aliases keep the middleware call sites here reading in terms of their own
// package.
type (
	Config              = loglineconfig.Config
	MiddlewareConfig    = loglineconfig.MiddlewareConfig
	ShardPlanningConfig = loglineconfig.ShardPlanningConfig
)

const (
	defaultShardPlanningEnabled           = loglineconfig.DefaultShardPlanningEnabled
	defaultShardPlanningMinReductionRatio = loglineconfig.DefaultShardPlanningMinReductionRatio

	shardPlanningStrategyPowerOfTwo = "power_of_two"
)
