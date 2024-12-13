package local

import (
	"bytes"
	"context"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
)

func TestFSObjectClient_DeleteChunksBefore(t *testing.T) {
	deleteFilesOlderThan := 10 * time.Minute

	fsChunksDir := t.TempDir()

	bucketClient, err := NewFSObjectClient(FSConfig{
		Directory: fsChunksDir,
	})
	require.NoError(t, err)

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
	files, _ := os.ReadDir(".")
	require.Equal(t, 2, len(files), "Number of files should be 2")

	// No files should be deleted, since all of them are not much older
	require.NoError(t, bucketClient.DeleteChunksBefore(context.Background(), time.Now().Add(-deleteFilesOlderThan)))
	files, _ = os.ReadDir(".")
	require.Equal(t, 2, len(files), "Number of files should be 2")

	// Changing mtime of file1 to make it look older
	require.NoError(t, os.Chtimes(file1, time.Now().Add(-deleteFilesOlderThan), time.Now().Add(-deleteFilesOlderThan)))
	require.NoError(t, bucketClient.DeleteChunksBefore(context.Background(), time.Now().Add(-deleteFilesOlderThan)))

	// Verifying whether older file got deleted
	files, _ = os.ReadDir(".")
	require.Equal(t, 1, len(files), "Number of files should be 1 after enforcing retention")
}

func TestFSObjectClient_List_and_ObjectExists(t *testing.T) {
	fsObjectsDir := t.TempDir()

	bucketClient, err := NewFSObjectClient(FSConfig{
		Directory: fsObjectsDir,
	})
	require.NoError(t, err)

	allFiles := []string{
		"outer-file1",
		"outer-file2",
		"folder1/file1",
		"folder1/file2",
		"folder2/file3",
		"folder2/file4",
		"folder2/file5",
		"deeply/nested/folder/a",
		"deeply/nested/folder/b",
		"deeply/nested/folder/c",
	}

	topLevelFolders := map[string]bool{}
	topLevelFiles := map[string]bool{}
	filesInTopLevelFolders := map[string]map[string]bool{}

	for _, f := range allFiles {
		require.NoError(t, bucketClient.PutObject(context.Background(), f, bytes.NewReader([]byte(f))))

		s := strings.Split(f, "/")
		if len(s) > 1 {
			topLevelFolders[s[0]] = true
		} else {
			topLevelFiles[s[0]] = true
		}

		if len(s) == 2 {
			if filesInTopLevelFolders[s[0]] == nil {
				filesInTopLevelFolders[s[0]] = map[string]bool{}
			}
			filesInTopLevelFolders[s[0]][s[1]] = true
		}
	}

	// create an empty directory which should get excluded from the list
	require.NoError(t, util.EnsureDirectory(filepath.Join(fsObjectsDir, "empty-folder")))

	storageObjects, commonPrefixes, err := bucketClient.List(context.Background(), "", "/")
	require.NoError(t, err)

	require.Len(t, storageObjects, len(topLevelFiles))
	for _, so := range storageObjects {
		require.True(t, topLevelFiles[so.Key])
	}

	require.Len(t, commonPrefixes, len(topLevelFolders))
	for _, commonPrefix := range commonPrefixes {
		require.True(t, topLevelFolders[string(commonPrefix)[:len(commonPrefix)-1]]) // 1 to remove "/" separator.
	}

	for folder, files := range filesInTopLevelFolders {
		storageObjects, commonPrefixes, err := bucketClient.List(context.Background(), folder, "/")
		require.NoError(t, err)

		require.Len(t, storageObjects, len(files))
		for _, so := range storageObjects {
			require.True(t, strings.HasPrefix(so.Key, folder+"/"))
			require.True(t, files[path.Base(so.Key)])
		}

		require.Len(t, commonPrefixes, 0)
	}

	// List everything from the top, recursively.
	storageObjects, commonPrefixes, err = bucketClient.List(context.Background(), "", "")
	require.NoError(t, err)

	// Since delimiter is empty, there are no commonPrefixes.
	require.Empty(t, commonPrefixes)

	var storageObjectPaths []string
	for _, so := range storageObjects {
		storageObjectPaths = append(storageObjectPaths, so.Key)
	}
	require.ElementsMatch(t, allFiles, storageObjectPaths)

	storageObjects, commonPrefixes, err = bucketClient.List(context.Background(), "doesnt_exist", "")
	require.NoError(t, err)
	require.Empty(t, storageObjects)
	require.Empty(t, commonPrefixes)

	storageObjects, commonPrefixes, err = bucketClient.List(context.Background(), "outer-file1", "")
	require.NoError(t, err)
	require.Len(t, storageObjects, 1)
	require.Equal(t, "outer-file1", storageObjects[0].Key)
	require.Empty(t, commonPrefixes)

	ok, err := bucketClient.ObjectExists(context.Background(), "outer-file2")
	require.NoError(t, err)
	require.True(t, ok)

	attrs, err := bucketClient.GetAttributes(context.Background(), "outer-file2")
	require.NoError(t, err)
	require.True(t, ok)
	require.EqualValues(t, len("outer-file2"), attrs.Size)
}

func TestFSObjectClient_DeleteObject(t *testing.T) {
	fsObjectsDir := t.TempDir()

	bucketClient, err := NewFSObjectClient(FSConfig{
		Directory: fsObjectsDir,
	})
	require.NoError(t, err)

	foldersWithFiles := make(map[string][]string)
	foldersWithFiles["folder1"] = []string{"file1", "file2"}

	for folder, files := range foldersWithFiles {
		for _, filename := range files {
			err := bucketClient.PutObject(context.Background(), path.Join(folder, filename), bytes.NewReader([]byte(filename)))
			require.NoError(t, err)
		}
	}

	// let us check if we have right folders created
	_, commonPrefixes, err := bucketClient.List(context.Background(), "", "/")
	require.NoError(t, err)
	require.Len(t, commonPrefixes, len(foldersWithFiles))

	// let us delete file1 from folder1 and check that file1 is gone but folder1 with file2 is still there
	require.NoError(t, bucketClient.DeleteObject(context.Background(), path.Join("folder1", "file1")))
	_, err = os.Stat(filepath.Join(fsObjectsDir, filepath.Join("folder1", "file1")))
	require.True(t, os.IsNotExist(err))

	_, err = os.Stat(filepath.Join(fsObjectsDir, filepath.Join("folder1", "file2")))
	require.NoError(t, err)

	// let us delete second file as well and check that folder1 also got removed
	require.NoError(t, bucketClient.DeleteObject(context.Background(), path.Join("folder1", "file2")))
	_, err = os.Stat(filepath.Join(fsObjectsDir, "folder1"))
	require.True(t, os.IsNotExist(err))

	_, err = os.Stat(fsObjectsDir)
	require.NoError(t, err)

	// let us see ensure folder2 is still there will all the files:
	/*files, commonPrefixes, err := bucketClient.List(context.Background(), "folder2/")
	require.NoError(t, err)
	require.Len(t, commonPrefixes, 0)
	require.Len(t, files, len(foldersWithFiles["folder2/"]))*/
}
