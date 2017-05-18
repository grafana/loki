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
	dynamoDB := NewMockStorage()

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
	tableManager, err := NewTableManager(cfg, dynamoDB)
	if err != nil {
		t.Fatal(err)
	}

	test := func(name string, tm time.Time, expected []tableDescription) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			mtime.NowForce(tm)
			if err := tableManager.syncTables(ctx); err != nil {
				t.Fatal(err)
			}
			expectTables(ctx, t, dynamoDB, expected)
		})
	}

	// Check at time zero, we have the base table and one weekly table
	test(
		"Initial test",
		time.Unix(0, 0),
		[]tableDescription{
			{name: "", provisionedRead: read, provisionedWrite: write},
			{name: tablePrefix + "0", provisionedRead: read, provisionedWrite: write},
			{name: chunkTablePrefix + "0", provisionedRead: read, provisionedWrite: write},
		},
	)

	// Check running twice doesn't change anything
	test(
		"Nothing changed",
		time.Unix(0, 0),
		[]tableDescription{
			{name: "", provisionedRead: read, provisionedWrite: write},
			{name: tablePrefix + "0", provisionedRead: read, provisionedWrite: write},
			{name: chunkTablePrefix + "0", provisionedRead: read, provisionedWrite: write},
		},
	)

	// Fast forward grace period, check we still have write throughput on base table
	test(
		"Move forward by grace period",
		time.Unix(0, 0).Add(gracePeriod),
		[]tableDescription{
			{name: "", provisionedRead: read, provisionedWrite: write},
			{name: tablePrefix + "0", provisionedRead: read, provisionedWrite: write},
			{name: chunkTablePrefix + "0", provisionedRead: read, provisionedWrite: write},
		},
	)

	// Fast forward max chunk age + grace period, check write throughput on base table has gone
	test(
		"Move forward by max chunk age + grace period",
		time.Unix(0, 0).Add(maxChunkAge).Add(gracePeriod),
		[]tableDescription{
			{name: "", provisionedRead: inactiveRead, provisionedWrite: inactiveWrite},
			{name: tablePrefix + "0", provisionedRead: read, provisionedWrite: write},
			{name: chunkTablePrefix + "0", provisionedRead: read, provisionedWrite: write},
		},
	)

	// Fast forward table period - grace period, check we add another weekly table
	test(
		"Move forward by table period - grace period",
		time.Unix(0, 0).Add(tablePeriod).Add(-gracePeriod),
		[]tableDescription{
			{name: "", provisionedRead: inactiveRead, provisionedWrite: inactiveWrite},
			{name: tablePrefix + "0", provisionedRead: read, provisionedWrite: write},
			{name: tablePrefix + "1", provisionedRead: read, provisionedWrite: write},
			{name: chunkTablePrefix + "0", provisionedRead: read, provisionedWrite: write},
			{name: chunkTablePrefix + "1", provisionedRead: read, provisionedWrite: write},
		},
	)

	// Fast forward table period + grace period, check we still have provisioned throughput
	test(
		"Move forward by table period + grace period",
		time.Unix(0, 0).Add(tablePeriod).Add(gracePeriod),
		[]tableDescription{
			{name: "", provisionedRead: inactiveRead, provisionedWrite: inactiveWrite},
			{name: tablePrefix + "0", provisionedRead: read, provisionedWrite: write},
			{name: tablePrefix + "1", provisionedRead: read, provisionedWrite: write},
			{name: chunkTablePrefix + "0", provisionedRead: read, provisionedWrite: write},
			{name: chunkTablePrefix + "1", provisionedRead: read, provisionedWrite: write},
		},
	)

	// Fast forward table period + max chunk age + grace period, check we remove provisioned throughput
	test(
		"Move forward by table period + max chunk age + grace period",
		time.Unix(0, 0).Add(tablePeriod).Add(maxChunkAge).Add(gracePeriod),
		[]tableDescription{
			{name: "", provisionedRead: inactiveRead, provisionedWrite: inactiveWrite},
			{name: tablePrefix + "0", provisionedRead: inactiveRead, provisionedWrite: inactiveWrite},
			{name: tablePrefix + "1", provisionedRead: read, provisionedWrite: write},
			{name: chunkTablePrefix + "0", provisionedRead: inactiveRead, provisionedWrite: inactiveWrite},
			{name: chunkTablePrefix + "1", provisionedRead: read, provisionedWrite: write},
		},
	)

	// Check running twice doesn't change anything
	test(
		"Nothing changed",
		time.Unix(0, 0).Add(tablePeriod).Add(maxChunkAge).Add(gracePeriod),
		[]tableDescription{
			{name: "", provisionedRead: inactiveRead, provisionedWrite: inactiveWrite},
			{name: tablePrefix + "0", provisionedRead: inactiveRead, provisionedWrite: inactiveWrite},
			{name: tablePrefix + "1", provisionedRead: read, provisionedWrite: write},
			{name: chunkTablePrefix + "0", provisionedRead: inactiveRead, provisionedWrite: inactiveWrite},
			{name: chunkTablePrefix + "1", provisionedRead: read, provisionedWrite: write},
		},
	)
}

func expectTables(ctx context.Context, t *testing.T, dynamo TableClient, expected []tableDescription) {
	tables, err := dynamo.ListTables(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(expected) != len(tables) {
		t.Fatalf("Unexpected number of tables: %v != %v", expected, tables)
	}

	sort.Strings(tables)
	sort.Sort(byName(expected))

	for i, desc := range expected {
		if tables[i] != desc.name {
			t.Fatalf("Expected '%s', found '%s'", desc.name, tables[i])
		}

		read, write, _, err := dynamo.DescribeTable(ctx, desc.name)
		if err != nil {
			t.Fatal(err)
		}

		if read != desc.provisionedRead {
			t.Fatalf("Expected '%d', found '%d' for table '%s'", desc.provisionedRead, read, desc.name)
		}

		if write != desc.provisionedWrite {
			t.Fatalf("Expected '%d', found '%d' for table '%s'", desc.provisionedWrite, write, desc.name)
		}
	}
}
