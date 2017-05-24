package chunk

import (
	"sort"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/mtime"
	"golang.org/x/net/context"

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
)

func TestTableManager(t *testing.T) {
	dynamoDB := newMockDynamoDB(0, 0)
	client := dynamoTableClient{
		DynamoDB: dynamoDB,
	}

	cfg := TableManagerConfig{
		PeriodicTableConfig: PeriodicTableConfig{
			UsePeriodicTables:    true,
			TablePrefix:          tablePrefix,
			TablePeriod:          tablePeriod,
			PeriodicTableStartAt: util.NewDayValue(model.TimeFromUnix(0)),
		},

		PeriodicChunkTableConfig: PeriodicChunkTableConfig{
			ChunkTablePrefix: chunkTablePrefix,
			ChunkTablePeriod: tablePeriod,
			ChunkTableFrom:   util.NewDayValue(model.TimeFromUnix(0)),
		},

		CreationGracePeriod:                  gracePeriod,
		MaxChunkAge:                          maxChunkAge,
		ProvisionedWriteThroughput:           write,
		ProvisionedReadThroughput:            read,
		InactiveWriteThroughput:              inactiveWrite,
		InactiveReadThroughput:               inactiveRead,
		ChunkTableProvisionedWriteThroughput: write,
		ChunkTableProvisionedReadThroughput:  read,
		ChunkTableInactiveWriteThroughput:    inactiveWrite,
		ChunkTableInactiveReadThroughput:     inactiveRead,
	}
	tableManager, err := NewTableManager(cfg, client)
	if err != nil {
		t.Fatal(err)
	}

	test := func(name string, tm time.Time, expected []TableDesc) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			mtime.NowForce(tm)
			if err := tableManager.syncTables(ctx); err != nil {
				t.Fatal(err)
			}
			expectTables(ctx, t, client, expected)
		})
	}

	// Check at time zero, we have the base table and one weekly table
	test(
		"Initial test",
		time.Unix(0, 0),
		[]TableDesc{
			{Name: "", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: tablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Check running twice doesn't change anything
	test(
		"Nothing changed",
		time.Unix(0, 0),
		[]TableDesc{
			{Name: "", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: tablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Fast forward grace period, check we still have write throughput on base table
	test(
		"Move forward by grace period",
		time.Unix(0, 0).Add(gracePeriod),
		[]TableDesc{
			{Name: "", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: tablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Fast forward max chunk age + grace period, check write throughput on base table has gone
	test(
		"Move forward by max chunk age + grace period",
		time.Unix(0, 0).Add(maxChunkAge).Add(gracePeriod),
		[]TableDesc{
			{Name: "", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Fast forward table period - grace period, check we add another weekly table
	test(
		"Move forward by table period - grace period",
		time.Unix(0, 0).Add(tablePeriod).Add(-gracePeriod),
		[]TableDesc{
			{Name: "", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: tablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Fast forward table period + grace period, check we still have provisioned throughput
	test(
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
	test(
		"Move forward by table period + max chunk age + grace period",
		time.Unix(0, 0).Add(tablePeriod).Add(maxChunkAge).Add(gracePeriod),
		[]TableDesc{
			{Name: "", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + "0", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "0", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: chunkTablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Check running twice doesn't change anything
	test(
		"Nothing changed",
		time.Unix(0, 0).Add(tablePeriod).Add(maxChunkAge).Add(gracePeriod),
		[]TableDesc{
			{Name: "", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + "0", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "0", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: chunkTablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)
}

func TestTableManagerTags(t *testing.T) {
	dynamoDB := newMockDynamoDB(0, 0)
	client := dynamoTableClient{
		DynamoDB: dynamoDB,
	}

	test := func(tableManager *TableManager, name string, tm time.Time, expected []TableDesc) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			mtime.NowForce(tm)
			if err := tableManager.syncTables(ctx); err != nil {
				t.Fatal(err)
			}
			expectTables(ctx, t, client, expected)
		})
	}

	// Check at time zero, we have the base table with no tags.
	{
		tableManager, err := NewTableManager(TableManagerConfig{}, client)
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
		cfg := TableManagerConfig{}
		cfg.TableTags.Set("foo=bar")
		tableManager, err := NewTableManager(cfg, client)
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

func expectTables(ctx context.Context, t *testing.T, dynamo TableClient, expected []TableDesc) {
	tables, err := dynamo.ListTables(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(expected) != len(tables) {
		t.Fatalf("Unexpected number of tables: %v != %v", expected, tables)
	}

	sort.Strings(tables)
	sort.Sort(byName(expected))

	for i, expect := range expected {
		if tables[i] != expect.Name {
			t.Fatalf("Expected '%s', found '%s'", expect.Name, tables[i])
		}

		desc, _, err := dynamo.DescribeTable(ctx, expect.Name)
		if err != nil {
			t.Fatal(err)
		}

		if !desc.Equals(expect) {
			t.Fatalf("Expected '%v', found '%v' for table '%s'", expect, desc, desc.Name)
		}
	}
}
