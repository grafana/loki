package bucket

import (
	"bytes"
	"context"
	"sort"
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/bucket/filesystem"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"
)

func TestObjectClientAdapter_List(t *testing.T) {
	tests := []struct {
		name              string
		prefix            string
		delimiter         string
		storageObjKeys    []string
		storageCommonPref []client.StorageCommonPrefix
		wantErr           error
	}{
		{
			"list_top_level_only",
			"",
			"/",
			[]string{"top-level-file-1", "top-level-file-2"},
			[]client.StorageCommonPrefix{"dir-1/", "dir-2/", "depply/"},
			nil,
		},
		{
			"list_all_dir_1",
			"dir-1",
			"",
			[]string{"dir-1/file-1", "dir-1/file-2"},
			nil,
			nil,
		},
		{
			"list_recursive",
			"",
			"",
			[]string{
				"top-level-file-1",
				"top-level-file-2",
				"dir-1/file-1",
				"dir-1/file-2",
				"dir-2/file-3",
				"dir-2/file-4",
				"dir-2/file-5",
				"depply/nested/folder/a",
				"depply/nested/folder/b",
				"depply/nested/folder/c",
			},
			nil,
			nil,
		},
		{
			"unknown_prefix",
			"test",
			"",
			[]string{},
			nil,
			nil,
		},
		{
			"only_storage_common_prefix",
			"depply/",
			"/",
			[]string{},
			[]client.StorageCommonPrefix{
				"depply/nested/",
			},
			nil,
		},
	}

	for _, tt := range tests {
		config := filesystem.Config{
			Directory: t.TempDir(),
		}
		newBucket, err := filesystem.NewBucketClient(config)
		require.NoError(t, err)

		buff := bytes.NewBufferString("foo")
		require.NoError(t, newBucket.Upload(context.Background(), "top-level-file-1", buff))
		require.NoError(t, newBucket.Upload(context.Background(), "top-level-file-2", buff))
		require.NoError(t, newBucket.Upload(context.Background(), "dir-1/file-1", buff))
		require.NoError(t, newBucket.Upload(context.Background(), "dir-1/file-2", buff))
		require.NoError(t, newBucket.Upload(context.Background(), "dir-2/file-3", buff))
		require.NoError(t, newBucket.Upload(context.Background(), "dir-2/file-4", buff))
		require.NoError(t, newBucket.Upload(context.Background(), "dir-2/file-5", buff))
		require.NoError(t, newBucket.Upload(context.Background(), "depply/nested/folder/a", buff))
		require.NoError(t, newBucket.Upload(context.Background(), "depply/nested/folder/b", buff))
		require.NoError(t, newBucket.Upload(context.Background(), "depply/nested/folder/c", buff))

		client, err := NewObjectClient(context.Background(), "filesystem", ConfigWithNamedStores{
			Config: Config{
				Filesystem: config,
			},
		}, "test", hedging.Config{}, false, log.NewNopLogger())
		require.NoError(t, err)

		storageObj, storageCommonPref, err := client.List(context.Background(), tt.prefix, tt.delimiter)
		if tt.wantErr != nil {
			require.Equal(t, tt.wantErr.Error(), err.Error())
			continue
		}

		keys := []string{}
		for _, key := range storageObj {
			keys = append(keys, key.Key)
		}

		sort.Slice(tt.storageObjKeys, func(i, j int) bool {
			return tt.storageObjKeys[i] < tt.storageObjKeys[j]
		})
		sort.Slice(tt.storageCommonPref, func(i, j int) bool {
			return tt.storageCommonPref[i] < tt.storageCommonPref[j]
		})

		require.NoError(t, err)
		require.Equal(t, tt.storageObjKeys, keys)
		require.Equal(t, tt.storageCommonPref, storageCommonPref)
	}
}
