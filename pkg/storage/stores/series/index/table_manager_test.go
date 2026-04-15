package index

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/mtime"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/config"
)

const (
	baseTableName     = "cortex_base"
	tablePrefix       = "cortex_"
	table2Prefix      = "cortex2_"
	chunkTablePrefix  = "chunks_"
	chunkTable2Prefix = "chunks2_"
	tableRetention    = 2 * 7 * 24 * time.Hour
	tablePeriod       = 7 * 24 * time.Hour
	gracePeriod       = 15 * time.Minute
	maxChunkAge       = 12 * time.Hour
	inactiveWrite     = 1
	inactiveRead      = 2
	write             = 200
	read              = 100
	autoScaleLastN    = 2
	autoScaleMin      = 50
	autoScaleMax      = 500
	autoScaleTarget   = 80
)

var (
	baseTableStart    = time.Unix(0, 0)
	weeklyTableStart  = baseTableStart.Add(tablePeriod * 3)
	weeklyTable2Start = baseTableStart.Add(tablePeriod * 5)
	week1Suffix       = "3"
	week2Suffix       = "4"
)

type mockTableClient struct {
	sync.Mutex
	tables map[string]config.TableDesc
}

func newMockTableClient() *mockTableClient {
	return &mockTableClient{
		tables: map[string]config.TableDesc{},
	}
}

func (m *mockTableClient) ListTables(_ context.Context) ([]string, error) {
	m.Lock()
	defer m.Unlock()

	result := []string{}
	for name := range m.tables {
		result = append(result, name)
	}
	return result, nil
}

func (m *mockTableClient) CreateTable(_ context.Context, desc config.TableDesc) error {
	m.Lock()
	defer m.Unlock()

	m.tables[desc.Name] = desc
	return nil
}

func (m *mockTableClient) DeleteTable(_ context.Context, name string) error {
	m.Lock()
	defer m.Unlock()

	delete(m.tables, name)
	return nil
}

func (m *mockTableClient) DescribeTable(_ context.Context, name string) (desc config.TableDesc, isActive bool, err error) {
	m.Lock()
	defer m.Unlock()

	return m.tables[name], true, nil
}

func (m *mockTableClient) UpdateTable(_ context.Context, current, expected config.TableDesc) error {
	m.Lock()
	defer m.Unlock()

	m.tables[current.Name] = expected
	return nil
}

func (*mockTableClient) Stop() {}

// nolint
func tmTest(t *testing.T, client *mockTableClient, tableManager *TableManager, name string, tm time.Time, expected []config.TableDesc) {
	t.Run(name, func(t *testing.T) {
		ctx := context.Background()
		mtime.NowForce(tm)
		defer mtime.NowReset()
		if err := tableManager.SyncTables(ctx); err != nil {
			t.Fatal(err)
		}
		err := ExpectTables(ctx, client, expected)
		require.NoError(t, err)
	})
}

var activeScalingConfig = config.AutoScalingConfig{
	Enabled:     true,
	MinCapacity: autoScaleMin * 2,
	MaxCapacity: autoScaleMax * 2,
	TargetValue: autoScaleTarget,
}

var inactiveScalingConfig = config.AutoScalingConfig{
	Enabled:     true,
	MinCapacity: autoScaleMin,
	MaxCapacity: autoScaleMax,
	TargetValue: autoScaleTarget,
}

func TestTableManager(t *testing.T) {
	client := newMockTableClient()

	cfg := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From: config.DayTime{Time: model.TimeFromUnix(baseTableStart.Unix())},
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: baseTableName,
					}},
			},
			{
				From: config.DayTime{Time: model.TimeFromUnix(weeklyTableStart.Unix())},
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: tablePrefix,
						Period: tablePeriod,
					}},

				ChunkTables: config.PeriodicTableConfig{
					Prefix: chunkTablePrefix,
					Period: tablePeriod,
				},
			},
			{
				From: config.DayTime{Time: model.TimeFromUnix(weeklyTable2Start.Unix())},
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: table2Prefix,
						Period: tablePeriod,
					}},

				ChunkTables: config.PeriodicTableConfig{
					Prefix: chunkTable2Prefix,
					Period: tablePeriod,
				},
			},
		},
	}
	tbmConfig := TableManagerConfig{
		CreationGracePeriod: gracePeriod,
		IndexTables: config.ProvisionConfig{
			ActiveTableProvisionConfig: config.ActiveTableProvisionConfig{
				ProvisionedWriteThroughput: write,
				ProvisionedReadThroughput:  read,
				WriteScale:                 activeScalingConfig,
			},
			InactiveTableProvisionConfig: config.InactiveTableProvisionConfig{
				InactiveWriteThroughput: inactiveWrite,
				InactiveReadThroughput:  inactiveRead,
				InactiveWriteScale:      inactiveScalingConfig,
				InactiveWriteScaleLastN: autoScaleLastN,
			},
		},
		ChunkTables: config.ProvisionConfig{
			ActiveTableProvisionConfig: config.ActiveTableProvisionConfig{
				ProvisionedWriteThroughput: write,
				ProvisionedReadThroughput:  read,
			},
			InactiveTableProvisionConfig: config.InactiveTableProvisionConfig{
				InactiveWriteThroughput: inactiveWrite,
				InactiveReadThroughput:  inactiveRead,
			},
		},
	}
	tableManager, err := NewTableManager(tbmConfig, cfg, maxChunkAge, client, nil, nil, nil, log.NewNopLogger())
	if err != nil {
		t.Fatal(err)
	}

	// Check at time zero, we have the base table only
	tmTest(t, client, tableManager,
		"Initial test",
		baseTableStart,
		[]config.TableDesc{
			{Name: baseTableName, ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
		},
	)

	// Check at start of weekly tables, we have the base table and one weekly table
	tmTest(t, client, tableManager,
		"Initial test weekly",
		weeklyTableStart,
		[]config.TableDesc{
			{Name: baseTableName, ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: tablePrefix + week1Suffix, ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: chunkTablePrefix + week1Suffix, ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Check running twice doesn't change anything
	tmTest(t, client, tableManager,
		"Nothing changed",
		weeklyTableStart,
		[]config.TableDesc{
			{Name: baseTableName, ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: tablePrefix + week1Suffix, ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: chunkTablePrefix + week1Suffix, ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Fast forward grace period, check we still have write throughput on base table
	tmTest(t, client, tableManager,
		"Move forward by grace period",
		weeklyTableStart.Add(gracePeriod),
		[]config.TableDesc{
			{Name: baseTableName, ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: tablePrefix + week1Suffix, ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: chunkTablePrefix + week1Suffix, ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Fast forward max chunk age + grace period, check write throughput on base table has gone
	// (and we don't put inactive auto-scaling on base table)
	tmTest(t, client, tableManager,
		"Move forward by max chunk age + grace period",
		weeklyTableStart.Add(maxChunkAge).Add(gracePeriod),
		[]config.TableDesc{
			{Name: baseTableName, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + week1Suffix, ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: chunkTablePrefix + week1Suffix, ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Fast forward table period - grace period, check we add another weekly table
	tmTest(t, client, tableManager,
		"Move forward by table period - grace period",
		weeklyTableStart.Add(tablePeriod).Add(-gracePeriod+time.Second),
		[]config.TableDesc{
			{Name: baseTableName, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + week1Suffix, ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: tablePrefix + week2Suffix, ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: chunkTablePrefix + week1Suffix, ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + week2Suffix, ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Fast forward table period + grace period, check we still have provisioned throughput
	tmTest(t, client, tableManager,
		"Move forward by table period + grace period",
		weeklyTableStart.Add(tablePeriod).Add(gracePeriod),
		[]config.TableDesc{
			{Name: baseTableName, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + week1Suffix, ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: tablePrefix + week2Suffix, ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: chunkTablePrefix + week1Suffix, ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + week2Suffix, ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Fast forward table period + max chunk age + grace period, check we remove provisioned throughput
	tmTest(t, client, tableManager,
		"Move forward by table period + max chunk age + grace period",
		weeklyTableStart.Add(tablePeriod).Add(maxChunkAge).Add(gracePeriod),
		[]config.TableDesc{
			{Name: baseTableName, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + week1Suffix, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite, WriteScale: inactiveScalingConfig},
			{Name: tablePrefix + week2Suffix, ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: chunkTablePrefix + week1Suffix, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: chunkTablePrefix + week2Suffix, ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Check running twice doesn't change anything
	tmTest(t, client, tableManager,
		"Nothing changed",
		weeklyTableStart.Add(tablePeriod).Add(maxChunkAge).Add(gracePeriod),
		[]config.TableDesc{
			{Name: baseTableName, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + week1Suffix, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite, WriteScale: inactiveScalingConfig},
			{Name: tablePrefix + week2Suffix, ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: chunkTablePrefix + week1Suffix, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: chunkTablePrefix + week2Suffix, ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Move ahead where we are short by just grace period before hitting next section
	tmTest(t, client, tableManager,
		"Move ahead where we are short by just grace period before hitting next section",
		weeklyTable2Start.Add(-gracePeriod+time.Second),
		[]config.TableDesc{
			{Name: baseTableName, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + week1Suffix, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite, WriteScale: inactiveScalingConfig},
			{Name: tablePrefix + week2Suffix, ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: table2Prefix + "5", ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: chunkTablePrefix + week1Suffix, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: chunkTablePrefix + week2Suffix, ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTable2Prefix + "5", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Move to the next section of the config
	tmTest(t, client, tableManager,
		"Move forward to next section of schema config",
		weeklyTable2Start,
		[]config.TableDesc{
			{Name: baseTableName, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + week1Suffix, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite, WriteScale: inactiveScalingConfig},
			{Name: tablePrefix + week2Suffix, ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: table2Prefix + "5", ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: chunkTablePrefix + week1Suffix, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: chunkTablePrefix + week2Suffix, ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTable2Prefix + "5", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)
}

func TestTableManagerAutoscaleInactiveOnly(t *testing.T) {
	client := newMockTableClient()

	cfg := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From: config.DayTime{Time: model.TimeFromUnix(baseTableStart.Unix())},
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: baseTableName,
					}},
			},
			{
				From: config.DayTime{Time: model.TimeFromUnix(weeklyTableStart.Unix())},
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: tablePrefix,
						Period: tablePeriod,
					}},

				ChunkTables: config.PeriodicTableConfig{
					Prefix: chunkTablePrefix,
					Period: tablePeriod,
				},
			},
		},
	}
	tbmConfig := TableManagerConfig{
		CreationGracePeriod: gracePeriod,
		IndexTables: config.ProvisionConfig{
			ActiveTableProvisionConfig: config.ActiveTableProvisionConfig{
				ProvisionedWriteThroughput: write,
				ProvisionedReadThroughput:  read,
			},
			InactiveTableProvisionConfig: config.InactiveTableProvisionConfig{
				InactiveWriteThroughput: inactiveWrite,
				InactiveReadThroughput:  inactiveRead,
				InactiveWriteScale:      inactiveScalingConfig,
				InactiveWriteScaleLastN: autoScaleLastN,
			},
		},
		ChunkTables: config.ProvisionConfig{
			ActiveTableProvisionConfig: config.ActiveTableProvisionConfig{
				ProvisionedWriteThroughput: write,
				ProvisionedReadThroughput:  read,
			},
			InactiveTableProvisionConfig: config.InactiveTableProvisionConfig{
				InactiveWriteThroughput: inactiveWrite,
				InactiveReadThroughput:  inactiveRead,
			},
		},
	}
	tableManager, err := NewTableManager(tbmConfig, cfg, maxChunkAge, client, nil, nil, nil, log.NewNopLogger())
	if err != nil {
		t.Fatal(err)
	}

	// Check at time zero, we have the base table and one weekly table
	tmTest(t, client, tableManager,
		"Initial test",
		weeklyTableStart,
		[]config.TableDesc{
			{Name: baseTableName, ProvisionedRead: read, ProvisionedWrite: write},
			{Name: tablePrefix + week1Suffix, ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + week1Suffix, ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Fast forward table period + grace period, check we still have provisioned throughput
	tmTest(t, client, tableManager,
		"Move forward by table period + grace period",
		weeklyTableStart.Add(tablePeriod).Add(gracePeriod),
		[]config.TableDesc{
			{Name: baseTableName, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + week1Suffix, ProvisionedRead: read, ProvisionedWrite: write},
			{Name: tablePrefix + week2Suffix, ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + week1Suffix, ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + week2Suffix, ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Fast forward table period + max chunk age + grace period, check we remove provisioned throughput

	tmTest(t, client, tableManager,
		"Move forward by table period + max chunk age + grace period",
		weeklyTableStart.Add(tablePeriod).Add(maxChunkAge).Add(gracePeriod),
		[]config.TableDesc{
			{Name: baseTableName, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + week1Suffix, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite, WriteScale: inactiveScalingConfig},
			{Name: tablePrefix + week2Suffix, ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + week1Suffix, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: chunkTablePrefix + week2Suffix, ProvisionedRead: read, ProvisionedWrite: write},
		},
	)
}

func TestTableManagerDynamicIOModeInactiveOnly(t *testing.T) {
	client := newMockTableClient()

	cfg := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From: config.DayTime{Time: model.TimeFromUnix(baseTableStart.Unix())},
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: baseTableName,
					}},
			},
			{
				From: config.DayTime{Time: model.TimeFromUnix(weeklyTableStart.Unix())},
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: tablePrefix,
						Period: tablePeriod,
					}},

				ChunkTables: config.PeriodicTableConfig{
					Prefix: chunkTablePrefix,
					Period: tablePeriod,
				},
			},
		},
	}
	tbmConfig := TableManagerConfig{
		CreationGracePeriod: gracePeriod,
		IndexTables: config.ProvisionConfig{
			ActiveTableProvisionConfig: config.ActiveTableProvisionConfig{
				ProvisionedWriteThroughput: write,
				ProvisionedReadThroughput:  read,
			},
			InactiveTableProvisionConfig: config.InactiveTableProvisionConfig{
				InactiveWriteThroughput:        inactiveWrite,
				InactiveReadThroughput:         inactiveRead,
				InactiveWriteScale:             inactiveScalingConfig,
				InactiveThroughputOnDemandMode: true,
				InactiveWriteScaleLastN:        1,
			},
		},
		ChunkTables: config.ProvisionConfig{
			ActiveTableProvisionConfig: config.ActiveTableProvisionConfig{
				ProvisionedWriteThroughput: write,
				ProvisionedReadThroughput:  read,
			},
			InactiveTableProvisionConfig: config.InactiveTableProvisionConfig{
				InactiveWriteThroughput:        inactiveWrite,
				InactiveReadThroughput:         inactiveRead,
				InactiveThroughputOnDemandMode: true,
			},
		},
	}
	tableManager, err := NewTableManager(tbmConfig, cfg, maxChunkAge, client, nil, nil, nil, log.NewNopLogger())
	if err != nil {
		t.Fatal(err)
	}

	// Check at time zero, we have the base table and one weekly table
	tmTest(t, client, tableManager,
		"Initial test",
		weeklyTableStart,
		[]config.TableDesc{
			{Name: baseTableName, ProvisionedRead: read, ProvisionedWrite: write},
			{Name: tablePrefix + week1Suffix, ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + week1Suffix, ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Fast forward table period + grace period, check we still have provisioned throughput
	tmTest(t, client, tableManager,
		"Move forward by table period + grace period",
		weeklyTableStart.Add(tablePeriod).Add(gracePeriod),
		[]config.TableDesc{
			{Name: baseTableName, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite, UseOnDemandIOMode: true},
			{Name: tablePrefix + week1Suffix, ProvisionedRead: read, ProvisionedWrite: write},
			{Name: tablePrefix + week2Suffix, ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + week1Suffix, ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + week2Suffix, ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Fast forward table period + max chunk age + grace period, check we remove provisioned throughput
	// Week 1 index table will not have dynamic mode enabled, since it has an active autoscale config for
	// a managed provisioning mode. However the week 1 chunk table will flip to the DynamicIO mode.
	tmTest(t, client, tableManager,
		"Move forward by table period + max chunk age + grace period",
		weeklyTableStart.Add(tablePeriod).Add(maxChunkAge).Add(gracePeriod),
		[]config.TableDesc{
			{Name: baseTableName, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite, UseOnDemandIOMode: true},
			{Name: tablePrefix + week1Suffix, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite, WriteScale: inactiveScalingConfig, UseOnDemandIOMode: false},
			{Name: tablePrefix + week2Suffix, ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + week1Suffix, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite, UseOnDemandIOMode: true},
			{Name: chunkTablePrefix + week2Suffix, ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// fast forward to another table period. Now week 1's dynamic mode will flip to true, as the managed autoscaling config is no longer active
	tmTest(t, client, tableManager,
		"Move forward by table period + max chunk age + grace period",
		weeklyTableStart.Add(tablePeriod*2).Add(maxChunkAge).Add(gracePeriod),
		[]config.TableDesc{
			{Name: baseTableName, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite, UseOnDemandIOMode: true},
			{Name: tablePrefix + week1Suffix, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite, UseOnDemandIOMode: true},
			{Name: tablePrefix + week2Suffix, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite, WriteScale: inactiveScalingConfig, UseOnDemandIOMode: false},
			{Name: tablePrefix + "5", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + week1Suffix, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite, UseOnDemandIOMode: true},
			{Name: chunkTablePrefix + week2Suffix, ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite, UseOnDemandIOMode: true},
			{Name: chunkTablePrefix + "5", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)
}

func TestTableManagerTags(t *testing.T) {
	client := newMockTableClient()

	test := func(tableManager *TableManager, name string, tm time.Time, expected []config.TableDesc) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			mtime.NowForce(tm)
			defer mtime.NowReset()
			if err := tableManager.SyncTables(ctx); err != nil {
				t.Fatal(err)
			}
			err := ExpectTables(ctx, client, expected)
			require.NoError(t, err)
		})
	}

	// Check at time zero, we have the base table with no tags.
	{
		cfg := config.SchemaConfig{
			Configs: []config.PeriodConfig{{
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{}},
			}},
		}
		tableManager, err := NewTableManager(TableManagerConfig{}, cfg, maxChunkAge, client, nil, nil, nil, log.NewNopLogger())
		if err != nil {
			t.Fatal(err)
		}

		test(
			tableManager,
			"Initial test",
			baseTableStart,
			[]config.TableDesc{
				{Name: ""},
			},
		)
	}

	// Check after restarting table manager we get some tags.
	{
		cfg := config.SchemaConfig{
			Configs: []config.PeriodConfig{{
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{
						Tags: config.Tags{"foo": "bar"},
					}},
			}},
		}
		tableManager, err := NewTableManager(TableManagerConfig{}, cfg, maxChunkAge, client, nil, nil, nil, log.NewNopLogger())
		if err != nil {
			t.Fatal(err)
		}

		test(
			tableManager,
			"Tagged test",
			baseTableStart,
			[]config.TableDesc{
				{Name: "", Tags: config.Tags{"foo": "bar"}},
			},
		)
	}
}

func TestTableManagerRetentionOnly(t *testing.T) {
	client := newMockTableClient()

	cfg := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From: config.DayTime{Time: model.TimeFromUnix(baseTableStart.Unix())},
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: tablePrefix,
						Period: tablePeriod,
					}},

				ChunkTables: config.PeriodicTableConfig{
					Prefix: chunkTablePrefix,
					Period: tablePeriod,
				},
			},
		},
	}
	tbmConfig := TableManagerConfig{
		RetentionPeriod:         tableRetention,
		RetentionDeletesEnabled: true,
		CreationGracePeriod:     gracePeriod,
		IndexTables: config.ProvisionConfig{
			ActiveTableProvisionConfig: config.ActiveTableProvisionConfig{
				ProvisionedWriteThroughput: write,
				ProvisionedReadThroughput:  read,
			},
			InactiveTableProvisionConfig: config.InactiveTableProvisionConfig{
				InactiveWriteThroughput: inactiveWrite,
				InactiveReadThroughput:  inactiveRead,
				InactiveWriteScale:      inactiveScalingConfig,
				InactiveWriteScaleLastN: autoScaleLastN,
			},
		},
		ChunkTables: config.ProvisionConfig{
			ActiveTableProvisionConfig: config.ActiveTableProvisionConfig{
				ProvisionedWriteThroughput: write,
				ProvisionedReadThroughput:  read,
			},
			InactiveTableProvisionConfig: config.InactiveTableProvisionConfig{
				InactiveWriteThroughput: inactiveWrite,
				InactiveReadThroughput:  inactiveRead,
			},
		},
	}
	tableManager, err := NewTableManager(tbmConfig, cfg, maxChunkAge, client, nil, nil, nil, log.NewNopLogger())
	if err != nil {
		t.Fatal(err)
	}

	// Check at time zero, we have one weekly table
	tmTest(t, client, tableManager,
		"Initial test",
		baseTableStart,
		[]config.TableDesc{
			{Name: tablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Check after one week, we have two weekly tables
	tmTest(t, client, tableManager,
		"Move forward by one table period",
		baseTableStart.Add(tablePeriod),
		[]config.TableDesc{
			{Name: tablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: tablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Check after two weeks, we have three tables (two previous periods and the new one)
	tmTest(t, client, tableManager,
		"Move forward by two table periods",
		baseTableStart.Add(tablePeriod*2),
		[]config.TableDesc{
			{Name: tablePrefix + "0", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite, WriteScale: inactiveScalingConfig},
			{Name: tablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: tablePrefix + "2", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "0", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: chunkTablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "2", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Check after three weeks, we have three tables (two previous periods and the new one), table 0 was deleted
	tmTest(t, client, tableManager,
		"Move forward by three table periods",
		baseTableStart.Add(tablePeriod*3),
		[]config.TableDesc{
			{Name: tablePrefix + "1", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite, WriteScale: inactiveScalingConfig},
			{Name: tablePrefix + "2", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: tablePrefix + "3", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "1", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: chunkTablePrefix + "2", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "3", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Check after three weeks and a day short by grace period, we have three tables (two previous periods and the new one), table 0 was deleted
	tmTest(t, client, tableManager,
		"Move forward by three table periods and a day short by grace period",
		baseTableStart.Add(tablePeriod*3+24*time.Hour-gracePeriod),
		[]config.TableDesc{
			{Name: tablePrefix + "1", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite, WriteScale: inactiveScalingConfig},
			{Name: tablePrefix + "2", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite, WriteScale: inactiveScalingConfig},
			{Name: tablePrefix + "3", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "1", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: chunkTablePrefix + "2", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: chunkTablePrefix + "3", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Verify that without RetentionDeletesEnabled no tables are removed
	tableManager.cfg.RetentionDeletesEnabled = false
	// Retention > 0 will prevent older tables from being created so we need to create the old tables manually for the test
	err = client.CreateTable(context.Background(), config.TableDesc{Name: tablePrefix + "0", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite, WriteScale: inactiveScalingConfig})
	require.NoError(t, err)

	err = client.CreateTable(context.Background(), config.TableDesc{Name: chunkTablePrefix + "0", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite})
	require.NoError(t, err)

	tmTest(t, client, tableManager,
		"Move forward by three table periods (no deletes)",
		baseTableStart.Add(tablePeriod*3),
		[]config.TableDesc{
			{Name: tablePrefix + "0", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite, WriteScale: inactiveScalingConfig},
			{Name: tablePrefix + "1", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite, WriteScale: inactiveScalingConfig},
			{Name: tablePrefix + "2", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: tablePrefix + "3", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "0", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: chunkTablePrefix + "1", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: chunkTablePrefix + "2", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "3", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Re-enable table deletions
	tableManager.cfg.RetentionDeletesEnabled = true

	// Verify that with a retention period of zero no tables outside the configs 'From' range are removed
	tableManager.cfg.RetentionPeriod = 0
	tableManager.schemaCfg.Configs[0].From = config.DayTime{Time: model.TimeFromUnix(baseTableStart.Add(tablePeriod).Unix())}
	// Retention > 0 will prevent older tables from being created so we need to create the old tables manually for the test
	err = client.CreateTable(context.Background(), config.TableDesc{Name: tablePrefix + "0", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite, WriteScale: inactiveScalingConfig})
	require.NoError(t, err)

	err = client.CreateTable(context.Background(), config.TableDesc{Name: chunkTablePrefix + "0", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite})
	require.NoError(t, err)

	tmTest(t, client, tableManager,
		"Move forward by three table periods (no deletes) and move From one table forward",
		baseTableStart.Add(tablePeriod*3),
		[]config.TableDesc{
			{Name: tablePrefix + "0", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite, WriteScale: inactiveScalingConfig},
			{Name: tablePrefix + "1", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite, WriteScale: inactiveScalingConfig},
			{Name: tablePrefix + "2", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: tablePrefix + "3", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "0", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: chunkTablePrefix + "1", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: chunkTablePrefix + "2", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "3", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Test table manager retention not multiple of periodic config
	tbmConfig.RetentionPeriod++
	_, err = NewTableManager(tbmConfig, cfg, maxChunkAge, client, nil, nil, nil, log.NewNopLogger())
	require.Error(t, err)
}
