package dataobj_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"sync"
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/storage/bucket"
	"github.com/grafana/loki/v3/pkg/storage/bucket/s3"
)

func TestObject_Reader_S3Upload(t *testing.T) {
	t.Run("uploads a small object in one PUT without a large part buffer", func(t *testing.T) {
		// An upload of unknown size makes minio-go allocate a part buffer of
		// about 528MiB. An upload of known size up to 16MiB needs no part buffer.
		const maxAllocatedBytes = 64 << 20

		srv := newFakeS3(t)
		bkt, err := bucket.NewClient(t.Context(), bucket.S3, bucket.Config{
			S3: s3.Config{
				Endpoint:        srv.addr,
				Region:          "test",
				BucketName:      "test-bucket",
				AccessKeyID:     "test",
				SecretAccessKey: flagext.SecretWithValue("test"),
				Insecure:        true,
			},
		}, "test", log.NewNopLogger(), nil)
		require.NoError(t, err)

		builder := dataobj.NewBuilder(nil)
		require.NoError(t, builder.Append(fakeSectionBuilder{
			SectionType: logsSectionType,
			FlushFunc: func(w dataobj.SectionWriter) (int64, error) {
				return w.WriteSection(nil, make([]byte, 4096), make([]byte, 1024))
			},
		}))
		obj, closer, err := builder.Flush()
		require.NoError(t, err)
		defer closer.Close()

		reader, err := obj.Reader(t.Context())
		require.NoError(t, err)
		defer reader.Close()

		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		require.NoError(t, bkt.Upload(t.Context(), "tenant/objects/test", reader))
		runtime.ReadMemStats(&after)

		require.Equal(t, []fakeS3Request{{
			Method:   http.MethodPut,
			Path:     "/test-bucket/tenant/objects/test",
			DataSize: obj.Size(),
		}}, srv.requests())
		require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(maxAllocatedBytes), "upload allocated a large part buffer")
	})
}

type fakeS3Request struct {
	Method   string
	Path     string
	Query    string
	DataSize int64
}

// fakeS3 is an S3 server that accepts single and multipart uploads and keeps
// no data. It answers multipart requests so that an upload of unknown size
// fails on the recorded requests, not on a client error.
type fakeS3 struct {
	addr string

	mu   sync.Mutex
	reqs []fakeS3Request
}

func newFakeS3(t *testing.T) *fakeS3 {
	t.Helper()

	f := &fakeS3{}
	srv := httptest.NewServer(http.HandlerFunc(f.serveHTTP))
	t.Cleanup(srv.Close)
	f.addr = srv.Listener.Addr().String()
	return f
}

func (f *fakeS3) serveHTTP(w http.ResponseWriter, r *http.Request) {
	n, _ := io.Copy(io.Discard, r.Body)

	// A body with a streaming signature holds chunk signatures around the
	// data. The header then gives the size of the data alone.
	if decoded := r.Header.Get("X-Amz-Decoded-Content-Length"); decoded != "" {
		n, _ = strconv.ParseInt(decoded, 10, 64)
	}

	f.mu.Lock()
	f.reqs = append(f.reqs, fakeS3Request{Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, DataSize: n})
	f.mu.Unlock()

	query := r.URL.Query()
	switch {
	case r.Method == http.MethodPost && query.Has("uploads"):
		fmt.Fprint(w, "<InitiateMultipartUploadResult><UploadId>upload-1</UploadId></InitiateMultipartUploadResult>")
	case r.Method == http.MethodPost && query.Has("uploadId"):
		fmt.Fprint(w, `<CompleteMultipartUploadResult><Bucket>test-bucket</Bucket><ETag>"etag"</ETag></CompleteMultipartUploadResult>`)
	default:
		w.Header().Set("ETag", `"etag"`)
	}
}

func (f *fakeS3) requests() []fakeS3Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeS3Request(nil), f.reqs...)
}
