package shipper

import (
	"bytes"
	"context"
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/local"
	"github.com/grafana/loki/pkg/storage/chunk/storage"
	"github.com/grafana/loki/pkg/storage/stores/util"
)

func TestBoltDBShipperTableClient(t *testing.T) {
	tempDir, err := ioutil.TempDir("", "boltdb-shipper")
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	objectClient, err := storage.NewObjectClient("filesystem", storage.Config{FSConfig: local.FSConfig{Directory: tempDir}})
	require.NoError(t, err)

	// create a couple of folders with files
	foldersWithFiles := map[string][]string{
		"table1": {"file1", "file2", "file3"},
		"table2": {"file3", "file4"},
		"table3": {"file5", "file6"},
	}

	// we need to use prefixed object client while creating files/folder
	prefixedObjectClient := util.NewPrefixedObjectClient(objectClient, "index/")

	for folder, files := range foldersWithFiles {
		for _, fileName := range files {
			err := prefixedObjectClient.PutObject(context.Background(), path.Join(folder, fileName), bytes.NewReader([]byte{}))
			require.NoError(t, err)
		}
	}

	tableClient := NewBoltDBShipperTableClient(objectClient, "index/")

	// check list of tables returns all the folders/tables created above
	checkExpectedTables(t, tableClient, foldersWithFiles)

	// let us delete table1 and see if it goes away from the list of tables
	err = tableClient.DeleteTable(context.Background(), "table1")
	require.NoError(t, err)

	delete(foldersWithFiles, "table1")
	checkExpectedTables(t, tableClient, foldersWithFiles)
}

func checkExpectedTables(t *testing.T, tableClient chunk.TableClient, expectedTables map[string][]string) {
	actualTables, err := tableClient.ListTables(context.Background())
	require.NoError(t, err)

	require.Len(t, actualTables, len(expectedTables))

	for _, table := range actualTables {
		_, ok := expectedTables[table]
		require.True(t, ok)
	}
}
