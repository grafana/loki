package engine

import (
	_ "github.com/grafana/loki/v3/pkg/dataobj"
	_ "github.com/grafana/loki/v3/pkg/dataobj/metastore"
	_ "github.com/grafana/loki/v3/pkg/engine/internal/executor"
	_ "github.com/grafana/loki/v3/pkg/engine/internal/scheduler"
	_ "github.com/grafana/loki/v3/pkg/engine/internal/scheduler/schedulerstat"
	_ "github.com/grafana/loki/v3/pkg/engine/internal/worker/workerstat"
	_ "github.com/grafana/loki/v3/pkg/engine/internal/workflow"
	"github.com/grafana/loki/v3/pkg/xcap"
)

// Import each statistic-owning package so all stable xcap IDs are registered
// before an engine process accepts worker captures.
func init() {
	xcap.MustValidateStatisticRegistry()
}
