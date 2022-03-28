package avatica

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

var registerer prometheus.Registerer
var testTableName = "test_chunk_loki_table"

var cfg = Config{
	"",
	"test_chunk_loki_table",
	"root",
	flagext.Secret{Value: "test"},
	0,
	"",
	BackendAlibabacloudLindorm,
	16,
	3,
	10 * time.Minute,
	time.Minute,
	backoff.Config{},
}

func TestTableClient_CreateTableQuery(t *testing.T) {
	if cfg.Addresses == "" { //skip test
		return
	}
	client, err := NewTableClient(context.Background(), cfg)
	require.NoError(t, err)
	desc, _, _ := client.DescribeTable(context.Background(), testTableName)
	err = client.CreateTable(context.Background(), desc)
	require.NoError(t, err)
}

func TestTableClient_ListTables(t *testing.T) {
	if cfg.Addresses == "" { //skip test
		return
	}
	client, err := NewTableClient(context.Background(), cfg)
	require.NoError(t, err)
	tables, err := client.ListTables(context.Background())
	require.NoError(t, err)
	require.Equal(t, true, len(tables) > 0)
}

func TestTableClient_DeleteTable(t *testing.T) {
	if cfg.Addresses == "" { //skip test
		return
	}
	client, err := NewTableClient(context.Background(), cfg)
	require.NoError(t, err)
	err = client.DeleteTable(context.Background(), testTableName)
	require.NoError(t, err)
}
