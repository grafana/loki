package util

import (
	"context"
	"testing"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/stretchr/testify/require"
)

type mockHostedObjectClient struct {
	chunk.ObjectClient
	objects []chunk.StorageObject
}

func (m *mockHostedObjectClient) List(_ context.Context, _, _ string) ([]chunk.StorageObject, []chunk.StorageCommonPrefix, error) {
	return m.objects, []chunk.StorageCommonPrefix{}, nil
}

func TestCachedObjectClient_List(t *testing.T) {
	objectClient := &mockHostedObjectClient{
		objects: []chunk.StorageObject{
			{
				Key: "table1/obj1",
			},
			{
				Key: "table1/obj2",
			},
			{
				Key: "table2/obj1",
			},
			{
				Key: "table2/obj2",
			},
			{
				Key: "table3/obj1",
			},
			{
				Key: "table3/obj2",
			},
		},
	}

	cachedObjectClient := NewCachedObjectClient(objectClient)

	// list tables which should build the cache
	_, tables, err := cachedObjectClient.List(context.Background(), "", "")
	require.NoError(t, err)
	require.Equal(t, []chunk.StorageCommonPrefix{"table1", "table2", "table3"}, tables)

	// verify whether cache has right items
	require.Len(t, cachedObjectClient.tables, 3)
	require.Equal(t, objectClient.objects[:2], cachedObjectClient.tables["table1"])
	require.Equal(t, objectClient.objects[2:4], cachedObjectClient.tables["table2"])
	require.Equal(t, objectClient.objects[4:], cachedObjectClient.tables["table3"])

	// remove table 3
	objectClient.objects = objectClient.objects[:4]

	// list tables again which should clear the cache before building it again
	_, tables, err = cachedObjectClient.List(context.Background(), "", "")
	require.NoError(t, err)
	require.Equal(t, []chunk.StorageCommonPrefix{"table1", "table2"}, tables)

	// verify whether cache has right items and table3 is gone
	require.Len(t, cachedObjectClient.tables, 2)
	require.Equal(t, objectClient.objects[:2], cachedObjectClient.tables["table1"])
	require.Equal(t, objectClient.objects[2:], cachedObjectClient.tables["table2"])

	// list table1 objects
	objects, _, err := cachedObjectClient.List(context.Background(), "table1/", "")
	require.NoError(t, err)
	require.Equal(t, objectClient.objects[:2], objects)

	// verify whether table1 got evicted
	require.Len(t, cachedObjectClient.tables, 1)
	require.Contains(t, cachedObjectClient.tables, "table2")

	// list table2 objects
	objects, _, err = cachedObjectClient.List(context.Background(), "table2/", "")
	require.NoError(t, err)
	require.Equal(t, objectClient.objects[2:], objects)

	// verify whether table2 got evicted as well
	require.Len(t, cachedObjectClient.tables, 0)

	// list table1 again which should rebuild the cache
	objects, _, err = cachedObjectClient.List(context.Background(), "table1/", "")
	require.NoError(t, err)
	require.Equal(t, objectClient.objects[:2], objects)

	// verify whether cache was rebuilt and table1 got evicted already
	require.Len(t, cachedObjectClient.tables, 1)
	require.Contains(t, cachedObjectClient.tables, "table2")

	// verify whether listing non-existing table should not error
	objects, _, err = cachedObjectClient.List(context.Background(), "table3/", "")
	require.NoError(t, err)
	require.Len(t, objects, 0)
}
