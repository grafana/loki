package bloomcompactor

import (
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/compactor/retention"
)

func getIntervalsForTables(tables []string) map[string]model.Interval {
	tablesIntervals := make(map[string]model.Interval, len(tables))
	for _, table := range tables {
		tablesIntervals[table] = retention.ExtractIntervalFromTableName(table)
	}

	return tablesIntervals
}
