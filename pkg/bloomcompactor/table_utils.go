package bloomcompactor

import (
	"sort"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/compactor/retention"
	"github.com/grafana/loki/pkg/storage/config"
)

func getIntervalsForTables(tables []string) map[string]model.Interval {
	tablesIntervals := make(map[string]model.Interval, len(tables))
	for _, table := range tables {
		tablesIntervals[table] = retention.ExtractIntervalFromTableName(table)
	}

	return tablesIntervals
}

func sortTablesByRange(tables []string, intervals map[string]model.Interval) {
	sort.Slice(tables, func(i, j int) bool {
		// less than if start time is after produces a most recent first sort order
		return intervals[tables[i]].Start.After(intervals[tables[j]].Start)
	})
}

// TODO: comes from pkg/compactor/compactor.go
func schemaPeriodForTable(cfg config.SchemaConfig, tableName string) (config.PeriodConfig, bool) {
	tableInterval := retention.ExtractIntervalFromTableName(tableName)
	schemaCfg, err := cfg.SchemaForTime(tableInterval.Start)
	if err != nil || schemaCfg.IndexTables.TableFor(tableInterval.Start) != tableName {
		return config.PeriodConfig{}, false
	}

	return schemaCfg, true
}
