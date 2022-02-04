// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package azblob

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/internal"
	"io"
	"sync"
	"sync/atomic"

	"github.com/Azure/azure-sdk-for-go/sdk/internal/uuid"
)

// blockWriter provides methods to upload blocks that represent a file to a server and commit them.
// This allows us to provide a local implementation that fakes the server for hermetic testing.
type blockWriter interface {
	StageBlock(context.Context, string, io.ReadSeekCloser, *StageBlockOptions) (BlockBlobStageBlockResponse, error)
	CommitBlockList(context.Context, []string, *CommitBlockListOptions) (BlockBlobCommitBlockListResponse, error)
}

// copyFromReader copies a source io.Reader to blob storage using concurrent uploads.
// TODO(someone): The existing model provides a buffer size and buffer limit as limiting factors.  The buffer size is probably
// useless other than needing to be above some number, as the network stack is going to hack up the buffer over some size. The
// max buffers is providing a cap on how much memory we use (by multiplying it times the buffer size) and how many go routines can upload
// at a time.  I think having a single max memory dial would be more efficient.  We can choose an internal buffer size that works
// well, 4 MiB or 8 MiB, and auto-scale to as many goroutines within the memory limit. This gives a single dial to tweak and we can
// choose a max value for the memory setting based on internal transfers within Azure (which will give us the maximum throughput model).
// We can even provide a utility to dial this number in for customer networks to optimize their copies.
func copyFromReader(ctx context.Context, from io.Reader, to blockWriter, o UploadStreamToBlockBlobOptions) (BlockBlobCommitBlockListResponse, error) {
	if err := o.defaults(); err != nil {
		return BlockBlobCommitBlockListResponse{}, err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var err error
	generatedUuid, err := uuid.New()
	if err != nil {
		return BlockBlobCommitBlockListResponse{}, err
	}

	cp := &copier{
		ctx:    ctx,
		cancel: cancel,
		reader: from,
		to:     to,
		id:     newID(generatedUuid),
		o:      o,
		errCh:  make(chan error, 1),
	}

	// Send all our chunks until we get an error.
	for {
		if err = cp.sendChunk(); err != nil {
			break
		}
	}
	// If the error is not EOF, then we have a problem.
	if err != nil && !errors.Is(err, io.EOF) {
		return BlockBlobCommitBlockListResponse{}, err
	}

	// Close out our upload.
	if err := cp.close(); err != nil {
		return BlockBlobCommitBlockListResponse{}, err
	}

	return cp.result, nil
}

// copier streams a file via chunks in parallel from a reader representing a file.
// Do not use directly, instead use copyFromReader().
type copier struct {
	// ctx holds the context of a copier. This is normally a faux pas to store a Context in a struct. In this case,
	// the copier has the lifetime of a function call, so it's fine.
	ctx    context.Context
	cancel context.CancelFunc

	// reader is the source to be written to storage.
	reader io.Reader
	// to is the location we are writing our chunks to.
	to blockWriter

	// o contains our options for uploading.
	o UploadStreamToBlockBlobOptions

	// id provides the ids for each chunk.
	id *id

	//// num is the current chunk we are on.
	//num int32
	//// ch is used to pass the next chunk of data from our reader to one of the writers.
	//ch chan copierChunk

	// errCh is used to hold the first error from our concurrent writers.
	errCh chan error
	// wg provides a count of how many writers we are waiting to finish.
	wg sync.WaitGroup

	// result holds the final result from blob storage after we have submitted all chunks.
	result BlockBlobCommitBlockListResponse
}

type copierChunk struct {
	buffer []byte
	id     string
	length int
}

// getErr returns an error by priority. First, if a function set an error, it returns that error. Next, if the Context has an error
// it returns that error. Otherwise, it is nil. getErr supports only returning an error once per copier.
func (c *copier) getErr() error {
	select {
	case err := <-c.errCh:
		return err
	default:
	}
	return c.ctx.Err()
}

// sendChunk reads data from out internal reader, creates a chunk, and sends it to be written via a channel.
// sendChunk returns io.EOF when the reader returns an io.EOF or io.ErrUnexpectedEOF.
func (c *copier) sendChunk() error {
	if err := c.getErr(); err != nil {
		return err
	}

	buffer := c.o.TransferManager.Get()
	if len(buffer) == 0 {
		return fmt.Errorf("TransferManager returned a 0 size buffer, this is a bug in the manager")
	}

	n, err := io.ReadFull(c.reader, buffer)
	if n > 0 {
		// Some data was read, schedule the write.
		id := c.id.next()
		c.wg.Add(1)
		c.o.TransferManager.Run(
			func() {
				defer c.wg.Done()
				c.write(copierChunk{buffer: buffer, id: id, length: n})
			},
		)
	} else {
		// Return the unused buffer to the manager.
		c.o.TransferManager.Put(buffer)
	}

	if err == nil {
		return nil
	} else if err == io.EOF || err == io.ErrUnexpectedEOF {
		return io.EOF
	}

	if cerr := c.getErr(); cerr != nil {
		return cerr
	}

	return err
}

// write uploads a chunk to blob storage.
func (c *copier) write(chunk copierChunk) {
	defer c.o.TransferManager.Put(chunk.buffer)

	if err := c.ctx.Err(); err != nil {
		return
	}
	stageBlockOptions := c.o.getStageBlockOptions()
	_, err := c.to.StageBlock(c.ctx, chunk.id, internal.NopCloser(bytes.NewReader(chunk.buffer[:chunk.length])), stageBlockOptions)
	if err != nil {
		c.errCh <- fmt.Errorf("write error: %w", err)
		return
	}
}

// close commits our blocks to blob storage and closes our writer.
func (c *copier) close() error {
	if err := c.waitForFinish(); err != nil {
		return err
	}

	var err error
	commitBlockListOptions := c.o.getCommitBlockListOptions()
	c.result, err = c.to.CommitBlockList(c.ctx, c.id.issued(), commitBlockListOptions)
	return err
}

// waitForFinish waits for all writes to complete while combining errors from errCh
func (c *copier) waitForFinish() error {
	var err error
	done := make(chan struct{})
	go func() {
		// when write latencies are long, several errors might have occurred
		// drain them all as we wait for writes to complete.
		err = c.drainErrs(done)
	}()

	c.wg.Wait()
	close(done)
	return err
}

// drainErrs drains all outstanding errors from writes
func (c *copier) drainErrs(done chan struct{}) error {
	var err error
	for {
		select {
		case <-done:
			return err
		default:
			if writeErr := c.getErr(); writeErr != nil {
				err = combineErrs(err, writeErr)
			}
		}
	}
}

// combineErrs combines err with newErr so multiple errors can be represented
func combineErrs(err, newErr error) error {
	if err == nil {
		return newErr
	}
	return fmt.Errorf("%s, %w", err.Error(), newErr)
}

// id allows the creation of unique IDs based on UUID4 + an int32. This auto-increments.
type id struct {
	u   [64]byte
	num uint32
	all []string
}

// newID constructs a new id.
func newID(uu uuid.UUID) *id {
	u := [64]byte{}
	copy(u[:], uu[:])
	return &id{u: u}
}

// next returns the next ID.
func (id *id) next() string {
	defer atomic.AddUint32(&id.num, 1)

	binary.BigEndian.PutUint32(id.u[len(uuid.UUID{}):], atomic.LoadUint32(&id.num))
	str := base64.StdEncoding.EncodeToString(id.u[:])
	id.all = append(id.all, str)

	return str
}

// issued returns all ids that have been issued. This returned value shares the internal slice, so it is not safe to modify the return.
// The value is only valid until the next time next() is called.
func (id *id) issued() []string {
	return id.all
}
