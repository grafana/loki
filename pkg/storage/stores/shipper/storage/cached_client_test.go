package storage

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/chunk"
)

type mockObjectClient struct {
	chunk.ObjectClient
	storageObjects []chunk.StorageObject
	errResp        error
	listCallsCount int
	listDelay      time.Duration
}

func newMockObjectClient(objects []string) *mockObjectClient {
	storageObjects := make([]chunk.StorageObject, 0, len(objects))
	for _, objectName := range objects {
		storageObjects = append(storageObjects, chunk.StorageObject{
			Key: objectName,
		})
	}

	return &mockObjectClient{
		storageObjects: storageObjects,
	}
}

func (m *mockObjectClient) List(_ context.Context, _, _ string) ([]chunk.StorageObject, []chunk.StorageCommonPrefix, error) {
	defer func() {
		time.Sleep(m.listDelay)
		m.listCallsCount++
	}()

	if m.errResp != nil {
		return nil, nil, m.errResp
	}

	return m.storageObjects, []chunk.StorageCommonPrefix{}, nil
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
	objects, commonPrefixes, err := cachedObjectClient.List(context.Background(), "", "")
	require.NoError(t, err)
	require.Equal(t, 1, objectClient.listCallsCount)
	require.Equal(t, []chunk.StorageObject{}, objects)
	require.Equal(t, []chunk.StorageCommonPrefix{"table1", "table2", "table3"}, commonPrefixes)

	// list objects in all 3 tables
	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "table1/", "")
	require.NoError(t, err)
	require.Equal(t, 1, objectClient.listCallsCount)
	require.Equal(t, []chunk.StorageObject{
		{Key: "table1/db1.gz"},
		{Key: "table1/db2.gz"},
	}, objects)
	require.Equal(t, []chunk.StorageCommonPrefix{}, commonPrefixes)

	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "table2/", "")
	require.NoError(t, err)
	require.Equal(t, 1, objectClient.listCallsCount)
	require.Equal(t, []chunk.StorageObject{
		{Key: "table2/db1.gz"},
	}, objects)
	require.Equal(t, []chunk.StorageCommonPrefix{"table2/user1"}, commonPrefixes)

	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "table3/", "")
	require.NoError(t, err)
	require.Equal(t, 1, objectClient.listCallsCount)
	require.Equal(t, []chunk.StorageObject{}, objects)
	require.Equal(t, []chunk.StorageCommonPrefix{"table3/user1"}, commonPrefixes)

	// list user objects from table2 and table3
	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "table2/user1/", "")
	require.NoError(t, err)
	require.Equal(t, 1, objectClient.listCallsCount)
	require.Equal(t, []chunk.StorageObject{
		{
			Key: "table2/user1/db1.gz",
		},
	}, objects)
	require.Equal(t, []chunk.StorageCommonPrefix{}, commonPrefixes)

	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "table3/user1/", "")
	require.NoError(t, err)
	require.Equal(t, 1, objectClient.listCallsCount)
	require.Equal(t, []chunk.StorageObject{
		{Key: "table3/user1/db1.gz"},
		{Key: "table3/user1/db2.gz"},
	}, objects)
	require.Equal(t, []chunk.StorageCommonPrefix{}, commonPrefixes)

	// list non-existent table
	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "table4/", "")
	require.NoError(t, err)
	require.Equal(t, 1, objectClient.listCallsCount)
	require.Equal(t, []chunk.StorageObject{}, objects)
	require.Equal(t, []chunk.StorageCommonPrefix{}, commonPrefixes)

	// list non-existent user
	objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "table3/user2/", "")
	require.NoError(t, err)
	require.Equal(t, 1, objectClient.listCallsCount)
	require.Equal(t, []chunk.StorageObject{}, objects)
	require.Equal(t, []chunk.StorageCommonPrefix{}, commonPrefixes)
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
	require.Equal(t, []chunk.StorageObject{}, objects)
	require.Equal(t, []chunk.StorageCommonPrefix{"table1"}, commonPrefixes)

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
			_, _, err := cachedObjectClient.List(context.Background(), "", "")
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
			objects, commonPrefixes, err = cachedObjectClient.List(context.Background(), "", "")
			require.NoError(t, err)
			require.Equal(t, 3, objectClient.listCallsCount)
			require.Equal(t, []chunk.StorageObject{}, objects)
			require.Equal(t, []chunk.StorageCommonPrefix{"table1"}, commonPrefixes)
		}()
	}
	wg.Wait()
}
