package lz4stream

import (
	"encoding/binary"
	"fmt"
	"io"
	"sync"

	"github.com/pierrec/lz4/v4/internal/lz4block"
	"github.com/pierrec/lz4/v4/internal/lz4errors"
	"github.com/pierrec/lz4/v4/internal/xxh32"
)

type Blocks struct {
	Block  *FrameDataBlock
	Blocks chan chan *FrameDataBlock
	reader *asyncReader // in flight if concurrency > 1, nil otherwise
	err    error
}

// asyncReader reads one frame with concurrency > 1. A Reset can
// abandon it mid-stream; it has its own error and its own copy of the
// frame so that it cannot affect the next read.
type asyncReader struct {
	mu    sync.Mutex
	err   error
	data  chan []byte // uncompressed blocks, in order
	frame Frame       // copy of the frame: an abandoned reader must not touch the next frame's scratch space, flags, or checksum
}

// fail keeps the first error; the read loop stops once one is set.
func (r *asyncReader) fail(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err == nil {
		r.err = err
	}
}

func (r *asyncReader) error() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.err
}

// drain reads everything the reader produces so that its goroutines exit,
// returning buffers to the pool.
func (r *asyncReader) drain() {
	for buf := range r.data {
		lz4block.Put(buf)
	}
}

var errAbandoned = lz4errors.Error("lz4: concurrent read abandoned")

func (b *Blocks) initW(f *Frame, dst io.Writer, num int) {
	if num == 1 {
		b.Blocks = nil
		b.Block = b.Block.init(f)
		return
	}
	b.Block = nil
	if cap(b.Blocks) != num {
		b.Blocks = make(chan chan *FrameDataBlock, num)
	}
	// goroutine managing concurrent block compression goroutines.
	go func() {
		// Process next block compression item.
		for c := range b.Blocks {
			// Read the next compressed block result.
			// Waiting here ensures that the blocks are output in the order they were sent.
			// The incoming channel is always closed as it indicates to the caller that
			// the block has been processed.
			block := <-c
			if block == nil {
				// Notify the block compression routine that we are done with its result.
				// This is used when a sentinel block is sent to terminate the compression.
				close(c)
				return
			}
			// Do not attempt to write the block upon any previous failure.
			if b.err == nil {
				// Write the block.
				if err := block.Write(f, dst); err != nil {
					// Keep the first error.
					b.err = err
					// All pending compression goroutines need to shut down, so we need to keep going.
				}
			}
			close(c)
		}
	}()
}

func (b *Blocks) close(f *Frame, num int) error {
	if r := b.reader; r != nil {
		// abandon the read: fail stops the read loop at its next block,
		// drain unblocks the goroutines and returns their buffers
		b.reader = nil
		r.fail(errAbandoned)
		go r.drain()
	}
	if num == 1 {
		if b.Block != nil {
			b.Block.Close(f)
		}
		err := b.err
		b.err = nil
		return err
	}
	if b.Blocks == nil {
		err := b.err
		b.err = nil
		return err
	}
	c := make(chan *FrameDataBlock)
	b.Blocks <- c
	c <- nil
	<-c
	b.Blocks = nil // ensure a second close from Reset does not block
	err := b.err
	b.err = nil
	return err
}

// ErrorR returns any error set while uncompressing a stream.
func (b *Blocks) ErrorR() error {
	if b.reader == nil {
		return nil
	}
	return b.reader.error()
}

// initR returns a channel that streams the uncompressed blocks if in concurrent
// mode and no error. When the channel is closed, check for any error with b.ErrorR.
//
// If not in concurrent mode, the uncompressed block is b.Block and the returned error
// needs to be checked.
func (b *Blocks) initR(f *Frame, num int, src io.Reader) (chan []byte, error) {
	size := f.BlockSizeIndex()
	if num == 1 {
		b.Blocks = nil
		b.Block = b.Block.init(f)
		return nil, nil
	}
	b.Block = nil
	blocks := make(chan chan []byte, num)
	// data receives the uncompressed blocks.
	data := make(chan []byte)
	r := &asyncReader{data: data, frame: *f}
	r.frame.Blocks = Blocks{}
	b.reader = r
	f = &r.frame
	// Read blocks from the source sequentially
	// and uncompress them concurrently.

	// In legacy mode, accrue the uncompress sizes in cum.
	var cum uint32
	go func() {
		var cumx uint32
		var err error
		for r.error() == nil {
			block := NewFrameDataBlock(f)
			cumx, err = block.Read(f, src, 0)
			if err != nil {
				block.Close(f)
				break
			}
			// Recheck for an error as reading may be slow and uncompressing is expensive.
			if r.error() != nil {
				block.Close(f)
				break
			}
			c := make(chan []byte)
			blocks <- c
			go func() {
				defer block.Close(f)
				data, err := block.Uncompress(f, size.Get(), nil, false)
				if err != nil {
					r.fail(err)
					// Close the block channel to indicate an error.
					close(c)
				} else {
					c <- data
				}
			}()
		}
		// End the collection loop and the data channel.
		c := make(chan []byte)
		blocks <- c
		c <- nil // signal the collection loop that we are done
		<-c      // wait for the collect loop to complete
		if f.isLegacy() && cum == cumx {
			err = lz4errors.ErrEndOfStream
		}
		r.fail(err)
		close(data)
	}()
	// Collect the uncompressed blocks and make them available
	// on the returned channel.
	go func(leg bool) {
		defer close(blocks)
		skipBlocks := false
		for c := range blocks {
			buf, ok := <-c
			if !ok {
				// A closed channel indicates an error.
				// All remaining channels should be discarded.
				skipBlocks = true
				continue
			}
			if buf == nil {
				// Signal to end the loop.
				close(c)
				return
			}
			if skipBlocks {
				// A previous error has occurred, skipping remaining channels.
				continue
			}
			// Perform checksum now as the blocks are received in order.
			if f.Descriptor.Flags.ContentChecksum() {
				_, _ = f.checksum.Write(buf)
			}
			if leg {
				cum += uint32(len(buf))
			}
			data <- buf
			close(c)
		}
	}(f.isLegacy())
	return data, nil
}

func NewFrameDataBlock(f *Frame) *FrameDataBlock {
	return (*FrameDataBlock)(nil).init(f)
}

// init readies b for a new frame, allocating it if nil. In non concurrent
// mode, the same block is reused across resets.
func (b *FrameDataBlock) init(f *Frame) *FrameDataBlock {
	if b == nil {
		b = new(FrameDataBlock)
	}
	b.Close(f) // return any buffer still held; noop if already closed
	buf := f.BlockSizeIndex().Get()
	b.Data = buf
	b.data = buf
	return b
}

type FrameDataBlock struct {
	Size     DataBlockSize
	Data     []byte // compressed or uncompressed data (.data or .src)
	Checksum uint32
	data     []byte // buffer for compressed data
	src      []byte // uncompressed data
	err      error  // used in concurrent mode
}

func (b *FrameDataBlock) Close(f *Frame) {
	b.Size = 0
	b.Checksum = 0
	b.err = nil
	if b.data != nil {
		// Block was not already closed.
		lz4block.Put(b.data)
		b.Data = nil
		b.data = nil
		b.src = nil
	}
}

// Block compression errors are ignored since the buffer is sized appropriately.
func (b *FrameDataBlock) Compress(f *Frame, src []byte, level lz4block.CompressionLevel) *FrameDataBlock {
	data := b.data
	if f.isLegacy() {
		data = data[:cap(data)]
	} else {
		data = data[:len(src)] // trigger the incompressible flag in CompressBlock
	}
	var n int
	switch level {
	case lz4block.Fast:
		n, _ = lz4block.CompressBlock(src, data)
	default:
		n, _ = lz4block.CompressBlockHC(src, data, level)
	}
	if n == 0 {
		b.Size.UncompressedSet(true)
		b.Data = src
	} else {
		b.Size.UncompressedSet(false)
		b.Data = data[:n]
	}
	b.Size.sizeSet(len(b.Data))
	b.src = src // keep track of the source for content checksum

	if !f.isLegacy() && f.Descriptor.Flags.BlockChecksum() {
		b.Checksum = xxh32.ChecksumZero(b.Data)
	}
	return b
}

func (b *FrameDataBlock) Write(f *Frame, dst io.Writer) error {
	// Write is called in the same order as blocks are compressed,
	// so content checksum must be done here.
	if f.Descriptor.Flags.ContentChecksum() {
		_, _ = f.checksum.Write(b.src)
	}
	buf := f.buf[:]
	binary.LittleEndian.PutUint32(buf, uint32(b.Size))
	if _, err := dst.Write(buf[:4]); err != nil {
		return err
	}

	if _, err := dst.Write(b.Data); err != nil {
		return err
	}

	if f.isLegacy() || !f.Descriptor.Flags.BlockChecksum() { // legacy frames have no block checksums
		return nil
	}
	binary.LittleEndian.PutUint32(buf, b.Checksum)
	_, err := dst.Write(buf[:4])
	return err
}

// Read updates b with the next block data, size and checksum if available.
func (b *FrameDataBlock) Read(f *Frame, src io.Reader, cum uint32) (uint32, error) {
	x, err := f.readUint32(src)
	if err != nil {
		return 0, err
	}
	if f.isLegacy() {
		switch x {
		case frameMagicLegacy:
			// Concatenated legacy frame.
			return b.Read(f, src, cum)
		case cum:
			// Only works in non concurrent mode, for concurrent mode
			// it is handled separately.
			// Linux kernel format appends the total uncompressed size at the end.
			return 0, lz4errors.ErrEndOfStream
		}
	} else if x == 0 {
		// Marker for end of stream.
		return 0, lz4errors.ErrEndOfStream
	}
	b.Size = DataBlockSize(x)

	size := b.Size.size()
	if size > cap(b.data) {
		return x, lz4errors.ErrOptionInvalidBlockSize
	}
	b.data = b.data[:size]
	if _, err := io.ReadFull(src, b.data); err != nil {
		return x, err
	}
	if f.Descriptor.Flags.BlockChecksum() {
		sum, err := f.readUint32(src)
		if err != nil {
			return 0, err
		}
		b.Checksum = sum
	}
	return x, nil
}

func (b *FrameDataBlock) Uncompress(f *Frame, dst, dict []byte, sum bool) ([]byte, error) {
	if b.Size.Uncompressed() {
		n := copy(dst, b.data)
		dst = dst[:n]
	} else {
		n, err := lz4block.UncompressBlock(b.data, dst, dict)
		if err != nil {
			return nil, err
		}
		dst = dst[:n]
	}
	if f.Descriptor.Flags.BlockChecksum() {
		if c := xxh32.ChecksumZero(b.data); c != b.Checksum {
			err := fmt.Errorf("%w: got %x; expected %x", lz4errors.ErrInvalidBlockChecksum, c, b.Checksum)
			return nil, err
		}
	}
	if sum && f.Descriptor.Flags.ContentChecksum() {
		_, _ = f.checksum.Write(dst)
	}
	return dst, nil
}

func (f *Frame) readUint32(r io.Reader) (x uint32, err error) {
	if _, err = io.ReadFull(r, f.buf[:4]); err != nil {
		return
	}
	x = binary.LittleEndian.Uint32(f.buf[:4])
	return
}
