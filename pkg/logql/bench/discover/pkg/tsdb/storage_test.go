package tsdb

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/aws"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/azure"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/gcp"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/stretchr/testify/require"
)

type fakeObjectClient struct{}

func (fakeObjectClient) ObjectExists(context.Context, string) (bool, error) { return false, nil }
func (fakeObjectClient) GetAttributes(context.Context, string) (client.ObjectAttributes, error) {
	return client.ObjectAttributes{}, nil
}
func (fakeObjectClient) PutObject(context.Context, string, io.Reader) error { return nil }
func (fakeObjectClient) GetObject(context.Context, string) (io.ReadCloser, int64, error) {
	return io.NopCloser(strings.NewReader("")), 0, nil
}
func (fakeObjectClient) GetObjectRange(context.Context, string, int64, int64) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}
func (fakeObjectClient) List(context.Context, string, string) ([]client.StorageObject, []client.StorageCommonPrefix, error) {
	return nil, nil, nil
}
func (fakeObjectClient) DeleteObject(context.Context, string) error { return nil }
func (fakeObjectClient) IsObjectNotFoundErr(error) bool             { return false }
func (fakeObjectClient) IsRetryableErr(error) bool                  { return false }
func (fakeObjectClient) Stop()                                      {}

func TestNewObjectClient(t *testing.T) {
	origS3 := newS3ObjectClient
	origGCS := newGCSObjectClient
	origAzure := newAzureObjectClient
	origFS := newFSObjectClient
	t.Cleanup(func() {
		newS3ObjectClient = origS3
		newGCSObjectClient = origGCS
		newAzureObjectClient = origAzure
		newFSObjectClient = origFS
	})

	t.Run("s3 backend", func(t *testing.T) {
		called := false
		newS3ObjectClient = func(cfg aws.S3Config, hedgingCfg hedging.Config) (client.ObjectClient, error) {
			called = true
			return fakeObjectClient{}, nil
		}

		obj, err := NewObjectClient(StorageConfig{StorageType: StorageTypeS3})
		require.NoError(t, err)
		require.NotNil(t, obj)
		require.True(t, called)
	})

	t.Run("gcs backend", func(t *testing.T) {
		called := false
		newGCSObjectClient = func(ctx context.Context, cfg gcp.GCSConfig, hedgingCfg hedging.Config) (client.ObjectClient, error) {
			called = true
			return fakeObjectClient{}, nil
		}

		obj, err := NewObjectClient(StorageConfig{StorageType: StorageTypeGCS})
		require.NoError(t, err)
		require.NotNil(t, obj)
		require.True(t, called)
	})

	t.Run("azure backend", func(t *testing.T) {
		called := false
		newAzureObjectClient = func(cfg *azure.BlobStorageConfig, hedgingCfg hedging.Config) (client.ObjectClient, error) {
			called = true
			return fakeObjectClient{}, nil
		}

		obj, err := NewObjectClient(StorageConfig{StorageType: StorageTypeAzure})
		require.NoError(t, err)
		require.NotNil(t, obj)
		require.True(t, called)
	})

	t.Run("filesystem backend", func(t *testing.T) {
		called := false
		newFSObjectClient = func(cfg local.FSConfig) (client.ObjectClient, error) {
			called = true
			return fakeObjectClient{}, nil
		}

		obj, err := NewObjectClient(StorageConfig{StorageType: StorageTypeFilesystem})
		require.NoError(t, err)
		require.NotNil(t, obj)
		require.True(t, called)
	})

	t.Run("unsupported backend", func(t *testing.T) {
		obj, err := NewObjectClient(StorageConfig{StorageType: StorageType("swift")})
		require.Error(t, err)
		require.Nil(t, obj)
		require.Contains(t, err.Error(), "unsupported --storage-type")
	})
}

func TestNewIndexStorageClient(t *testing.T) {
	origFS := newFSObjectClient
	t.Cleanup(func() { newFSObjectClient = origFS })

	t.Run("creates index storage client for filesystem", func(t *testing.T) {
		dir := t.TempDir()
		cfg := StorageConfig{
			StorageType: StorageTypeFilesystem,
			Bucket:      dir,
			Tenant:      "tenant-a",
		}

		idxClient, err := NewIndexStorageClient(cfg)
		require.NoError(t, err)
		require.NotNil(t, idxClient)

		tables, err := idxClient.ListTables(context.Background())
		require.NoError(t, err)
		require.Empty(t, tables)
		idxClient.Stop()
	})

	t.Run("returns validation errors before constructor calls", func(t *testing.T) {
		called := false
		newFSObjectClient = func(cfg local.FSConfig) (client.ObjectClient, error) {
			called = true
			return fakeObjectClient{}, nil
		}

		idxClient, err := NewIndexStorageClient(StorageConfig{StorageType: StorageTypeFilesystem, Tenant: "tenant-a"})
		require.Error(t, err)
		require.Nil(t, idxClient)
		require.False(t, called)
	})
}
