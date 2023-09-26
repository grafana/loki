package cassandra

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTableClient_getCreateTableQuery_default(t *testing.T) {
	client := &tableClient{
		cfg: Config{},
	}
	desc, _, _ := client.DescribeTable(context.Background(), "test_table")
	query := client.getCreateTableQuery(&desc)
	assert.Equal(
		t,
		`
		CREATE TABLE IF NOT EXISTS test_table (
			hash text,
			range blob,
			value blob,
			PRIMARY KEY (hash, range)
		)`,
		query,
	)
}

func TestTableClient_getCreateTableQuery_withOptions(t *testing.T) {
	client := &tableClient{
		cfg: Config{
			TableOptions: "CLUSTERING ORDER BY (range DESC) AND compaction = { 'class' : 'LeveledCompactionStrategy' }",
		},
	}
	desc, _, _ := client.DescribeTable(context.Background(), "test_table")
	query := client.getCreateTableQuery(&desc)
	assert.Equal(
		t,
		`
		CREATE TABLE IF NOT EXISTS test_table (
			hash text,
			range blob,
			value blob,
			PRIMARY KEY (hash, range)
		) WITH CLUSTERING ORDER BY (range DESC) AND compaction = { 'class' : 'LeveledCompactionStrategy' }`,
		query,
	)
}
