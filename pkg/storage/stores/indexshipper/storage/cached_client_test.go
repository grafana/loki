package storage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/chunk/client"
)

type mockObjectClient struct {
	client.ObjectClient
	storageObjects []client.StorageObject
	errResp        error
	listCallsCount int
	listDelay      time.Duration
}

func newMockObjectClient(objects []string) *mockObjectClient {
	storageObjects := make([]client.StorageObject, 0, len(objects))
	for _, objectName := range objects {
		storageObjects = append(storageObjects, client.StorageObject{
			Key: objectName,
		})
	}

	return &mockObjectClient{
		storageObjects: storageObjects,
	}
}

func (m *mockObjectClient) List(_ context.Context, _, _ string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	defer func() {
		time.Sleep(m.listDelay)
		m.listCallsCount++
	}()

	if m.errResp != nil {
		return nil, nil, m.errResp
	}

	return m.storageObjects, []client.StorageCommonPrefix{}, nil
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

	objectClient := newMockObjectClient(objectsInStorage)
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
	require.Equal(t, 1, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{
		{Key: "table1/db1.gz"},
		{Key: "table1/db2.gz"},
	}, objects)
	require.Equal(t, []client.StorageCommonPrefix{}, commonPrefixes)

	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "table2/", "", false)
	require.NoError(t, err)
	require.Equal(t, 1, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{
		{Key: "table2/db1.gz"},
	}, objects)
	require.Equal(t, []client.StorageCommonPrefix{"table2/user1"}, commonPrefixes)

	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "table3/", "", false)
	require.NoError(t, err)
	require.Equal(t, 1, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{}, objects)
	require.Equal(t, []client.StorageCommonPrefix{"table3/user1"}, commonPrefixes)

	// list user objects from table2 and table3
	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "table2/user1/", "", false)
	require.NoError(t, err)
	require.Equal(t, 1, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{
		{
			Key: "table2/user1/db1.gz",
		},
	}, objects)
	require.Equal(t, []client.StorageCommonPrefix{}, commonPrefixes)

	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "table3/user1/", "", false)
	require.NoError(t, err)
	require.Equal(t, 1, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{
		{Key: "table3/user1/db1.gz"},
		{Key: "table3/user1/db2.gz"},
	}, objects)
	require.Equal(t, []client.StorageCommonPrefix{}, commonPrefixes)

	// list non-existent table
	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "table4/", "", false)
	require.NoError(t, err)
	require.Equal(t, 1, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{}, objects)
	require.Equal(t, []client.StorageCommonPrefix{}, commonPrefixes)

	// list non-existent user
	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "table3/user2/", "", false)
	require.NoError(t, err)
	require.Equal(t, 1, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{}, objects)
	require.Equal(t, []client.StorageCommonPrefix{}, commonPrefixes)
}

func TestCachedObjectClient_errors(t *testing.T) {
	objectsInStorage := []string{
		// table with just common dbs
		"table1/db1.gz",
		"table1/db2.gz",
	}

	objectClient := newMockObjectClient(objectsInStorage)
	cachedObjectClient := newCachedObjectClient(objectClient)

	// do the initial listing
	objects, commonPrefixes, err := cachedObjectClient.List(context.Background(), "", "", false)
	require.NoError(t, err)
	require.Equal(t, 1, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{}, objects)
	require.Equal(t, []client.StorageCommonPrefix{"table1"}, commonPrefixes)

	// timeout the cache and call List concurrently with objectClient throwing an error
	// objectClient must receive just one request and all the cachedObjectClient.List calls should get an error
	wg := sync.WaitGroup{}
	cachedObjectClient.cacheBuiltAt = time.Now().Add(-(cacheTimeout + time.Second))
	objectClient.listDelay = time.Millisecond * 100
	objectClient.errResp = errors.New("fake error")
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := cachedObjectClient.List(context.Background(), "", "", false)
			require.Error(t, err)
			require.Equal(t, 2, objectClient.listCallsCount)
		}()
	}

	wg.Wait()

	// clear the error and call the List concurrently again
	// objectClient must receive just one request and all the calls should not get any error
	objectClient.errResp = nil
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "", "", false)
			require.NoError(t, err)
			require.Equal(t, 3, objectClient.listCallsCount)
			require.Equal(t, []client.StorageObject{}, objects)
			require.Equal(t, []client.StorageCommonPrefix{"table1"}, commonPrefixes)
		}()
	}
	wg.Wait()
}
