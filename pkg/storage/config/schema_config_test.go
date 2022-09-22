package config

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yaml "gopkg.in/yaml.v2"
)

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
			err: "must have row_shards > 0 (current: 0) for schema (v10)",
		},
		{
			desc: "v12",
			in: PeriodConfig{
				Schema:      "v12",
				RowShards:   16,
				IndexTables: PeriodicTableConfig{Period: 0},
				ChunkTables: PeriodicTableConfig{Period: 0},
			},
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

func TestSchemaForTime(t *testing.T) {
	schemaCfg := SchemaConfig{Configs: []PeriodConfig{
		{
			From:       DayTime{Time: 1564358400000},
			IndexType:  "grpc-store",
			ObjectType: "grpc-store",
			Schema:     "v10",
			IndexTables: PeriodicTableConfig{
				Prefix: "index_",
				Period: 604800000000000,
				Tags:   nil,
			},
			RowShards: 16,
		},
		{
			From:       DayTime{Time: 1564444800000},
			IndexType:  "grpc-store",
			ObjectType: "grpc-store",
			Schema:     "v10",
			IndexTables: PeriodicTableConfig{
				Prefix: "index_",
				Period: 604800000000000,
				Tags:   nil,
			},
			RowShards: 32,
		},
	}}

	first, err := schemaCfg.SchemaForTime(model.TimeFromUnix(1564444800 + 100))
	require.NoError(t, err)
	require.Equal(t, schemaCfg.Configs[1], first)

	second, err := schemaCfg.SchemaForTime(model.TimeFromUnix(1564358400 + 100))
	require.NoError(t, err)
	require.Equal(t, schemaCfg.Configs[0], second)
}

func TestVersionAsInt(t *testing.T) {
	for _, tc := range []struct {
		name      string
		schemaCfg SchemaConfig
		expected  int
		err       bool
	}{
		{
			name: "v9",
			schemaCfg: SchemaConfig{
				Configs: []PeriodConfig{
					{
						From:   DayTime{Time: 0},
						Schema: "v9",
					},
				},
			},
			expected: int(9),
		},
		{
			name: "malformed",
			schemaCfg: SchemaConfig{
				Configs: []PeriodConfig{
					{
						From:   DayTime{Time: 0},
						Schema: "v",
					},
				},
			},
			expected: int(0),
			err:      true,
		},
		{
			name: "v12",
			schemaCfg: SchemaConfig{
				Configs: []PeriodConfig{
					{
						From:      DayTime{Time: 0},
						Schema:    "v12",
						RowShards: 16,
					},
				},
			},
			expected: int(12),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			version, err := tc.schemaCfg.Configs[0].VersionAsInt()
			require.Equal(t, tc.expected, version)
			if tc.err {
				require.NotNil(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestUnmarshalPeriodConfig(t *testing.T) {
	input := `
from: "2020-07-31"
index:
  period: 24h
  prefix: loki_index_
object_store: gcs
schema: v11
store: boltdb-shipper
`

	var cfg PeriodConfig
	require.Nil(t, yaml.Unmarshal([]byte(input), &cfg))
	n := 11

	expected := PeriodConfig{
		From:       DayTime{model.Time(1596153600000)},
		IndexType:  "boltdb-shipper",
		ObjectType: "gcs",
		Schema:     "v11",
		IndexTables: PeriodicTableConfig{
			Prefix: "loki_index_",
			Period: 24 * time.Hour,
		},
		schemaInt: &n,
	}

	require.Equal(t, expected, cfg)
}

func TestUsingObjectStorageIndex(t *testing.T) {
	var cfg SchemaConfig

	// just one PeriodConfig in the past using boltdb-shipper
	cfg.Configs = []PeriodConfig{{
		From:      DayTime{Time: model.Now().Add(-24 * time.Hour)},
		IndexType: "boltdb-shipper",
	}}
	assert.Equal(t, true, UsingObjectStorageIndex(cfg.Configs))

	// just one PeriodConfig in the past not using object storge index
	cfg.Configs[0].IndexType = "boltdb"
	assert.Equal(t, false, UsingObjectStorageIndex(cfg.Configs))

	// add a newer PeriodConfig in the future using tsdb
	cfg.Configs = append(cfg.Configs, PeriodConfig{
		From:      DayTime{Time: model.Now().Add(time.Hour)},
		IndexType: "tsdb",
	})
	assert.Equal(t, true, UsingObjectStorageIndex(cfg.Configs))
}

func TestActiveIndexType(t *testing.T) {
	var cfg SchemaConfig

	// just one PeriodConfig in the past
	cfg.Configs = []PeriodConfig{{
		From:      DayTime{Time: model.Now().Add(-24 * time.Hour)},
		IndexType: "first",
	}}

	assert.Equal(t, 0, ActivePeriodConfig(cfg.Configs))

	// add a newer PeriodConfig in the past which should be considered
	cfg.Configs = append(cfg.Configs, PeriodConfig{
		From:      DayTime{Time: model.Now().Add(-12 * time.Hour)},
		IndexType: "second",
	})
	assert.Equal(t, 1, ActivePeriodConfig(cfg.Configs))

	// add a newer PeriodConfig in the future which should not be considered
	cfg.Configs = append(cfg.Configs, PeriodConfig{
		From:      DayTime{Time: model.Now().Add(time.Hour)},
		IndexType: "third",
	})
	assert.Equal(t, 1, ActivePeriodConfig(cfg.Configs))
}

func TestSchemaConfig_ValidateBoltdb(t *testing.T) {
	for _, tc := range []struct {
		name    string
		configs []PeriodConfig
		err     error
	}{
		{
			name:    "empty",
			configs: []PeriodConfig{},
			err:     errZeroLengthConfig,
		},
		{
			name: "NOT using boltdb-shipper",
			configs: []PeriodConfig{{
				From:      DayTime{Time: model.Now().Add(-24 * time.Hour)},
				IndexType: "boltdb",
				Schema:    "v9",
				IndexTables: PeriodicTableConfig{
					Period: 7 * 24 * time.Hour,
				},
			}},
		},
		{
			name: "current config boltdb-shipper with 7 days periodic config, without future index type changes",
			configs: []PeriodConfig{{
				From:      DayTime{Time: model.Now().Add(-24 * time.Hour)},
				IndexType: "boltdb-shipper",
				Schema:    "v9",
				IndexTables: PeriodicTableConfig{
					Period: 7 * 24 * time.Hour,
				},
			}},
			err: errCurrentBoltdbShipperNon24Hours,
		},
		{
			name: "current config boltdb-shipper with 1 day periodic config, without future index type changes",
			configs: []PeriodConfig{{
				From:      DayTime{Time: model.Now().Add(-24 * time.Hour)},
				IndexType: "boltdb-shipper",
				Schema:    "v9",
				IndexTables: PeriodicTableConfig{
					Period: 24 * time.Hour,
				},
			}},
		},
		{
			name: "current config boltdb-shipper with 7 days periodic config, upcoming config NOT boltdb-shipper",
			configs: []PeriodConfig{{
				From:      DayTime{Time: model.Now().Add(-24 * time.Hour)},
				IndexType: "boltdb-shipper",
				Schema:    "v9",
				IndexTables: PeriodicTableConfig{
					Period: 24 * time.Hour,
				},
			}, {
				From:      DayTime{Time: model.Now().Add(time.Hour)},
				IndexType: "boltdb",
				Schema:    "v9",
				IndexTables: PeriodicTableConfig{
					Period: 7 * 24 * time.Hour,
				},
			}},
		},
		{
			name: "current and upcoming config boltdb-shipper with 7 days periodic config",
			configs: []PeriodConfig{{
				From:      DayTime{Time: model.Now().Add(-24 * time.Hour)},
				IndexType: "boltdb-shipper",
				Schema:    "v9",
				IndexTables: PeriodicTableConfig{
					Period: 24 * time.Hour,
				},
			}, {
				From:      DayTime{Time: model.Now().Add(time.Hour)},
				IndexType: "boltdb-shipper",
				Schema:    "v9",
				IndexTables: PeriodicTableConfig{
					Period: 7 * 24 * time.Hour,
				},
			}},
			err: errUpcomingBoltdbShipperNon24Hours,
		},
		{
			name: "current config NOT boltdb-shipper, upcoming config boltdb-shipper with 7 days periodic config",
			configs: []PeriodConfig{{
				From:      DayTime{Time: model.Now().Add(-24 * time.Hour)},
				IndexType: "boltdb",
				Schema:    "v9",
				IndexTables: PeriodicTableConfig{
					Period: 24 * time.Hour,
				},
			}, {
				From:      DayTime{Time: model.Now().Add(time.Hour)},
				IndexType: "boltdb-shipper",
				Schema:    "v9",
				IndexTables: PeriodicTableConfig{
					Period: 7 * 24 * time.Hour,
				},
			}},
			err: errUpcomingBoltdbShipperNon24Hours,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := SchemaConfig{Configs: tc.configs}
			err := cfg.Validate()
			if tc.err == nil {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tc.err.Error())
			}
		})
	}
}

func TestTableRanges_TableInRange(t *testing.T) {
	tableRanges := TableRanges{
		TableRange{
			Start: 1,
			End:   10,
			PeriodConfig: &PeriodConfig{IndexTables: PeriodicTableConfig{
				Prefix: "index_",
			}},
		},
		TableRange{
			Start: 11,
			End:   20,
			PeriodConfig: &PeriodConfig{IndexTables: PeriodicTableConfig{
				Prefix: "index_foo_",
			}},
		},
	}

	for i, tc := range []struct {
		tableNumber int64
		tableName   string
		expResp     bool
	}{
		{
			tableNumber: 1,
			tableName:   "index_1",
			expResp:     true,
		},
		{
			tableNumber: 15,
			tableName:   "index_foo_15",
			expResp:     true,
		},
		{
			tableNumber: 25,
			tableName:   "index_15",
		},
		{
			tableNumber: 15,
			tableName:   "index_15",
		},
	} {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			require.Equal(t, tc.expResp, tableRanges.TableInRange(tc.tableNumber, tc.tableName))
		})
	}
}
