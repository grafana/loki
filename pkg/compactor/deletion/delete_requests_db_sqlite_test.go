package deletion

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/testutil"
)

func TestDeleteRequestsDBSQLite(t *testing.T) {
	// build test table
	tempDir := t.TempDir()

	workingDir := filepath.Join(tempDir, "working-dir")
	objectStorePath := filepath.Join(tempDir, "object-store")

	objectClient, err := local.NewFSObjectClient(local.FSConfig{
		Directory: objectStorePath,
	})
	require.NoError(t, err)
	sqliteDB, err := newSQLiteDB(workingDir, storage.NewIndexStorageClient(objectClient, ""))
	require.NoError(t, err)

	// see if delete requests db was created
	require.NotEmpty(t, sqliteDB.path)
	require.FileExists(t, sqliteDB.path)

	// create a table
	require.NoError(t, sqliteDB.Exec(context.Background(), true, sqlQuery{query: `CREATE TABLE user (id TEXT PRIMARY KEY)`}))

	// add some records to the db
	require.NoError(t, sqliteDB.Exec(context.Background(), true, sqlQuery{
		query: `INSERT INTO user VALUES (?)`,
		execOpts: &sqlitex.ExecOptions{
			Args: []any{"1"},
		},
	}, sqlQuery{
		query: `INSERT INTO user VALUES (?)`,
		execOpts: &sqlitex.ExecOptions{
			Args: []any{"2"},
		},
	}))

	expectedUserIDsInDB := []string{"1", "2"}

	// see if right records were written
	conn, err := sqliteDB.connPool.Take(context.Background())
	require.NoError(t, err)
	require.Equal(t, expectedUserIDsInDB, getUserIDsWithConn(t, conn))
	sqliteDB.connPool.Put(conn)

	// upload the file to the storage
	require.NoError(t, sqliteDB.uploadFile())
	storageFilePath := filepath.Join(objectStorePath, DeleteRequestsTableName+"/"+deleteRequestsDBSQLiteFileNameGZ)
	require.FileExists(t, storageFilePath)

	// validate records in the storage db
	require.Equal(t, expectedUserIDsInDB, getUserIDsFromFile(t, storageFilePath))

	// add more records to the db
	require.NoError(t, sqliteDB.Exec(context.Background(), true, sqlQuery{
		query: `INSERT INTO user VALUES (?)`,
		execOpts: &sqlitex.ExecOptions{
			Args: []any{"3"},
		},
	}, sqlQuery{
		query: `INSERT INTO user VALUES (?)`,
		execOpts: &sqlitex.ExecOptions{
			Args: []any{"4"},
		},
	}))
	expectedUserIDsInDB = append(expectedUserIDsInDB, "3", "4")

	// stop the table which should upload the db to storage
	sqliteDB.Stop()

	// see if the storage db got the new records
	require.Equal(t, expectedUserIDsInDB, getUserIDsFromFile(t, storageFilePath))

	// remove local db
	require.NoError(t, os.Remove(sqliteDB.path))
	require.NoError(t, err)

	// re-create db to see if the db gets downloaded locally since it does not exist anymore
	sqliteDB, err = newSQLiteDB(workingDir, storage.NewIndexStorageClient(objectClient, ""))
	require.NoError(t, err)
	defer sqliteDB.Stop()

	require.NotEmpty(t, sqliteDB.path)

	// validate records in local db
	conn, err = sqliteDB.connPool.Take(context.Background())
	require.NoError(t, err)
	require.Equal(t, expectedUserIDsInDB, getUserIDsWithConn(t, conn))
	sqliteDB.connPool.Put(conn)
}

func getUserIDsFromFile(t *testing.T, storageFilePath string) []string {
	tempDir := t.TempDir()
	tempFilePath := filepath.Join(tempDir, DeleteRequestsTableName)
	testutil.DecompressFile(t, storageFilePath, tempFilePath)

	conn, err := sqlite.OpenConn(tempFilePath)
	require.NoError(t, err)

	defer func() {
		require.NoError(t, conn.Close())
	}()

	return getUserIDsWithConn(t, conn)
}

func getUserIDsWithConn(t *testing.T, conn *sqlite.Conn) []string {
	var userIDs []string
	require.NoError(t, sqlitex.Execute(conn, `SELECT * FROM user`, &sqlitex.ExecOptions{
		ResultFunc: func(stmt *sqlite.Stmt) error {
			userIDs = append(userIDs, stmt.ColumnText(0))
			return nil
		},
	}))

	return userIDs
}
