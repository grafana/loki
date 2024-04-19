package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
)

var objectsMtime = time.Now().Local()

type mockObjectClient struct {
	client.ObjectClient
	storageObjects []client.StorageObject
	errResp        error
	listCallsCount int
	listDelay      time.Duration
}

func newMockObjectClient(t *testing.T, objects []string) *mockObjectClient {
	tempDir := t.TempDir()
	for _, objectName := range objects {
		objectFullPath := filepath.Join(tempDir, objectName)
		parentDir := filepath.Dir(objectFullPath)
		require.NoError(t, util.EnsureDirectory(parentDir))
		require.NoError(t, os.WriteFile(objectFullPath, []byte("foo"), 0644))
		require.NoError(t, os.Chtimes(objectFullPath, objectsMtime, objectsMtime))
	}

	objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: tempDir})
	require.NoError(t, err)
	return &mockObjectClient{
		ObjectClient: objectClient,
	}
}

func (m *mockObjectClient) List(ctx context.Context, prefix, delimiter string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	defer func() {
		time.Sleep(m.listDelay)
		m.listCallsCount++
	}()

	if m.errResp != nil {
		return nil, nil, m.errResp
	}

	return m.ObjectClient.List(ctx, prefix, delimiter)
}

func TestCachedObjectClient_List(t *testing.T) {
	objectKeys := func(items []client.StorageObject) []string {
		keys := make([]string, 0, len(items))
		for _, item := range items {
			keys = append(keys, item.Key)
		}
		return keys
	}

	t.Run("refresh table name cache if requested table is not in cache", func(t *testing.T) {
		ctx := context.Background()

		oldObjectsInStorage := []string{
			"table1/db.gz",
		}

		objectClient := newMockObjectClient(t, oldObjectsInStorage)
		cachedObjectClient := newCachedObjectClient(objectClient)

		// populate table names case with a List() operation
		objects, commonPrefixes, err := cachedObjectClient.List(ctx, "", "/", false)
		require.Nil(t, err)
		require.Equal(t, objects, []client.StorageObject{})
		require.Equal(t, commonPrefixes, []client.StorageCommonPrefix{"table1"})

		newObjectsInStorage := []string{
			"table1/db.gz",
			"table2/db.gz",
		}

		// replace mock object client with one that returns more tables
		cachedObjectClient.ObjectClient = newMockObjectClient(t, newObjectsInStorage)

		// list contents of a table that is in table name cache
		objects, _, err = cachedObjectClient.listTable(ctx, "table1")
		require.Nil(t, err)
		require.Equal(t, []string{"table1/db.gz"}, objectKeys(objects))
		objectsFromListCall, _, _ := cachedObjectClient.List(ctx, "table1/", "/", false)
		require.Equal(t, objectsFromListCall, objects)

		// list contents of a table that is not in table name cache but exists on object storage
		objects, _, err = cachedObjectClient.listTable(ctx, "table2")
		require.Nil(t, err)
		require.Equal(t, []string{"table2/db.gz"}, objectKeys(objects))
		objectsFromListCall, _, _ = cachedObjectClient.List(ctx, "table2/", "/", false)
		require.Equal(t, objectsFromListCall, objects)

		// list contents of a table that is not in table name cache and does not exist on object storage
		objects, _, err = cachedObjectClient.listTable(ctx, "table3")
		require.Nil(t, err)
		require.Equal(t, []string{}, objectKeys(objects))
		objectsFromListCall, _, _ = cachedObjectClient.List(ctx, "table3/", "/", false)
		require.Equal(t, objectsFromListCall, objects)
	})

	t.Run("supports prefixed clients", func(t *testing.T) {
		ctx := context.Background()

		prefix := "my/amazing/prefix/"
		objectsInStorage := []string{
			prefix + "table1/db.gz",
			prefix + "table2/db.gz",
			prefix + "table2/db2.gz",
		}
		objectClient := newMockObjectClient(t, objectsInStorage)
		prefixedClient := client.NewPrefixedObjectClient(objectClient, prefix)
		cachedObjectClient := newCachedObjectClient(prefixedClient)

		objects, _, err := cachedObjectClient.List(ctx, "table2/", "/", false)
		require.Nil(t, err)
		require.Equal(t, []string{"table2/db.gz", "table2/db2.gz"}, objectKeys(objects))
	})
}

func TestCachedObjectClient(t *testing.T) {
	objectsInStorage := []string{
		// table with just common dbs
		"table1/db1.gz",
		"table1/db2.gz",

		// table with both common and user dbs
		"table2/db1.gz",
		"table2/user1/db1.gz",

		// table with just user dbs
		"table3/user1/db1.gz",
		"table3/user1/db2.gz",
	}

	objectClient := newMockObjectClient(t, objectsInStorage)
	cachedObjectClient := newCachedObjectClient(objectClient)

	// list tables
	objects, commonPrefixes, err := cachedObjectClient.List(context.Background(), "", "", false)
	require.NoError(t, err)
	require.Equal(t, 1, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{}, objects)
	require.Equal(t, []client.StorageCommonPrefix{"table1", "table2", "table3"}, commonPrefixes)

	// list objects in all 3 tables
	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "table1/", "", false)
	require.NoError(t, err)
	require.Equal(t, 2, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{
		{Key: "table1/db1.gz", ModifiedAt: objectsMtime},
		{Key: "table1/db2.gz", ModifiedAt: objectsMtime},
	}, objects)
	require.Equal(t, []client.StorageCommonPrefix{}, commonPrefixes)

	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "table2/", "", false)
	require.NoError(t, err)
	require.Equal(t, 3, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{
		{Key: "table2/db1.gz", ModifiedAt: objectsMtime},
	}, objects)
	require.Equal(t, []client.StorageCommonPrefix{"table2/user1"}, commonPrefixes)

	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "table3/", "", false)
	require.NoError(t, err)
	require.Equal(t, 4, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{}, objects)
	require.Equal(t, []client.StorageCommonPrefix{"table3/user1"}, commonPrefixes)

	// list user objects from table2 and table3, which should not make any new list calls
	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "table2/user1/", "", false)
	require.NoError(t, err)
	require.Equal(t, 4, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{
		{
			Key:        "table2/user1/db1.gz",
			ModifiedAt: objectsMtime,
		},
	}, objects)
	require.Equal(t, []client.StorageCommonPrefix{}, commonPrefixes)

	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "table3/user1/", "", false)
	require.NoError(t, err)
	require.Equal(t, 4, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{
		{Key: "table3/user1/db1.gz", ModifiedAt: objectsMtime},
		{Key: "table3/user1/db2.gz", ModifiedAt: objectsMtime},
	}, objects)
	require.Equal(t, []client.StorageCommonPrefix{}, commonPrefixes)

	// list non-existent table
	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "table4/", "", false)
	require.NoError(t, err)
	require.Equal(t, 5, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{}, objects)
	require.Equal(t, []client.StorageCommonPrefix{}, commonPrefixes)

	// list non-existent user
	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "table3/user2/", "", false)
	require.NoError(t, err)
	require.Equal(t, 5, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{}, objects)
	require.Equal(t, []client.StorageCommonPrefix{}, commonPrefixes)
}

func TestCachedObjectClient_errors(t *testing.T) {
	objectsInStorage := []string{
		"table1/db1.gz",
		"table1/u1/db2.gz",
	}

	for _, tc := range []struct {
		name                   string
		prefix                 string
		expectedObjects        []client.StorageObject
		expectedCommonPrefixes []client.StorageCommonPrefix
	}{
		{
			name:            "list tables",
			prefix:          "",
			expectedObjects: []client.StorageObject{},
			expectedCommonPrefixes: []client.StorageCommonPrefix{
				"table1",
			},
		},
		{
			name:   "list table1",
			prefix: "table1/",
			expectedObjects: []client.StorageObject{
				{Key: "table1/db1.gz", ModifiedAt: objectsMtime},
			},
			expectedCommonPrefixes: []client.StorageCommonPrefix{
				"table1/u1",
			},
		},
		{
			name:   "list table1/u1/",
			prefix: "table1/u1/",
			expectedObjects: []client.StorageObject{
				{Key: "table1/u1/db2.gz", ModifiedAt: objectsMtime},
			},
			expectedCommonPrefixes: []client.StorageCommonPrefix{},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			objectClient := newMockObjectClient(t, objectsInStorage)
			cachedObjectClient := newCachedObjectClient(objectClient)

			// do the initial listing
			objects, commonPrefixes, err := cachedObjectClient.List(context.Background(), tc.prefix, "", false)
			require.NoError(t, err)
			require.Equal(t, tc.expectedObjects, objects)
			require.Equal(t, tc.expectedCommonPrefixes, commonPrefixes)
			expectedListCallsCount := objectClient.listCallsCount

			// timeout the cache and call List concurrently with objectClient throwing an error
			// objectClient must receive just one request and all the cachedObjectClient.List calls should get an error
			wg := sync.WaitGroup{}
			cachedObjectClient.tableNamesCacheBuiltAt = time.Now().Add(-(cacheTimeout + time.Second))
			cachedObjectClient.tables["table1"].cacheBuiltAt = time.Now().Add(-(cacheTimeout + time.Second))
			objectClient.listDelay = time.Millisecond * 100
			objectClient.errResp = errors.New("fake error")
			expectedListCallsCount++
			for i := 0; i < 5; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, _, err := cachedObjectClient.List(context.Background(), tc.prefix, "", false)
					require.Error(t, err)
					require.Equal(t, expectedListCallsCount, objectClient.listCallsCount)
				}()
			}

			wg.Wait()

			// clear the error and call the List concurrently again
			// objectClient must receive just one request and all the calls should not get any error
			objectClient.errResp = nil
			expectedListCallsCount++
			for i := 0; i < 5; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					objects, commonPrefixes, err := cachedObjectClient.List(context.Background(), tc.prefix, "", false)
					require.NoError(t, err)
					require.Equal(t, expectedListCallsCount, objectClient.listCallsCount)
					require.Equal(t, tc.expectedObjects, objects)
					require.Equal(t, tc.expectedCommonPrefixes, commonPrefixes)
				}()
			}
			wg.Wait()
		})
	}
}
