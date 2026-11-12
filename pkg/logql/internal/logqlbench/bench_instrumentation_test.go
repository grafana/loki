package logqlbench

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"golang.org/x/sync/errgroup"

	objectclient "github.com/grafana/loki/v3/pkg/storage/chunk/client"
)

func newTestBucket(t *testing.T, inst *instrumentation) *instrumentedBucket {
	t.Helper()
	inner := objstore.NewInMemBucket()
	for _, name := range []string{"objects/a", "index/v0/tocs/b"} {
		require.NoError(t, inner.Upload(t.Context(), name, strings.NewReader("0123456789")))
	}
	return newInstrumentedBucket(inner, inst)
}

type fakeObjectClient struct {
	objectclient.ObjectClient
}

const fakeMissingKey = "tenant/missing"

func (fakeObjectClient) GetObject(_ context.Context, key string) (io.ReadCloser, int64, error) {
	if key == fakeMissingKey {
		return nil, 0, errors.New("object not found")
	}
	return io.NopCloser(strings.NewReader("0123456789")), 10, nil
}

func (fakeObjectClient) GetObjectRange(_ context.Context, key string, _, _ int64) (io.ReadCloser, error) {
	if key == fakeMissingKey {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(strings.NewReader("0123456789")), nil
}

func TestInstrumentation(t *testing.T) {
	t.Run("the peak is the number of reads open at once", func(t *testing.T) {
		inst := &instrumentation{}
		bucket := newTestBucket(t, inst)

		var open []io.ReadCloser
		for range 8 {
			rc, err := bucket.GetRange(t.Context(), "objects/a", 0, 4)
			require.NoError(t, err)
			open = append(open, rc)
		}
		for _, rc := range open {
			require.NoError(t, rc.Close())
		}

		require.Equal(t, int64(8), inst.maxInflight.Load())
		require.Equal(t, int64(0), inst.inflight.Load())
	})

	t.Run("the peak stays at one when reads run one after another", func(t *testing.T) {
		inst := &instrumentation{}
		bucket := newTestBucket(t, inst)

		for range 5 {
			rc, err := bucket.Get(t.Context(), "objects/a")
			require.NoError(t, err)
			require.NoError(t, rc.Close())
		}

		require.Equal(t, int64(1), inst.maxInflight.Load())
		require.Equal(t, int64(5), inst.requests.Load())
	})

	t.Run("closing a body twice releases its slot once", func(t *testing.T) {
		inst := &instrumentation{}
		bucket := newTestBucket(t, inst)

		first, err := bucket.Get(t.Context(), "objects/a")
		require.NoError(t, err)
		second, err := bucket.Get(t.Context(), "objects/a")
		require.NoError(t, err)
		require.NoError(t, first.Close())
		require.NoError(t, first.Close())

		require.Equal(t, int64(1), inst.inflight.Load())
		require.NoError(t, second.Close())
	})

	t.Run("a failed read releases its slot", func(t *testing.T) {
		inst := &instrumentation{}
		bucket := newTestBucket(t, inst)

		_, err := bucket.Get(t.Context(), "objects/missing")
		require.Error(t, err)

		require.Equal(t, int64(1), inst.maxInflight.Load())
		require.Equal(t, int64(0), inst.inflight.Load())
	})

	t.Run("reset clears the peak, the in-flight count and the counters but keeps the latency", func(t *testing.T) {
		inst := &instrumentation{}
		inst.artificialLatencyNs.Store(int64(time.Millisecond))
		bucket := newTestBucket(t, inst)

		rc, err := bucket.Get(t.Context(), "objects/a")
		require.NoError(t, err)
		_, err = io.ReadAll(rc)
		require.NoError(t, err)
		// Leave this read open on purpose, so the in-flight count is not zero at reset.
		_, err = bucket.Get(t.Context(), "objects/a")
		require.NoError(t, err)
		require.NoError(t, rc.Close())
		inst.Reset()

		require.Zero(t, inst.inflight.Load())
		require.Zero(t, inst.maxInflight.Load())
		require.Zero(t, inst.requests.Load())
		require.Zero(t, inst.bytes.Load())
		require.Equal(t, int64(time.Millisecond), inst.artificialLatencyNs.Load())
	})

	t.Run("a bucket index read is neither delayed, counted nor tracked", func(t *testing.T) {
		inst := &instrumentation{}
		inst.artificialLatencyNs.Store(int64(time.Hour))
		bucket := newTestBucket(t, inst)

		for _, read := range []func() (io.ReadCloser, error){
			func() (io.ReadCloser, error) { return bucket.Get(t.Context(), "index/v0/tocs/b") },
			func() (io.ReadCloser, error) { return bucket.GetRange(t.Context(), "index/v0/tocs/b", 0, 4) },
		} {
			rc, err := read()
			require.NoError(t, err)
			require.NoError(t, rc.Close())
		}

		require.Zero(t, inst.requests.Load())
		require.Zero(t, inst.maxInflight.Load())
	})

	t.Run("an object-client index read is neither delayed, counted nor tracked", func(t *testing.T) {
		inst := &instrumentation{}
		inst.artificialLatencyNs.Store(int64(time.Hour))
		client := newInstrumentedObjectClient(fakeObjectClient{}, inst)

		rc, _, err := client.GetObject(t.Context(), "index/tsdb/x")
		require.NoError(t, err)
		require.NoError(t, rc.Close())
		rc, err = client.GetObjectRange(t.Context(), "index/tsdb/x", 0, 4)
		require.NoError(t, err)
		require.NoError(t, rc.Close())

		require.Zero(t, inst.requests.Load())
		require.Zero(t, inst.maxInflight.Load())
	})

	t.Run("a failed object-client read releases its slot", func(t *testing.T) {
		inst := &instrumentation{}
		client := newInstrumentedObjectClient(fakeObjectClient{}, inst)

		_, _, err := client.GetObject(t.Context(), fakeMissingKey)
		require.Error(t, err)
		_, err = client.GetObjectRange(t.Context(), fakeMissingKey, 0, 4)
		require.Error(t, err)

		require.Equal(t, int64(1), inst.maxInflight.Load())
		require.Equal(t, int64(0), inst.inflight.Load())
	})

	t.Run("the peak counts reads that run on several goroutines", func(t *testing.T) {
		const readers = 16
		inst := &instrumentation{}
		bucket := newTestBucket(t, inst)

		var opened, release sync.WaitGroup
		opened.Add(readers)
		release.Add(1)
		var group errgroup.Group
		for range readers {
			group.Go(func() error {
				rc, err := bucket.Get(t.Context(), "objects/a")
				opened.Done()
				if err != nil {
					return err
				}
				release.Wait()
				return rc.Close()
			})
		}
		opened.Wait()
		release.Done()
		require.NoError(t, group.Wait())

		require.Equal(t, int64(readers), inst.maxInflight.Load())
		require.Equal(t, int64(0), inst.inflight.Load())
	})

	t.Run("an object-client data read is counted and tracked", func(t *testing.T) {
		inst := &instrumentation{}
		client := newInstrumentedObjectClient(fakeObjectClient{}, inst)

		first, _, err := client.GetObject(t.Context(), "tenant/chunk")
		require.NoError(t, err)
		second, err := client.GetObjectRange(t.Context(), "tenant/chunk", 0, 4)
		require.NoError(t, err)
		require.NoError(t, first.Close())
		require.NoError(t, second.Close())

		require.Equal(t, int64(2), inst.requests.Load())
		require.Equal(t, int64(2), inst.maxInflight.Load())
	})
}
