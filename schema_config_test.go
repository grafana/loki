package chunk

import (
	"reflect"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestHourlyBuckets(t *testing.T) {
	const (
		userID     = "0"
		metricName = model.LabelValue("name")
		tableName  = "table"
	)
	var cfg = PeriodConfig{
		IndexTables: PeriodicTableConfig{Prefix: tableName},
	}

	type args struct {
		from    model.Time
		through model.Time
	}
	tests := []struct {
		name string
		args args
		want []Bucket
	}{
		{
			"0 hour window",
			args{
				from:    model.TimeFromUnix(0),
				through: model.TimeFromUnix(0),
			},
			[]Bucket{},
		},
		{
			"30 minute window",
			args{
				from:    model.TimeFromUnix(0),
				through: model.TimeFromUnix(1800),
			},
			[]Bucket{{
				from:      0,
				through:   1800 * 1000, // ms
				tableName: "table",
				hashKey:   "0:0",
			}},
		},
		{
			"1 hour window",
			args{
				from:    model.TimeFromUnix(0),
				through: model.TimeFromUnix(3600),
			},
			[]Bucket{{
				from:      0,
				through:   3600 * 1000, // ms
				tableName: "table",
				hashKey:   "0:0",
			}},
		},
		{
			"window spanning 3 hours with non-zero start",
			args{
				from:    model.TimeFromUnix(900),
				through: model.TimeFromUnix((2 * 3600) + 1800),
			},
			[]Bucket{{
				from:      900 * 1000,  // ms
				through:   3600 * 1000, // ms
				tableName: "table",
				hashKey:   "0:0",
			}, {
				from:      0,
				through:   3600 * 1000, // ms
				tableName: "table",
				hashKey:   "0:1",
			}, {
				from:      0,
				through:   1800 * 1000, // ms
				tableName: "table",
				hashKey:   "0:2",
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cfg.hourlyBuckets(tt.args.from, tt.args.through, userID); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SchemaConfig.dailyBuckets() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDailyBuckets(t *testing.T) {
	const (
		userID     = "0"
		metricName = model.LabelValue("name")
		tableName  = "table"
	)
	var cfg = PeriodConfig{
		IndexTables: PeriodicTableConfig{Prefix: tableName},
	}

	type args struct {
		from    model.Time
		through model.Time
	}
	tests := []struct {
		name string
		args args
		want []Bucket
	}{
		{
			"0 day window",
			args{
				from:    model.TimeFromUnix(0),
				through: model.TimeFromUnix(0),
			},
			[]Bucket{},
		},
		{
			"6 hour window",
			args{
				from:    model.TimeFromUnix(0),
				through: model.TimeFromUnix(6 * 3600),
			},
			[]Bucket{{
				from:      0,
				through:   (6 * 3600) * 1000, // ms
				tableName: "table",
				hashKey:   "0:d0",
			}},
		},
		{
			"1 day window",
			args{
				from:    model.TimeFromUnix(0),
				through: model.TimeFromUnix(24 * 3600),
			},
			[]Bucket{{
				from:      0,
				through:   (24 * 3600) * 1000, // ms
				tableName: "table",
				hashKey:   "0:d0",
			}},
		},
		{
			"window spanning 3 days with non-zero start",
			args{
				from:    model.TimeFromUnix(6 * 3600),
				through: model.TimeFromUnix((2 * 24 * 3600) + (12 * 3600)),
			},
			[]Bucket{{
				from:      (6 * 3600) * 1000,  // ms
				through:   (24 * 3600) * 1000, // ms
				tableName: "table",
				hashKey:   "0:d0",
			}, {
				from:      0,
				through:   (24 * 3600) * 1000, // ms
				tableName: "table",
				hashKey:   "0:d1",
			}, {
				from:      0,
				through:   (12 * 3600) * 1000, // ms
				tableName: "table",
				hashKey:   "0:d2",
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cfg.dailyBuckets(tt.args.from, tt.args.through, userID); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SchemaConfig.dailyBuckets() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestChunkTableFor(t *testing.T) {
	tablePeriod, err := time.ParseDuration("168h")
	require.NoError(t, err)

	periodConfigs := []PeriodConfig{
		{
			From: MustParseDayTime("1970-01-01"),
			IndexTables: PeriodicTableConfig{
				Prefix: "index_1_",
				Period: tablePeriod,
			},
			ChunkTables: PeriodicTableConfig{
				Prefix: "chunks_1_",
				Period: tablePeriod,
			},
		},
		{
			From: MustParseDayTime("2019-01-02"),
			IndexTables: PeriodicTableConfig{
				Prefix: "index_2_",
				Period: tablePeriod,
			},
			ChunkTables: PeriodicTableConfig{
				Prefix: "chunks_2_",
				Period: tablePeriod,
			},
		},
		{
			From: MustParseDayTime("2019-03-06"),
			IndexTables: PeriodicTableConfig{
				Prefix: "index_3_",
				Period: tablePeriod,
			},
			ChunkTables: PeriodicTableConfig{
				Prefix: "chunks_3_",
				Period: tablePeriod,
			},
		},
	}

	schemaCfg := SchemaConfig{
		Configs: periodConfigs,
	}

	testCases := []struct {
		timeStr    string // RFC3339
		chunkTable string
	}{
		{
			timeStr:    "1970-01-01T00:00:00Z",
			chunkTable: "chunks_1_0",
		},
		{
			timeStr:    "1970-01-01T00:00:01Z",
			chunkTable: "chunks_1_0",
		},
		{
			timeStr:    "2019-01-01T00:00:00Z",
			chunkTable: "chunks_1_2556",
		},
		{
			timeStr:    "2019-01-01T23:59:59Z",
			chunkTable: "chunks_1_2556",
		},
		{
			timeStr:    "2019-01-02T00:00:00Z",
			chunkTable: "chunks_2_2556",
		},
		{
			timeStr:    "2019-03-06T00:00:00Z",
			chunkTable: "chunks_3_2565",
		},
		{
			timeStr:    "2020-03-06T00:00:00Z",
			chunkTable: "chunks_3_2618",
		},
	}

	for _, tc := range testCases {
		ts, err := time.Parse(time.RFC3339, tc.timeStr)
		require.NoError(t, err)

		table, err := schemaCfg.ChunkTableFor(model.TimeFromUnix(ts.Unix()))
		require.NoError(t, err)

		require.Equal(t, tc.chunkTable, table)
	}
}

func MustParseDayTime(s string) DayTime {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return DayTime{model.TimeFromUnix(t.Unix())}
}
