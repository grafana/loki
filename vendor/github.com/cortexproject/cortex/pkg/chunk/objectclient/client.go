package objectclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"io/ioutil"

	"github.com/pkg/errors"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/util"
)

// KeyEncoder is used to encode chunk keys before writing/retrieving chunks
// from the underlying ObjectClient
type KeyEncoder func(string) string

// Base64Encoder is used to encode chunk keys in base64 before storing/retrieving
// them from the ObjectClient
var Base64Encoder = func(key string) string {
	return base64.StdEncoding.EncodeToString([]byte(key))
}

// Client is used to store chunks in object store backends
type Client struct {
	store      chunk.ObjectClient
	keyEncoder KeyEncoder
}

// NewClient wraps the provided ObjectClient with a chunk.Client implementation
func NewClient(store chunk.ObjectClient, encoder KeyEncoder) *Client {
	return &Client{
		store:      store,
		keyEncoder: encoder,
	}
}

// Stop shuts down the object store and any underlying clients
func (o *Client) Stop() {
	o.store.Stop()
}

// PutChunks stores the provided chunks in the configured backend. If multiple errors are
// returned, the last one sequentially will be propagated up.
func (o *Client) PutChunks(ctx context.Context, chunks []chunk.Chunk) error {
	var (
		chunkKeys []string
		chunkBufs [][]byte
	)

	for i := range chunks {
		buf, err := chunks[i].Encoded()
		if err != nil {
			return err
		}
		key := chunks[i].ExternalKey()
		if o.keyEncoder != nil {
			key = o.keyEncoder(key)
		}

		chunkKeys = append(chunkKeys, key)
		chunkBufs = append(chunkBufs, buf)
	}

	incomingErrors := make(chan error)
	for i := range chunkBufs {
		go func(i int) {
			incomingErrors <- o.store.PutObject(ctx, chunkKeys[i], bytes.NewReader(chunkBufs[i]))
		}(i)
	}

	var lastErr error
	for range chunkKeys {
		err := <-incomingErrors
		if err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// GetChunks retrieves the specified chunks from the configured backend
func (o *Client) GetChunks(ctx context.Context, chunks []chunk.Chunk) ([]chunk.Chunk, error) {
	return util.GetParallelChunks(ctx, chunks, o.getChunk)
}

func (o *Client) getChunk(ctx context.Context, decodeContext *chunk.DecodeContext, c chunk.Chunk) (chunk.Chunk, error) {
	key := c.ExternalKey()
	if o.keyEncoder != nil {
		key = o.keyEncoder(key)
	}

	readCloser, err := o.store.GetObject(ctx, key)
	if err != nil {
		return chunk.Chunk{}, errors.WithStack(err)
	}

	defer readCloser.Close()

	buf, err := ioutil.ReadAll(readCloser)
	if err != nil {
		return chunk.Chunk{}, errors.WithStack(err)
	}

	if err := c.Decode(decodeContext, buf); err != nil {
		return chunk.Chunk{}, errors.WithStack(err)
	}
	return c, nil
}

// GetChunks retrieves the specified chunks from the configured backend
func (o *Client) DeleteChunk(ctx context.Context, userID, chunkID string) error {
	key := chunkID
	if o.keyEncoder != nil {
		key = o.keyEncoder(key)
	}
	return o.store.DeleteObject(ctx, key)
}
