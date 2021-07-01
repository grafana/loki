package compactor

import (
	"fmt"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/prometheus/common/model"

	"github.com/stretchr/testify/require"
)

func TestIsDefaults(t *testing.T) {
	for i, tc := range []struct {
		in  *Config
		out bool
	}{
		{&Config{
			WorkingDirectory: "/tmp",
		}, false},
		{&Config{}, false},
		{&Config{
			SharedStoreKeyPrefix:      "index/",
			CompactionInterval:        10 * time.Minute,
			RetentionDeleteDelay:      2 * time.Hour,
			RetentionDeleteWorkCount:  150,
			DeleteRequestCancelPeriod: 24 * time.Hour,
		}, true},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			require.Equal(t, tc.out, tc.in.IsDefaults())
		})
	}
}

func TestExtractIntervalFromTableName(t *testing.T) {
	periodicTableConfig := chunk.PeriodicTableConfig{
		Prefix: "dummy",
		Period: 24 * time.Hour,
	}

	const millisecondsInDay = model.Time(24 * time.Hour / time.Millisecond)

	calculateInterval := func(tm model.Time) (m model.Interval) {
		m.Start = tm - tm%millisecondsInDay
		m.End = m.Start + millisecondsInDay
		return
	}

	for i, tc := range []struct {
		tableName        string
		expectedInterval model.Interval
	}{
		{
			tableName:        periodicTableConfig.TableFor(model.Now()),
			expectedInterval: calculateInterval(model.Now()),
		},
		{
			tableName:        periodicTableConfig.TableFor(model.Now().Add(-24 * time.Hour)),
			expectedInterval: calculateInterval(model.Now().Add(-24 * time.Hour)),
		},
		{
			tableName:        periodicTableConfig.TableFor(model.Now().Add(-24 * time.Hour).Add(time.Minute)),
			expectedInterval: calculateInterval(model.Now().Add(-24 * time.Hour).Add(time.Minute)),
		},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			require.Equal(t, tc.expectedInterval, extractIntervalFromTableName(tc.tableName))
		})
	}

}
