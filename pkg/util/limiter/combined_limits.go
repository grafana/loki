package limiter

import (
	"github.com/grafana/loki/pkg/distributor"
	"github.com/grafana/loki/pkg/ingester"
	"github.com/grafana/loki/pkg/querier"
	"github.com/grafana/loki/pkg/querier/queryrange"
	"github.com/grafana/loki/pkg/ruler"
	"github.com/grafana/loki/pkg/scheduler"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/compactor"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway"
)

type CombinedLimits interface {
	compactor.Limits
	distributor.Limits
	ingester.Limits
	querier.Limits
	queryrange.Limits
	ruler.RulesLimits
	scheduler.Limits
	storage.StoreLimits
	indexgateway.Limits
}
