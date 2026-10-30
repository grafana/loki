package limiter

import (
	bloombuilder "github.com/grafana/loki/v4/pkg/bloombuild/builder"
	bloomplanner "github.com/grafana/loki/v4/pkg/bloombuild/planner"
	"github.com/grafana/loki/v4/pkg/bloomgateway"
	"github.com/grafana/loki/v4/pkg/compactor"
	"github.com/grafana/loki/v4/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v4/pkg/distributor"
	"github.com/grafana/loki/v4/pkg/indexgateway"
	"github.com/grafana/loki/v4/pkg/ingester"
	logline_query_limits "github.com/grafana/loki/v4/pkg/logline/queryfrontend/limits"
	"github.com/grafana/loki/v4/pkg/pattern"
	querier_limits "github.com/grafana/loki/v4/pkg/querier/limits"
	queryrange_limits "github.com/grafana/loki/v4/pkg/querier/queryrange/limits"
	"github.com/grafana/loki/v4/pkg/ruler"
	scheduler_limits "github.com/grafana/loki/v4/pkg/scheduler/limits"
	"github.com/grafana/loki/v4/pkg/storage"
	"github.com/grafana/loki/v4/pkg/storage/bucket"
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
	pattern.Limits
	bucket.SSEConfigProvider
	logline_query_limits.Limits
	SortSchemaLabels(userID string) []string
	DataObjShardLayout(userID string) streams.ShardLayout
	CompactionPhases(userID string) (runIndex, runLog bool)
}
