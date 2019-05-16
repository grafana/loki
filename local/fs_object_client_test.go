package local

import (
	"context"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFsObjectClient_DeleteChunksBefore(t *testing.T) {
	deleteFilesOlderThan := 10 * time.Minute

	fsChunksDir, err := ioutil.TempDir(os.TempDir(), "fs-chunks")
	require.NoError(t, err)

	bucketClient, err := NewFSObjectClient(FSConfig{
		Directory: fsChunksDir,
	})
	require.NoError(t, err)

	defer func() {
		require.NoError(t, os.RemoveAll(fsChunksDir))
	}()

	file1 := "file1"
	file2 := "file2"

	// Creating dummy files
	require.NoError(t, os.Chdir(fsChunksDir))

	f, err := os.Create(file1)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	f, err = os.Create(file2)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	// Verify whether all files are created
	files, _ := ioutil.ReadDir(".")
	require.Equal(t, 2, len(files), "Number of files should be 2")

	// No files should be deleted, since all of them are not much older
	require.NoError(t, bucketClient.DeleteChunksBefore(context.Background(), time.Now().Add(-deleteFilesOlderThan)))
	files, _ = ioutil.ReadDir(".")
	require.Equal(t, 2, len(files), "Number of files should be 2")

	// Changing mtime of file1 to make it look older
	require.NoError(t, os.Chtimes(file1, time.Now().Add(-deleteFilesOlderThan), time.Now().Add(-deleteFilesOlderThan)))
	require.NoError(t, bucketClient.DeleteChunksBefore(context.Background(), time.Now().Add(-deleteFilesOlderThan)))

	// Verifying whether older file got deleted
	files, _ = ioutil.ReadDir(".")
	require.Equal(t, 1, len(files), "Number of files should be 1 after enforcing retention")
}
