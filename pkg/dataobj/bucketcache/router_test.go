package bucketcache

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/metadatacache"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

var (
	logsObject  = "objects/a"
	indexObject = metastore.IndexStoragePrefix + "/indexes/a"
	tocObject   = metastore.IndexStoragePrefix + "/" + metastore.TableOfContentsPath("tenant", time.Unix(0, 0).UTC())
)

func TestRouter(t *testing.T) {
	t.Run("reads each kind of object through its own cache and counts it under its own kind label", func(t *testing.T) {
		f := newRouterFixture(t, true, true)

		f.read(t, logsObject, 500, 1500)
		f.read(t, indexObject, 0, 1000)

		require.Len(t, f.logs.subranges.GetKeys(), 2)
		require.Len(t, f.index.subranges.GetKeys(), 1)
		require.Len(t, f.logs.attributes.GetKeys(), 1)
		require.Len(t, f.index.attributes.GetKeys(), 1)
		require.Equal(t, 2.0, f.counter(t, "loki_dataobj_bucket_cache_requested_subranges_total", kindLogs))
		require.Equal(t, 1.0, f.counter(t, "loki_dataobj_bucket_cache_requested_subranges_total", kindIndex))
	})

	t.Run("serves the attributes of each kind of object from its own cache", func(t *testing.T) {
		f := newRouterFixture(t, true, true)

		for range 2 {
			for _, name := range []string{logsObject, indexObject} {
				got, err := f.router.Attributes(t.Context(), name)
				require.NoError(t, err)
				require.Equal(t, int64(len(f.content)), got.Size)
			}
		}

		require.Equal(t, int64(2), f.inner.attributes.Load(), "the bucket answers the first lookup of each object only")
		require.Len(t, f.logs.attributes.GetKeys(), 1)
		require.Len(t, f.index.attributes.GetKeys(), 1)
	})

	t.Run("passes the table of contents to the bucket and never caches it", func(t *testing.T) {
		f := newRouterFixture(t, true, true)

		f.read(t, tocObject, 0, 1000)
		_, err := f.router.Attributes(t.Context(), tocObject)
		require.NoError(t, err)
		_, err = f.router.Attributes(t.Context(), tocObject)
		require.NoError(t, err)

		require.Equal(t, []span{{offset: 0, length: 1000}}, f.inner.spans())
		require.Equal(t, int64(2), f.inner.attributes.Load())
		require.Empty(t, f.logs.subranges.GetKeys())
		require.Empty(t, f.index.subranges.GetKeys())
		require.Empty(t, f.logs.attributes.GetKeys())
		require.Empty(t, f.index.attributes.GetKeys())
	})

	t.Run("passes the reads of a kind without a cache to the bucket", func(t *testing.T) {
		f := newRouterFixture(t, true, false)

		got := f.read(t, indexObject, 500, 1500)
		_, err := f.router.Attributes(t.Context(), indexObject)
		require.NoError(t, err)

		require.Equal(t, f.content[500:2000], got)
		require.Equal(t, []span{{offset: 500, length: 1500}}, f.inner.spans())
		require.Equal(t, int64(1), f.inner.attributes.Load())
	})

	t.Run("returns an error when it has no backend", func(t *testing.T) {
		_, err := New(objstore.NewInMemBucket(), nil, nil, "test", prometheus.NewRegistry())

		require.EqualError(t, err, "the bucket cache needs a backend for log objects or index objects")
	})

	t.Run("returns an error when a backend has no attributes cache", func(t *testing.T) {
		backend := &Backend{Config: Config{SubrangeSize: 1000}, Subranges: cache.NewMockCache()}

		_, err := New(objstore.NewInMemBucket(), backend, nil, "test", prometheus.NewRegistry())

		require.EqualError(t, err, "creating the cache of log objects: the bucket cache needs a bucket, a subrange cache and an attributes cache")
	})
}

func TestKindOf(t *testing.T) {
	t.Run("returns logs for a log object", func(t *testing.T) {
		require.Equal(t, kindLogs, kindOf("objects/ab/cdef"))
		require.Equal(t, kindLogs, kindOf("objects/tenants/tenant/ab/cdef"))
	})

	t.Run("returns index for an index object", func(t *testing.T) {
		require.Equal(t, kindIndex, kindOf(metastore.IndexStoragePrefix+"/indexes/ab/cdef"))
		require.Equal(t, kindIndex, kindOf(metastore.IndexStoragePrefix+"/indexes/tenants/tenant/ab/cdef"))
	})

	t.Run("returns no kind for the table of contents and for any other object", func(t *testing.T) {
		require.Empty(t, kindOf(tocObject))
		require.Empty(t, kindOf("other/ab/cdef"))
	})
}

type routerFixture struct {
	router  *Router
	inner   *recordingBucket
	reg     *prometheus.Registry
	logs    kindCaches
	index   kindCaches
	content []byte
}

// kindCaches are the cache backends of one kind in a routerFixture.
type kindCaches struct {
	subranges  cache.MockCache
	attributes cache.MockCache
}

// newRouterFixture returns a Router over a bucket that holds a log object, an index object and a table of
// contents, each 3000 bytes long. The caches have subranges of 1000 bytes.
func newRouterFixture(t *testing.T, withLogs, withIndex bool) *routerFixture {
	t.Helper()

	content := make([]byte, 3000)
	for i := range content {
		content[i] = byte(i*31 + i/251)
	}
	mem := objstore.NewInMemBucket()
	for _, name := range []string{logsObject, indexObject, tocObject} {
		require.NoError(t, mem.Upload(t.Context(), name, bytes.NewReader(content)))
	}

	f := &routerFixture{
		inner:   &recordingBucket{Bucket: mem},
		reg:     prometheus.NewRegistry(),
		logs:    kindCaches{subranges: cache.NewMockCache(), attributes: cache.NewMockCache()},
		index:   kindCaches{subranges: cache.NewMockCache(), attributes: cache.NewMockCache()},
		content: content,
	}
	backend := func(c kindCaches) *Backend {
		attributes := metadatacache.NewCache(c.attributes, 0, metadatacache.KindObjectAttributes, metadatacache.NewMetrics(prometheus.NewRegistry()), nil)
		return &Backend{Config: Config{SubrangeSize: 1000}, Subranges: c.subranges, Attributes: attributes}
	}

	var logs, index *Backend
	if withLogs {
		logs = backend(f.logs)
	}
	if withIndex {
		index = backend(f.index)
	}
	var err error
	f.router, err = New(f.inner, logs, index, "test", f.reg)
	require.NoError(t, err)
	return f
}

func (f *routerFixture) read(t *testing.T, name string, offset, length int64) []byte {
	t.Helper()

	rc, err := f.router.GetRange(t.Context(), name, offset, length)
	require.NoError(t, err)
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	return got
}

// counter returns the value of a counter that has no label but kind.
func (f *routerFixture) counter(t *testing.T, name, kind string) float64 {
	t.Helper()

	families, err := f.reg.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if label.GetName() == "kind" && label.GetValue() == kind {
					return metric.GetCounter().GetValue()
				}
			}
		}
	}
	require.Failf(t, "counter not found", "%s with kind %q", name, kind)
	return 0
}
