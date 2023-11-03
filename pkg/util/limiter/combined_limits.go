package limiter

import (
	"github.com/grafana/loki/pkg/bloomgateway"
	"github.com/grafana/loki/pkg/compactor"
	"github.com/grafana/loki/pkg/distributor"
	"github.com/grafana/loki/pkg/ingester"
	querier_limits "github.com/grafana/loki/pkg/querier/limits"
	queryrange_limits "github.com/grafana/loki/pkg/querier/queryrange/limits"
	"github.com/grafana/loki/pkg/ruler"
	"github.com/grafana/loki/pkg/scheduler"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/indexgateway"
)

type CombinedLimits interface {
	compactor.Limits
	distributor.Limits
	ingester.Limits
	querier_limits.Limits
	queryrange_limits.Limits
	ruler.RulesLimits
	scheduler.Limits
	storage.StoreLimits
	indexgateway.Limits
	bloomgateway.Limits
}
