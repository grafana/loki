package chunk

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/mtime"

	"github.com/weaveworks/cortex/pkg/util"
)

const (
	tablePrefix      = "cortex_"
	chunkTablePrefix = "chunks_"
	tablePeriod      = 7 * 24 * time.Hour
	gracePeriod      = 15 * time.Minute
	maxChunkAge      = 12 * time.Hour
	inactiveWrite    = 1
	inactiveRead     = 2
	write            = 200
	read             = 100
	autoScaleLastN   = 2
	autoScaleMin     = 50
	autoScaleMax     = 500
	autoScaleTarget  = 80
)

type mockTableClient struct {
	sync.Mutex
	tables map[string]TableDesc
}

func newMockTableClient() *mockTableClient {
	return &mockTableClient{
		tables: map[string]TableDesc{},
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

func (m *mockTableClient) CreateTable(_ context.Context, desc TableDesc) error {
	m.Lock()
	defer m.Unlock()

	m.tables[desc.Name] = desc
	return nil
}

func (m *mockTableClient) DescribeTable(_ context.Context, name string) (desc TableDesc, isActive bool, err error) {
	m.Lock()
	defer m.Unlock()

	return m.tables[name], true, nil
}

func (m *mockTableClient) UpdateTable(_ context.Context, current, expected TableDesc) error {
	m.Lock()
	defer m.Unlock()

	m.tables[current.Name] = expected
	return nil
}

func tmTest(t *testing.T, client *mockTableClient, tableManager *TableManager, name string, tm time.Time, expected []TableDesc) {
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

var activeScalingConfig = AutoScalingConfig{
	Enabled:     true,
	MinCapacity: autoScaleMin * 2,
	MaxCapacity: autoScaleMax * 2,
	TargetValue: autoScaleTarget,
}
var inactiveScalingConfig = AutoScalingConfig{
	Enabled:     true,
	MinCapacity: autoScaleMin,
	MaxCapacity: autoScaleMax,
	TargetValue: autoScaleTarget,
}

func TestTableManager(t *testing.T) {
	client := newMockTableClient()

	cfg := SchemaConfig{
		UsePeriodicTables: true,
		IndexTables: PeriodicTableConfig{
			Prefix: tablePrefix,
			Period: tablePeriod,
			From:   util.NewDayValue(model.TimeFromUnix(0)),
			ProvisionedWriteThroughput: write,
			ProvisionedReadThroughput:  read,
			InactiveWriteThroughput:    inactiveWrite,
			InactiveReadThroughput:     inactiveRead,
			WriteScale:                 activeScalingConfig,
			InactiveWriteScale:         inactiveScalingConfig,
			InactiveWriteScaleLastN:    autoScaleLastN,
		},

		ChunkTables: PeriodicTableConfig{
			Prefix: chunkTablePrefix,
			Period: tablePeriod,
			From:   util.NewDayValue(model.TimeFromUnix(0)),
			ProvisionedWriteThroughput: write,
			ProvisionedReadThroughput:  read,
			InactiveWriteThroughput:    inactiveWrite,
			InactiveReadThroughput:     inactiveRead,
		},

		CreationGracePeriod: gracePeriod,
	}
	tableManager, err := NewTableManager(cfg, maxChunkAge, client)
	if err != nil {
		t.Fatal(err)
	}

	// Check at time zero, we have the base table and one weekly table
	tmTest(t, client, tableManager,
		"Initial test",
		time.Unix(0, 0),
		[]TableDesc{
			{Name: "", ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: tablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: chunkTablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Check running twice doesn't change anything
	tmTest(t, client, tableManager,
		"Nothing changed",
		time.Unix(0, 0),
		[]TableDesc{
			{Name: "", ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: tablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: chunkTablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Fast forward grace period, check we still have write throughput on base table
	tmTest(t, client, tableManager,
		"Move forward by grace period",
		time.Unix(0, 0).Add(gracePeriod),
		[]TableDesc{
			{Name: "", ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: tablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: chunkTablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Fast forward max chunk age + grace period, check write throughput on base table has gone
	// (and we don't put inactive auto-scaling on base table)
	tmTest(t, client, tableManager,
		"Move forward by max chunk age + grace period",
		time.Unix(0, 0).Add(maxChunkAge).Add(gracePeriod),
		[]TableDesc{
			{Name: "", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: chunkTablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Fast forward table period - grace period, check we add another weekly table
	tmTest(t, client, tableManager,
		"Move forward by table period - grace period",
		time.Unix(0, 0).Add(tablePeriod).Add(-gracePeriod),
		[]TableDesc{
			{Name: "", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: tablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: chunkTablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Fast forward table period + grace period, check we still have provisioned throughput
	tmTest(t, client, tableManager,
		"Move forward by table period + grace period",
		time.Unix(0, 0).Add(tablePeriod).Add(gracePeriod),
		[]TableDesc{
			{Name: "", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: tablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: chunkTablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Fast forward table period + max chunk age + grace period, check we remove provisioned throughput
	tmTest(t, client, tableManager,
		"Move forward by table period + max chunk age + grace period",
		time.Unix(0, 0).Add(tablePeriod).Add(maxChunkAge).Add(gracePeriod),
		[]TableDesc{
			{Name: "", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + "0", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite, WriteScale: inactiveScalingConfig},
			{Name: tablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: chunkTablePrefix + "0", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: chunkTablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Check running twice doesn't change anything
	tmTest(t, client, tableManager,
		"Nothing changed",
		time.Unix(0, 0).Add(tablePeriod).Add(maxChunkAge).Add(gracePeriod),
		[]TableDesc{
			{Name: "", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + "0", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite, WriteScale: inactiveScalingConfig},
			{Name: tablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write, WriteScale: activeScalingConfig},
			{Name: chunkTablePrefix + "0", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: chunkTablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)
}

func TestTableManagerAutoscaleInactiveOnly(t *testing.T) {
	client := newMockTableClient()

	cfg := SchemaConfig{
		UsePeriodicTables: true,
		IndexTables: PeriodicTableConfig{
			Prefix: tablePrefix,
			Period: tablePeriod,
			From:   util.NewDayValue(model.TimeFromUnix(0)),
			ProvisionedWriteThroughput: write,
			ProvisionedReadThroughput:  read,
			InactiveWriteThroughput:    inactiveWrite,
			InactiveReadThroughput:     inactiveRead,
			InactiveWriteScale:         inactiveScalingConfig,
			InactiveWriteScaleLastN:    autoScaleLastN,
		},

		ChunkTables: PeriodicTableConfig{
			Prefix: chunkTablePrefix,
			Period: tablePeriod,
			From:   util.NewDayValue(model.TimeFromUnix(0)),
			ProvisionedWriteThroughput: write,
			ProvisionedReadThroughput:  read,
			InactiveWriteThroughput:    inactiveWrite,
			InactiveReadThroughput:     inactiveRead,
		},

		CreationGracePeriod: gracePeriod,
	}
	tableManager, err := NewTableManager(cfg, maxChunkAge, client)
	if err != nil {
		t.Fatal(err)
	}

	// Check at time zero, we have the base table and one weekly table
	tmTest(t, client, tableManager,
		"Initial test",
		time.Unix(0, 0),
		[]TableDesc{
			{Name: "", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: tablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Fast forward table period + grace period, check we still have provisioned throughput
	tmTest(t, client, tableManager,
		"Move forward by table period + grace period",
		time.Unix(0, 0).Add(tablePeriod).Add(gracePeriod),
		[]TableDesc{
			{Name: "", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: tablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Fast forward table period + max chunk age + grace period, check we remove provisioned throughput
	tmTest(t, client, tableManager,
		"Move forward by table period + max chunk age + grace period",
		time.Unix(0, 0).Add(tablePeriod).Add(maxChunkAge).Add(gracePeriod),
		[]TableDesc{
			{Name: "", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + "0", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite, WriteScale: inactiveScalingConfig},
			{Name: tablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "0", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: chunkTablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)
}

func TestTableManagerTags(t *testing.T) {
	client := newMockTableClient()

	test := func(tableManager *TableManager, name string, tm time.Time, expected []TableDesc) {
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
		tableManager, err := NewTableManager(SchemaConfig{}, maxChunkAge, client)
		if err != nil {
			t.Fatal(err)
		}

		test(
			tableManager,
			"Initial test",
			time.Unix(0, 0),
			[]TableDesc{
				{Name: ""},
			},
		)
	}

	// Check after restarting table manager we get some tags.
	{
		cfg := SchemaConfig{}
		cfg.IndexTables.Tags.Set("foo=bar")
		tableManager, err := NewTableManager(cfg, maxChunkAge, client)
		if err != nil {
			t.Fatal(err)
		}

		test(
			tableManager,
			"Tagged test",
			time.Unix(0, 0),
			[]TableDesc{
				{Name: "", Tags: Tags{"foo": "bar"}},
			},
		)
	}
}
