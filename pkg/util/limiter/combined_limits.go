package limiter

import (
	bloombuilder "github.com/grafana/loki/v3/pkg/bloombuild/builder"
	bloomplanner "github.com/grafana/loki/v3/pkg/bloombuild/planner"
	"github.com/grafana/loki/v3/pkg/bloomgateway"
	"github.com/grafana/loki/v3/pkg/compactor"
	"github.com/grafana/loki/v3/pkg/distributor"
	"github.com/grafana/loki/v3/pkg/indexgateway"
	"github.com/grafana/loki/v3/pkg/ingester"
	querier_limits "github.com/grafana/loki/v3/pkg/querier/limits"
	queryrange_limits "github.com/grafana/loki/v3/pkg/querier/queryrange/limits"
	"github.com/grafana/loki/v3/pkg/ruler"
	scheduler_limits "github.com/grafana/loki/v3/pkg/scheduler/limits"
	"github.com/grafana/loki/v3/pkg/storage"
)

type CombinedLimits interface {
	compactor.Limits
	distributor.Limits
	ingester.Limits
	querier_limits.Limits
	queryrange_limits.Limits
	ruler.RulesLimits
	scheduler_limits.Limits
	storage.StoreLimits
	indexgateway.Limits
	bloomgateway.Limits
	bloomplanner.Limits
	bloombuilder.Limits
}
