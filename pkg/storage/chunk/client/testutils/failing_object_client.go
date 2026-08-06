package testutils

import (
	"context"
	"io"
	"sync"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
)

// FailingObjectClient injects chunk fetch failures for chosen object keys and
// delegates everything else to the client it wraps.
//
// Keys are the ones the chunk client asks for, which is
// schema.ExternalKey(chunk.ChunkRef) while its key encoder is nil. Every caller
// here builds the client with NewClientWithMaxParallel(store, nil, ...).
type FailingObjectClient struct {
	client.ObjectClient

	mu       sync.Mutex
	failures map[string]Failure
}

// Failure describes what happens to one key. The three fields model three
// distinct production shapes and can be combined on the same key.
type Failure struct {
	// Err is returned from GetObject before any bytes are handed out.
	Err error
	// TruncateAfter above zero yields that many bytes and then
	// io.ErrUnexpectedEOF, which is what a body dying mid-read looks like.
	TruncateAfter int
	// BlockUntil holds GetObject until the channel is closed or the context ends.
	BlockUntil chan struct{}
}

func NewFailingObjectClient(inner client.ObjectClient) *FailingObjectClient {
	return &FailingObjectClient{
		ObjectClient: inner,
		failures:     map[string]Failure{},
	}
}

// Fail makes GetObject return err for each of keys.
func (f *FailingObjectClient) Fail(err error, keys ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, key := range keys {
		failure := f.failures[key]
		failure.Err = err
		f.failures[key] = failure
	}
}

// Truncate makes key yield afterBytes bytes and then io.ErrUnexpectedEOF.
func (f *FailingObjectClient) Truncate(key string, afterBytes int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	failure := f.failures[key]
	failure.TruncateAfter = afterBytes
	f.failures[key] = failure
}

// Block holds GetObject for key until the returned channel is closed, or until
// the caller's context ends. It lets a test reach a known point without sleeping.
func (f *FailingObjectClient) Block(key string) chan struct{} {
	release := make(chan struct{})

	f.mu.Lock()
	defer f.mu.Unlock()

	failure := f.failures[key]
	failure.BlockUntil = release
	f.failures[key] = failure

	return release
}

func (f *FailingObjectClient) GetObject(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	// GetParallelChunks calls this from many goroutines at once, so the map must
	// not be held while a blocked key waits.
	f.mu.Lock()
	failure, injected := f.failures[key]
	f.mu.Unlock()

	if !injected {
		return f.ObjectClient.GetObject(ctx, key)
	}

	if failure.BlockUntil != nil {
		select {
		case <-failure.BlockUntil:
		case <-ctx.Done():
			// Unwrapped, so errors.Is still finds it once getChunk has wrapped it.
			return nil, 0, ctx.Err()
		}
	}

	if failure.Err != nil {
		return nil, 0, failure.Err
	}

	body, size, err := f.ObjectClient.GetObject(ctx, key)
	if err != nil || failure.TruncateAfter <= 0 {
		return body, size, err
	}
	return &truncatedBody{inner: body, remaining: failure.TruncateAfter}, size, nil
}

// truncatedBody models a body that stops arriving part way through. The size
// reported by GetObject is left alone, because a real short read is exactly a
// body that does not match its own content length.
type truncatedBody struct {
	inner     io.ReadCloser
	remaining int
}

func (t *truncatedBody) Read(p []byte) (int, error) {
	if t.remaining <= 0 {
		return 0, io.ErrUnexpectedEOF
	}
	if len(p) > t.remaining {
		p = p[:t.remaining]
	}
	n, err := t.inner.Read(p)
	t.remaining -= n
	return n, err
}

func (t *truncatedBody) Close() error {
	return t.inner.Close()
}
