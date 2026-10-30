package limiter

import (
	bloombuilder "github.com/grafana/loki/pkg/bloombuild/builder"
	bloomplanner "github.com/grafana/loki/pkg/bloombuild/planner"
	"github.com/grafana/loki/pkg/bloomgateway"
	"github.com/grafana/loki/pkg/compactor"
	"github.com/grafana/loki/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/pkg/distributor"
	"github.com/grafana/loki/pkg/indexgateway"
	"github.com/grafana/loki/pkg/ingester"
	logline_query_limits "github.com/grafana/loki/pkg/logline/queryfrontend/limits"
	"github.com/grafana/loki/pkg/pattern"
	querier_limits "github.com/grafana/loki/pkg/querier/limits"
	queryrange_limits "github.com/grafana/loki/pkg/querier/queryrange/limits"
	"github.com/grafana/loki/pkg/ruler"
	scheduler_limits "github.com/grafana/loki/pkg/scheduler/limits"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/bucket"
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
