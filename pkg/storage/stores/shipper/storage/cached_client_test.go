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

func (m *mockObjectClient) Stop() {}

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
	objects, commonPrefixes, err := cachedObjectClient.List(context.Background(), "", "")
	require.NoError(t, err)
	require.Equal(t, 1, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{}, objects)
	require.Equal(t, []client.StorageCommonPrefix{"table1", "table2", "table3"}, commonPrefixes)

	// list objects in all 3 tables
	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "table1/", "")
	require.NoError(t, err)
	require.Equal(t, 1, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{
		{Key: "table1/db1.gz"},
		{Key: "table1/db2.gz"},
	}, objects)
	require.Equal(t, []client.StorageCommonPrefix{}, commonPrefixes)

	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "table2/", "")
	require.NoError(t, err)
	require.Equal(t, 1, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{
		{Key: "table2/db1.gz"},
	}, objects)
	require.Equal(t, []client.StorageCommonPrefix{"table2/user1"}, commonPrefixes)

	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "table3/", "")
	require.NoError(t, err)
	require.Equal(t, 1, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{}, objects)
	require.Equal(t, []client.StorageCommonPrefix{"table3/user1"}, commonPrefixes)

	// list user objects from table2 and table3
	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "table2/user1/", "")
	require.NoError(t, err)
	require.Equal(t, 1, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{
		{
			Key: "table2/user1/db1.gz",
		},
	}, objects)
	require.Equal(t, []client.StorageCommonPrefix{}, commonPrefixes)

	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "table3/user1/", "")
	require.NoError(t, err)
	require.Equal(t, 1, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{
		{Key: "table3/user1/db1.gz"},
		{Key: "table3/user1/db2.gz"},
	}, objects)
	require.Equal(t, []client.StorageCommonPrefix{}, commonPrefixes)

	// list non-existent table
	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "table4/", "")
	require.NoError(t, err)
	require.Equal(t, 1, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{}, objects)
	require.Equal(t, []client.StorageCommonPrefix{}, commonPrefixes)

	// list non-existent user
	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "table3/user2/", "")
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
	objects, commonPrefixes, err := cachedObjectClient.List(context.Background(), "", "")
	require.NoError(t, err)
	require.Equal(t, 1, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{}, objects)
	require.Equal(t, []client.StorageCommonPrefix{"table1"}, commonPrefixes)

	// Refresh the cache and call List concurrently with objectClient throwing an error.
	// All the cachedObjectClient.List calls should get an error.
	objectClient.listDelay = time.Millisecond * 100
	objectClient.errResp = errors.New("fake error")
	cachedObjectClient.RefreshCache()
	require.Equal(t, 2, objectClient.listCallsCount)

	wg := sync.WaitGroup{}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := cachedObjectClient.List(context.Background(), "", "")
			require.Error(t, err)
		}()
	}

	wg.Wait()

	require.Equal(t, 2, objectClient.listCallsCount)

	// Clear the error and refresh the cache.
	// objectClient must receive just one request from cache refresh and all the calls should not get any error
	objectClient.errResp = nil
	cachedObjectClient.RefreshCache()

	require.Equal(t, 3, objectClient.listCallsCount)

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "", "")
			require.NoError(t, err)
			require.Equal(t, []client.StorageObject{}, objects)
			require.Equal(t, []client.StorageCommonPrefix{"table1"}, commonPrefixes)
		}()
	}
	wg.Wait()

	require.Equal(t, 3, objectClient.listCallsCount)
}

func TestCachedObjectClient_AutoRefresh(t *testing.T) {
	objectsInStorage := []string{
		// table with just common dbs
		"table1/db1.gz",
		"table1/db2.gz",
	}

	objectClient := newMockObjectClient(objectsInStorage)
	cachedObjectClient := newCachedObjectClient(objectClient)

	// initial cache should have been built already
	objects, commonPrefixes, err := cachedObjectClient.List(context.Background(), "", "")
	require.NoError(t, err)
	require.Equal(t, 1, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{}, objects)
	require.Equal(t, []client.StorageCommonPrefix{"table1"}, commonPrefixes)

	// add a new table to the storage
	objectClient.storageObjects = append(objectClient.storageObjects, client.StorageObject{
		Key: "table2/db1.gz",
	})

	// see that without enabling cache auto refresh, we do not get back the new table name in response
	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "", "")
	require.NoError(t, err)
	require.Equal(t, 1, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{}, objects)
	require.Equal(t, []client.StorageCommonPrefix{"table1"}, commonPrefixes)

	// enable auto refresh and see that it got refreshed already
	cachedObjectClient.EnableCacheAutoRefresh()

	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "", "")
	require.NoError(t, err)
	require.Equal(t, 2, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{}, objects)
	require.Equal(t, []client.StorageCommonPrefix{"table1", "table2"}, commonPrefixes)

	// disable the auto refresh
	cachedObjectClient.DisableCacheAutoRefresh()
	require.Equal(t, 0, cachedObjectClient.cacheAutoRefreshEnabledCount)
	require.Nil(t, cachedObjectClient.cacheAutoRefresher)

	// concurrently enable the auto refresh
	wg := sync.WaitGroup{}
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cachedObjectClient.EnableCacheAutoRefresh()
		}()
	}

	wg.Wait()

	// see that the cache is refreshed only once
	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "", "")
	require.NoError(t, err)
	require.Equal(t, 3, objectClient.listCallsCount)
	require.Equal(t, []client.StorageObject{}, objects)
	require.Equal(t, []client.StorageCommonPrefix{"table1", "table2"}, commonPrefixes)

	cachedObjectClient.Stop()
}

func TestCacheAutoRefresher(t *testing.T) {
	cacheRefreshCount := 0
	cacheAutoRefresher := newCacheAutoRefresher(func() {
		cacheRefreshCount++
	}, time.Second/2)

	// sleep for some time and stop the timer to ensure cache refresh func is called
	time.Sleep(time.Second + 100*time.Millisecond)
	cacheAutoRefresher.stop()
	require.Equal(t, 2, cacheRefreshCount)

	// sleep again to ensure cache refresh func is not called anymore and ensure things are cleaned up
	time.Sleep(time.Second)
	require.Equal(t, 2, cacheRefreshCount)
	cacheAutoRefresher.wg.Wait()
	select {
	case _, ok := <-cacheAutoRefresher.stopChan:
		require.False(t, ok)
	default:
		t.Fatal("stopChan should be closed")
	}
}
