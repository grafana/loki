package chunk

import (
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v2"
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
			[]Bucket{{
				from:       0,
				through:    0,
				tableName:  "table",
				hashKey:    "0:0",
				bucketSize: uint32(millisecondsInHour),
			}},
		},
		{
			"30 minute window",
			args{
				from:    model.TimeFromUnix(0),
				through: model.TimeFromUnix(1800),
			},
			[]Bucket{{
				from:       0,
				through:    1800 * 1000, // ms
				tableName:  "table",
				hashKey:    "0:0",
				bucketSize: uint32(millisecondsInHour),
			}},
		},
		{
			"1 hour window",
			args{
				from:    model.TimeFromUnix(0),
				through: model.TimeFromUnix(3600),
			},
			[]Bucket{{
				from:       0,
				through:    3600 * 1000, // ms
				tableName:  "table",
				hashKey:    "0:0",
				bucketSize: uint32(millisecondsInHour),
			}, {
				from:       0,
				through:    0, // ms
				tableName:  "table",
				hashKey:    "0:1",
				bucketSize: uint32(millisecondsInHour),
			}},
		},
		{
			"window spanning 3 hours with non-zero start",
			args{
				from:    model.TimeFromUnix(900),
				through: model.TimeFromUnix((2 * 3600) + 1800),
			},
			[]Bucket{{
				from:       900 * 1000,  // ms
				through:    3600 * 1000, // ms
				tableName:  "table",
				hashKey:    "0:0",
				bucketSize: uint32(millisecondsInHour),
			}, {
				from:       0,
				through:    3600 * 1000, // ms
				tableName:  "table",
				hashKey:    "0:1",
				bucketSize: uint32(millisecondsInHour),
			}, {
				from:       0,
				through:    1800 * 1000, // ms
				tableName:  "table",
				hashKey:    "0:2",
				bucketSize: uint32(millisecondsInHour),
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cfg.hourlyBuckets(tt.args.from, tt.args.through, userID)
			assert.Equal(t, tt.want, got)
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
			[]Bucket{{
				from:       0,
				through:    0,
				tableName:  "table",
				hashKey:    "0:d0",
				bucketSize: uint32(millisecondsInDay),
			}},
		},
		{
			"6 hour window",
			args{
				from:    model.TimeFromUnix(0),
				through: model.TimeFromUnix(6 * 3600),
			},
			[]Bucket{{
				from:       0,
				through:    (6 * 3600) * 1000, // ms
				tableName:  "table",
				hashKey:    "0:d0",
				bucketSize: uint32(millisecondsInDay),
			}},
		},
		{
			"1 day window",
			args{
				from:    model.TimeFromUnix(0),
				through: model.TimeFromUnix(24 * 3600),
			},
			[]Bucket{{
				from:       0,
				through:    (24 * 3600) * 1000, // ms
				tableName:  "table",
				hashKey:    "0:d0",
				bucketSize: uint32(millisecondsInDay),
			}, {
				from:       0,
				through:    0,
				tableName:  "table",
				hashKey:    "0:d1",
				bucketSize: uint32(millisecondsInDay),
			}},
		},
		{
			"window spanning 3 days with non-zero start",
			args{
				from:    model.TimeFromUnix(6 * 3600),
				through: model.TimeFromUnix((2 * 24 * 3600) + (12 * 3600)),
			},
			[]Bucket{{
				from:       (6 * 3600) * 1000,  // ms
				through:    (24 * 3600) * 1000, // ms
				tableName:  "table",
				hashKey:    "0:d0",
				bucketSize: uint32(millisecondsInDay),
			}, {
				from:       0,
				through:    (24 * 3600) * 1000, // ms
				tableName:  "table",
				hashKey:    "0:d1",
				bucketSize: uint32(millisecondsInDay),
			}, {
				from:       0,
				through:    (12 * 3600) * 1000, // ms
				tableName:  "table",
				hashKey:    "0:d2",
				bucketSize: uint32(millisecondsInDay),
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cfg.dailyBuckets(tt.args.from, tt.args.through, userID)
			assert.Equal(t, tt.want, got)
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

func TestSchemaConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		config   *SchemaConfig
		expected *SchemaConfig
		err      error
	}{
		"should pass the default config (ie. used cortex runs with a target not requiring the schema config)": {
			config: &SchemaConfig{},
			err:    nil,
		},
		"should fail on invalid schema version": {
			config: &SchemaConfig{
				Configs: []PeriodConfig{
					{Schema: "v0"},
				},
			},
			err: errInvalidSchemaVersion,
		},
		"should fail on index table period not multiple of 1h for schema v1": {
			config: &SchemaConfig{
				Configs: []PeriodConfig{
					{
						Schema:      "v1",
						IndexTables: PeriodicTableConfig{Period: 30 * time.Minute},
					},
				},
			},
			err: errInvalidTablePeriod,
		},
		"should fail on chunk table period not multiple of 1h for schema v1": {
			config: &SchemaConfig{
				Configs: []PeriodConfig{
					{
						Schema:      "v1",
						IndexTables: PeriodicTableConfig{Period: 6 * time.Hour},
						ChunkTables: PeriodicTableConfig{Period: 30 * time.Minute},
					},
				},
			},
			err: errInvalidTablePeriod,
		},
		"should pass on index and chunk table period multiple of 1h for schema v1": {
			config: &SchemaConfig{
				Configs: []PeriodConfig{
					{
						Schema:      "v1",
						IndexTables: PeriodicTableConfig{Period: 6 * time.Hour},
						ChunkTables: PeriodicTableConfig{Period: 6 * time.Hour},
					},
				},
			},
			err: nil,
		},
		"should fail on index table period not multiple of 24h for schema v10": {
			config: &SchemaConfig{
				Configs: []PeriodConfig{
					{
						Schema:      "v10",
						IndexTables: PeriodicTableConfig{Period: 6 * time.Hour},
					},
				},
			},
			err: errInvalidTablePeriod,
		},
		"should fail on chunk table period not multiple of 24h for schema v10": {
			config: &SchemaConfig{
				Configs: []PeriodConfig{
					{
						Schema:      "v10",
						IndexTables: PeriodicTableConfig{Period: 24 * time.Hour},
						ChunkTables: PeriodicTableConfig{Period: 6 * time.Hour},
					},
				},
			},
			err: errInvalidTablePeriod,
		},
		"should pass on index and chunk table period multiple of 24h for schema v10": {
			config: &SchemaConfig{
				Configs: []PeriodConfig{
					{
						Schema:      "v10",
						IndexTables: PeriodicTableConfig{Period: 24 * time.Hour},
						ChunkTables: PeriodicTableConfig{Period: 24 * time.Hour},
					},
				},
			},
			expected: &SchemaConfig{
				Configs: []PeriodConfig{
					{
						Schema:      "v10",
						RowShards:   16,
						IndexTables: PeriodicTableConfig{Period: 24 * time.Hour},
						ChunkTables: PeriodicTableConfig{Period: 24 * time.Hour},
					},
				},
			},
			err: nil,
		},
		"should pass on index and chunk table period set to zero (no period tables)": {
			config: &SchemaConfig{
				Configs: []PeriodConfig{
					{
						Schema:      "v10",
						IndexTables: PeriodicTableConfig{Period: 0},
						ChunkTables: PeriodicTableConfig{Period: 0},
					},
				},
			},
			expected: &SchemaConfig{
				Configs: []PeriodConfig{
					{
						Schema:      "v10",
						RowShards:   16,
						IndexTables: PeriodicTableConfig{Period: 0},
						ChunkTables: PeriodicTableConfig{Period: 0},
					},
				},
			},
			err: nil,
		},
		"should set shard factor defaults": {
			config: &SchemaConfig{
				Configs: []PeriodConfig{
					{
						Schema: "v10",
					},
				},
			},
			expected: &SchemaConfig{
				Configs: []PeriodConfig{
					{
						Schema:    "v10",
						RowShards: 16,
					},
				},
			},
			err: nil,
		},
		"should not override explicit shard factor": {
			config: &SchemaConfig{
				Configs: []PeriodConfig{
					{
						Schema:    "v11",
						RowShards: 6,
					},
				},
			},
			expected: &SchemaConfig{
				Configs: []PeriodConfig{
					{
						Schema:    "v11",
						RowShards: 6,
					},
				},
			},
			err: nil,
		},
		"should fail if chunks prefix is missing on IndexType: aws-dynamo": {
			config: &SchemaConfig{
				Configs: []PeriodConfig{
					{
						Schema:      "v10",
						IndexType:   "aws-dynamo",
						ObjectType:  "aws-dynamo",
						IndexTables: PeriodicTableConfig{Period: 24 * time.Hour},
					},
				},
			},
			err: errConfigChunkPrefixNotSet,
		},
		"should fail if chunks prefix is missing on IndexType: cassandra": {
			config: &SchemaConfig{
				Configs: []PeriodConfig{
					{
						Schema:      "v10",
						IndexType:   "cassandra",
						ObjectType:  "cassandra",
						IndexTables: PeriodicTableConfig{Period: 24 * time.Hour},
					},
				},
			},
			err: errConfigChunkPrefixNotSet,
		},
		"should fail if chunks prefix is missing on IndexType: bigtable-hashed": {
			config: &SchemaConfig{
				Configs: []PeriodConfig{
					{
						Schema:      "v10",
						IndexType:   "bigtable-hashed",
						ObjectType:  "bigtable-hashed",
						IndexTables: PeriodicTableConfig{Period: 24 * time.Hour},
					},
				},
			},
			err: errConfigChunkPrefixNotSet,
		},
		"should fail if chunks prefix is missing on IndexType: gcp": {
			config: &SchemaConfig{
				Configs: []PeriodConfig{
					{
						Schema:      "v10",
						IndexType:   "gcp",
						ObjectType:  "gcp",
						IndexTables: PeriodicTableConfig{Period: 24 * time.Hour},
					},
				},
			},
			err: errConfigChunkPrefixNotSet,
		},
		"should fail if chunks prefix is missing on IndexType: gcp-columnkey": {
			config: &SchemaConfig{
				Configs: []PeriodConfig{
					{
						Schema:      "v10",
						IndexType:   "gcp-columnkey",
						ObjectType:  "gcp-columnkey",
						IndexTables: PeriodicTableConfig{Period: 24 * time.Hour},
					},
				},
			},
			err: errConfigChunkPrefixNotSet,
		},
		"should fail if chunks prefix is missing on IndexType: bigtable": {
			config: &SchemaConfig{
				Configs: []PeriodConfig{
					{
						Schema:      "v10",
						IndexType:   "bigtable",
						ObjectType:  "bigtable",
						IndexTables: PeriodicTableConfig{Period: 24 * time.Hour},
					},
				},
			},
			err: errConfigChunkPrefixNotSet,
		},
		"should fail if chunks prefix is missing on IndexType: grpc-store": {
			config: &SchemaConfig{
				Configs: []PeriodConfig{
					{
						Schema:      "v10",
						IndexType:   "grpc-store",
						ObjectType:  "grpc-store",
						IndexTables: PeriodicTableConfig{Period: 24 * time.Hour},
					},
				},
			},
			err: errConfigChunkPrefixNotSet,
		},
		"invalid schema with same from time configs": {
			config: &SchemaConfig{
				Configs: []PeriodConfig{
					{
						From:   MustParseDayTime("1970-01-01"),
						Schema: "v9",
					},
					{
						From:   MustParseDayTime("1970-01-01"),
						Schema: "v10",
					},
				},
			},
			err: errSchemaIncreasingFromTime,
		},
		"invalid schema with from time not in increasing order": {
			config: &SchemaConfig{
				Configs: []PeriodConfig{
					{
						From:   MustParseDayTime("1970-01-02"),
						Schema: "v9",
					},
					{
						From:   MustParseDayTime("1970-01-01"),
						Schema: "v10",
					},
				},
			},
			err: errSchemaIncreasingFromTime,
		},
		"valid schema with different from time configs": {
			config: &SchemaConfig{
				Configs: []PeriodConfig{
					{
						From:   MustParseDayTime("1970-01-01"),
						Schema: "v9",
					},
					{
						From:   MustParseDayTime("1970-01-02"),
						Schema: "v10",
					},
				},
			},
		},
	}

	for testName, testData := range tests {
		testData := testData

		t.Run(testName, func(t *testing.T) {
			actual := testData.config.Validate()
			assert.Equal(t, testData.err, actual)
			if testData.expected != nil {
				require.Equal(t, testData.expected, testData.config)
			}
		})
	}
}

func TestPeriodConfig_Validate(t *testing.T) {
	for _, tc := range []struct {
		desc string
		in   PeriodConfig
		err  string
	}{
		{
			desc: "ignore pre v10 sharding",
			in: PeriodConfig{

				Schema:      "v9",
				IndexTables: PeriodicTableConfig{Period: 0},
				ChunkTables: PeriodicTableConfig{Period: 0},
			},
		},
		{
			desc: "error on invalid schema",
			in: PeriodConfig{

				Schema:      "v99",
				IndexTables: PeriodicTableConfig{Period: 0},
				ChunkTables: PeriodicTableConfig{Period: 0},
			},
			err: "invalid schema version",
		},
		{
			desc: "v10 with shard factor",
			in: PeriodConfig{

				Schema:      "v10",
				RowShards:   16,
				IndexTables: PeriodicTableConfig{Period: 0},
				ChunkTables: PeriodicTableConfig{Period: 0},
			},
		},
		{
			desc: "v11 with shard factor",
			in: PeriodConfig{

				Schema:      "v11",
				RowShards:   16,
				IndexTables: PeriodicTableConfig{Period: 0},
				ChunkTables: PeriodicTableConfig{Period: 0},
			},
		},
		{
			desc: "error v10 no specified shard factor",
			in: PeriodConfig{

				Schema:      "v10",
				IndexTables: PeriodicTableConfig{Period: 0},
				ChunkTables: PeriodicTableConfig{Period: 0},
			},
			err: "Must have row_shards > 0 (current: 0) for schema (v10)",
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			if tc.err == "" {
				require.Nil(t, tc.in.validate())
			} else {
				require.Error(t, tc.in.validate(), tc.err)
			}
		})
	}
}

func MustParseDayTime(s string) DayTime {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return DayTime{model.TimeFromUnix(t.Unix())}
}

func TestPeriodicTableConfigCustomUnmarshalling(t *testing.T) {
	yamlFile := `prefix: cortex_
period: 1w
tags:
  foo: bar
`

	cfg := PeriodicTableConfig{}
	err := yaml.Unmarshal([]byte(yamlFile), &cfg)
	require.NoError(t, err)

	expectedCfg := PeriodicTableConfig{
		Prefix: "cortex_",
		Period: 7 * 24 * time.Hour,
		Tags: map[string]string{
			"foo": "bar",
		},
	}

	require.Equal(t, expectedCfg, cfg)

	yamlGenerated, err := yaml.Marshal(&cfg)
	require.NoError(t, err)

	require.Equal(t, yamlFile, string(yamlGenerated))
}
