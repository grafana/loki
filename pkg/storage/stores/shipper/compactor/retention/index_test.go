package retention

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk"
)

func Test_schemaPeriodForTable(t *testing.T) {
	indexFromTime := func(t time.Time) string {
		return fmt.Sprintf("%d", t.Unix()/int64(24*time.Hour/time.Second))
	}
	tests := []struct {
		name          string
		config        storage.SchemaConfig
		tableName     string
		expected      chunk.PeriodConfig
		expectedFound bool
	}{
		{"out of scope", schemaCfg, "index_" + indexFromTime(start.Time().Add(-24*time.Hour)), chunk.PeriodConfig{}, false},
		{"first table", schemaCfg, "index_" + indexFromTime(dayFromTime(start).Time.Time()), schemaCfg.Configs[0], true},
		{"4 hour after first table", schemaCfg, "index_" + indexFromTime(dayFromTime(start).Time.Time().Add(4*time.Hour)), schemaCfg.Configs[0], true},
		{"second schema", schemaCfg, "index_" + indexFromTime(dayFromTime(start.Add(28*time.Hour)).Time.Time()), schemaCfg.Configs[1], true},
		{"now", schemaCfg, "index_" + indexFromTime(time.Now()), schemaCfg.Configs[2], true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual, actualFound := schemaPeriodForTable(tt.config, tt.tableName)
			require.Equal(t, tt.expected, actual)
			require.Equal(t, tt.expectedFound, actualFound)
		})
	}
}
