package client

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

// ObjectClient is used to store arbitrary data in Object Store (S3/GCS/Azure/...)
type ObjectClient interface {
	ObjectExists(ctx context.Context, objectKey string) (bool, error)

	PutObject(ctx context.Context, objectKey string, object io.Reader) error
	// NOTE: The consumer of GetObject should always call the Close method when it is done reading which otherwise could cause a resource leak.
	GetObject(ctx context.Context, objectKey string) (io.ReadCloser, int64, error)
	GetObjectRange(ctx context.Context, objectKey string, off, length int64) (io.ReadCloser, error)

	// List objects with given prefix.
	//
	// If delimiter is empty, all objects are returned, even if they are in nested in "subdirectories".
	// If delimiter is not empty, it is used to compute common prefixes ("subdirectories"),
	// and objects containing delimiter in the name will not be returned in the result.
	//
	// For example, if the prefix is "notes/" and the delimiter is a slash (/) as in "notes/summer/july", the common prefix is "notes/summer/".
	// Common prefixes will always end with passed delimiter.
	//
	// Keys of returned storage objects have given prefix.
	List(ctx context.Context, prefix string, delimiter string) ([]StorageObject, []StorageCommonPrefix, error)
	DeleteObject(ctx context.Context, objectKey string) error
	IsObjectNotFoundErr(err error) bool
	IsRetryableErr(err error) bool
	Stop()
}

// StorageObject represents an object being stored in an Object Store
type StorageObject struct {
	Key        string
	ModifiedAt time.Time
}

// StorageCommonPrefix represents a common prefix aka a synthetic directory in Object Store.
// It is guaranteed to always end with delimiter passed to List method.
type StorageCommonPrefix string

// KeyEncoder is used to encode chunk keys before writing/retrieving chunks
// from the underlying ObjectClient
// Schema/Chunk are passed as arguments to allow this to improve over revisions
type KeyEncoder func(schema config.SchemaConfig, chk chunk.Chunk) string

// base64Encoder is used to encode chunk keys in base64 before storing/retrieving
// them from the ObjectClient
var base64Encoder = func(key string) string {
	return base64.StdEncoding.EncodeToString([]byte(key))
}

var FSEncoder = func(schema config.SchemaConfig, chk chunk.Chunk) string {
	// Filesystem encoder pre-v12 encodes the chunk as one base64 string.
	// This has the downside of making them opaque and storing all chunks in a single
	// directory, hurting performance at scale and discoverability.
	// Post v12, we respect the directory structure imposed by chunk keys.
	key := schema.ExternalKey(chk.ChunkRef)
	if schema.VersionForChunk(chk.ChunkRef) > 11 {
		split := strings.LastIndexByte(key, '/')
		encodedTail := base64Encoder(key[split+1:])
		return strings.Join([]string{key[:split], encodedTail}, "/")

	}
	return base64Encoder(key)
}

const defaultMaxParallel = 150

// client is used to store chunks in object store backends
type client struct {
	store               ObjectClient
	keyEncoder          KeyEncoder
	getChunkMaxParallel int
	schema              config.SchemaConfig
}

// NewClient wraps the provided ObjectClient with a chunk.Client implementation
func NewClient(store ObjectClient, encoder KeyEncoder, schema config.SchemaConfig) Client {
	return NewClientWithMaxParallel(store, encoder, defaultMaxParallel, schema)
}

func NewClientWithMaxParallel(store ObjectClient, encoder KeyEncoder, maxParallel int, schema config.SchemaConfig) Client {
	return &client{
		store:               store,
		keyEncoder:          encoder,
		getChunkMaxParallel: maxParallel,
		schema:              schema,
	}
}

// Stop shuts down the object store and any underlying clients
func (o *client) Stop() {
	o.store.Stop()
}

// PutChunks stores the provided chunks in the configured backend. If multiple errors are
// returned, the last one sequentially will be propagated up.
func (o *client) PutChunks(ctx context.Context, chunks []chunk.Chunk) error {
	var (
		chunkKeys []string
		chunkBufs [][]byte
	)

	for i := range chunks {
		buf, err := chunks[i].Encoded()
		if err != nil {
			return err
		}

		var key string
		if o.keyEncoder != nil {
			key = o.keyEncoder(o.schema, chunks[i])
		} else {
			key = o.schema.ExternalKey(chunks[i].ChunkRef)
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
func (o *client) GetChunks(ctx context.Context, chunks []chunk.Chunk) ([]chunk.Chunk, error) {
	getChunkMaxParallel := o.getChunkMaxParallel
	if getChunkMaxParallel == 0 {
		getChunkMaxParallel = defaultMaxParallel
	}
	return util.GetParallelChunks(ctx, getChunkMaxParallel, chunks, o.getChunk)
}

func (o *client) getChunk(ctx context.Context, decodeContext *chunk.DecodeContext, c chunk.Chunk) (chunk.Chunk, error) {
	if ctx.Err() != nil {
		return chunk.Chunk{}, ctx.Err()
	}

	key := o.schema.ExternalKey(c.ChunkRef)
	if o.keyEncoder != nil {
		key = o.keyEncoder(o.schema, c)
	}

	readCloser, size, err := o.store.GetObject(ctx, key)
	if err != nil {
		return chunk.Chunk{}, errors.WithStack(errors.Wrapf(err, "failed to load chunk '%s'", key))
	}

	if readCloser == nil {
		return chunk.Chunk{}, errors.New("object client getChunk fail because object is nil")
	}
	defer readCloser.Close()

	// adds bytes.MinRead to avoid allocations when the size is known.
	// This is because ReadFrom reads bytes.MinRead by bytes.MinRead.
	buf := bytes.NewBuffer(make([]byte, 0, size+bytes.MinRead))
	_, err = buf.ReadFrom(readCloser)
	if err != nil {
		return chunk.Chunk{}, errors.WithStack(err)
	}

	if err := c.Decode(decodeContext, buf.Bytes()); err != nil {
		return chunk.Chunk{}, errors.WithStack(
			fmt.Errorf(
				"failed to decode chunk '%s' for tenant `%s`: %w",
				key,
				c.ChunkRef.UserID,
				err,
			),
		)
	}
	return c, nil
}

// GetChunks retrieves the specified chunks from the configured backend
func (o *client) DeleteChunk(ctx context.Context, userID, chunkID string) error {
	key := chunkID
	if o.keyEncoder != nil {
		c, err := chunk.ParseExternalKey(userID, key)
		if err != nil {
			return err
		}
		key = o.keyEncoder(o.schema, c)
	}
	return o.store.DeleteObject(ctx, key)
}

func (o *client) IsChunkNotFoundErr(err error) bool {
	return o.store.IsObjectNotFoundErr(err)
}

func (o *client) IsRetryableErr(err error) bool {
	return o.store.IsRetryableErr(err)
}
