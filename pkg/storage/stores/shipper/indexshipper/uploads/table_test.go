package uploads

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
)

const (
	testTableName = "test-table"
)

func TestTable(t *testing.T) {
	tempDir := t.TempDir()
	storageClient := buildTestStorageClient(t, tempDir)
	testTable := NewTable(testTableName, storageClient)
	defer testTable.Stop()

	for userIdx := 0; userIdx < 2; userIdx++ {
		userID := "user-" + strconv.Itoa(userIdx)
		t.Run(userID, func(t *testing.T) {
			userIndexPath := filepath.Join(tempDir, testTableName, userID)
			require.NoError(t, os.MkdirAll(userIndexPath, 0755))

			// build some test indexes and add them to the table.
			testIndexes := buildTestIndexes(t, userIndexPath, 5)
			for _, testIndex := range testIndexes {
				require.NoError(t, testTable.AddIndex(userID, testIndex))
			}

			// see if we can find all the added indexes in the table.
			indexesFound := map[string]*mockIndex{}
			err := testTable.ForEach(userID, func(_ bool, index index.Index) error {
				indexesFound[index.Path()] = index.(*mockIndex)
				return nil
			})
			require.NoError(t, err)

			require.Equal(t, testIndexes, indexesFound)
		})
	}
}
