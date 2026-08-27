package bucket

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/bucket/filesystem"
	"github.com/grafana/loki/v3/pkg/storage/bucket/s3"
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
		}, "test", hedging.Config{}, log.NewNopLogger())
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

func TestObjectClientAdapter_IsBackendFilesystem(t *testing.T) {
	// A filesystem-backed adapter must report true so that callers select the
	// FSEncoder for chunk keys (see compactor chunk client setup).
	client, err := NewObjectClient(context.Background(), "filesystem", ConfigWithNamedStores{
		Config: Config{
			Filesystem: filesystem.Config{Directory: t.TempDir()},
		},
	}, "test", hedging.Config{}, log.NewNopLogger())
	require.NoError(t, err)
	require.True(t, client.IsBackendFilesystem())

	// Non-filesystem backends must report false.
	require.False(t, (&ObjectClientAdapter{storeType: S3}).IsBackendFilesystem())
	require.False(t, (&ObjectClientAdapter{storeType: GCS}).IsBackendFilesystem())
}

func TestObjectClientAdapter_ClientReusesConnections(t *testing.T) {
	// We are going to make multiple rounds of calls to a fake S3, and expect that
	// connections opened by the first round should be reused by every later round.
	const (
		concurrency = 8
		rounds      = 6
	)

	// Fake S3 responses
	payload := []byte("some-object-contents")
	var newConns atomic.Int64
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		w.Header().Set("ETag", `"d0be2dc421be4fcd0172e5afceea3970"`)
		w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(payload)
		}
	}))
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			newConns.Add(1)
		}
	}
	srv.Start()
	t.Cleanup(srv.Close)

	var cfg Config
	flagext.DefaultValues(&cfg)
	cfg.S3.Endpoint = strings.TrimPrefix(srv.URL, "http://")
	cfg.S3.BucketName = "test-bucket"
	cfg.S3.Region = "us-east-1"
	cfg.S3.AccessKeyID = "access-key"
	cfg.S3.SecretAccessKey = flagext.SecretWithValue("secret-key")
	cfg.S3.Insecure = true
	cfg.S3.MaxRetries = 1

	require.GreaterOrEqual(t, cfg.S3.HTTP.MaxIdleConnsPerHost, concurrency,
		"test is only meaningful if the configured pool can hold every connection we open")

	// Configure hedging on, since the bug where this went wrong only happened when hedging was enabled.
	// However we don't actually want any hedged requests so use At=time.Hour.
	client, err := NewObjectClient(context.Background(), S3, ConfigWithNamedStores{Config: cfg}, "test",
		hedging.Config{At: time.Hour, UpTo: 2, MaxPerSecond: 100}, log.NewNopLogger())
	require.NoError(t, err)

	for range rounds {
		var wg sync.WaitGroup
		for range concurrency {
			wg.Go(func() {
				rc, _, err := client.GetObject(context.Background(), "some-key")
				if !assert.NoError(t, err) {
					return
				}
				defer rc.Close()

				// Must read the whole body to enable connection pooling.
				got, err := io.ReadAll(rc)
				assert.NoError(t, err)
				assert.Equal(t, payload, got)
			})
		}
		wg.Wait()
	}

	// Allow some leeway, up to 2x what should be needed.
	require.Lessf(t, newConns.Load(), int64(concurrency*2),
		"opened %d connections to serve %d requests: the client is not reusing connections",
		newConns.Load(), concurrency*rounds)
}

// TestObjectClientAdapter_IsRetryableErr_S3Minio locks the wiring that an S3
// backend recognises minio-go throttling errors. The thanos-objstore S3 client
// is backed by minio-go and returns minio.ErrorResponse (not smithy.APIError);
// if these are not treated as retryable, congestion control never backs off or
// retries and S3 throttling surfaces immediately as failed downloads.
func TestObjectClientAdapter_IsRetryableErr_S3Minio(t *testing.T) {
	// A fake endpoint is fine: the client is only constructed here, never used to
	// issue a request, so no network access occurs.
	c, err := NewObjectClient(context.Background(), S3, ConfigWithNamedStores{
		Config: Config{
			S3: s3.Config{
				Endpoint:   "localhost:9000",
				BucketName: "test",
			},
		},
	}, "test", hedging.Config{}, log.NewNopLogger())
	require.NoError(t, err)

	require.True(t, c.IsRetryableErr(minio.ErrorResponse{Code: "SlowDown", StatusCode: http.StatusServiceUnavailable}))
	require.True(t, c.IsRetryableErr(fmt.Errorf("failed to load chunk: %w", minio.ErrorResponse{Code: "SlowDown", StatusCode: http.StatusServiceUnavailable})))
	require.False(t, c.IsRetryableErr(minio.ErrorResponse{Code: minio.NoSuchKey, StatusCode: http.StatusNotFound}))
}
