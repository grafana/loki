package objectclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"io/ioutil"
	"reflect"
	"strings"

	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/util"
)

// KeyEncoder is used to encode chunk keys before writing/retrieving chunks
// from the underlying ObjectClient
type KeyEncoder func(string) string

// Base64Encoder is used to encode chunk keys in base64 before storing/retrieving
// them from the ObjectClient
var Base64Encoder = func(key string) string {
	return base64.StdEncoding.EncodeToString([]byte(key))
}

// TenantBase64Encoder is a variation of Base64Encoder that encodes tenant
// and remainder of the key separately and returns the results joined by "/"
var TenantBase64Encoder = func(key string) string {
	data := []byte(key)
	if i := bytes.LastIndex(data, []byte("/")); i > 0 {
		return strings.Join([]string{
			base64.URLEncoding.EncodeToString(data[:i]),
			base64.URLEncoding.EncodeToString(data[i+1:]),
		}, "/")
	} else {
		return base64.URLEncoding.EncodeToString(data)
	}
}

// Client is used to store chunks in object store backends
type Client struct {
	store      chunk.ObjectClient
	keyEncoder KeyEncoder
}

// NewClient wraps the provided ObjectClient with a chunk.Client implementation
func NewClient(store chunk.ObjectClient) *Client {
	var encoder KeyEncoder = nil
	// check if store provides a KeyEncoder
	var ok bool
	method := reflect.ValueOf(&store).MethodByName("KeyEncoder")
	if method.IsValid() {
		for _, v := range method.Call([]reflect.Value{}) {
			if encoder, ok = v.Interface().(KeyEncoder); !ok {
				encoder = nil
			}
		}
	}

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

func (o *Client) IsChunkNotFoundErr(err error) bool {
	return o.store.IsObjectNotFoundErr(err)
}
