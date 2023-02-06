package indexshipper_test

import (
	"bytes"
	"context"
	"path"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
)

func TestBoltDBShipperTableClient(t *testing.T) {
	tempDir := t.TempDir()

	cm := storage.NewClientMetrics()
	objectClient, err := storage.NewObjectClient("filesystem", storage.Config{FSConfig: local.FSConfig{Directory: tempDir}}, cm)
	require.NoError(t, err)

	// create a couple of folders with files
	foldersWithFiles := map[string][]string{
		"table1": {"file1", "file2", "file3"},
		"table2": {"file3", "file4"},
		"table3": {"file5", "file6"},
	}

	for folder, files := range foldersWithFiles {
		for _, fileName := range files {
			// we will use "index/" prefix for all the objects
			err := objectClient.PutObject(context.Background(), path.Join("index", folder, fileName), bytes.NewReader([]byte{}))
			require.NoError(t, err)
		}
	}

	tableClient := indexshipper.NewTableClient(objectClient, "index/")

	// check list of tables returns all the folders/tables created above
	checkExpectedTables(t, tableClient, foldersWithFiles)

	// let us delete table1 and see if it goes away from the list of tables
	err = tableClient.DeleteTable(context.Background(), "table1")
	require.NoError(t, err)

	delete(foldersWithFiles, "table1")
	checkExpectedTables(t, tableClient, foldersWithFiles)
}

func checkExpectedTables(t *testing.T, tableClient index.TableClient, expectedTables map[string][]string) {
	actualTables, err := tableClient.ListTables(context.Background())
	require.NoError(t, err)

	require.Len(t, actualTables, len(expectedTables))

	for _, table := range actualTables {
		_, ok := expectedTables[table]
		require.True(t, ok)
	}
}
