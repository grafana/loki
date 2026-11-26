// Package lz4stream provides the types that support reading and writing LZ4 data streams.
package lz4stream

import (
	"encoding/binary"
	"fmt"
	"io"

	"github.com/pierrec/lz4/v4/internal/lz4block"
	"github.com/pierrec/lz4/v4/internal/lz4errors"
	"github.com/pierrec/lz4/v4/internal/xxh32"
)

//go:generate go run gen.go

const (
	frameMagic       uint32 = 0x184D2204
	frameSkipMagic   uint32 = 0x184D2A50
	frameMagicLegacy uint32 = 0x184C2102
)

func NewFrame() *Frame {
	return &Frame{}
}

type Frame struct {
	buf        [15]byte // frame descriptor needs at most 2(flags)+8(size)+4(dict id)+1(checksum)=15 bytes
	Magic      uint32
	Descriptor FrameDescriptor
	Blocks     Blocks
	Checksum   uint32
	checksum   xxh32.XXHZero
	size       uint64 // uncompressed bytes read or written so far, checked against Descriptor.ContentSize
}

// unexpectedEOF is for reads that must not hit the end of the source,
// because the frame is not complete yet.
func unexpectedEOF(err error) error {
	if err == io.EOF {
		return io.ErrUnexpectedEOF
	}
	return err
}

// Reset allows reusing the Frame.
// The Descriptor configuration is not modified.
func (f *Frame) Reset(num int) {
	f.Magic = 0
	f.Descriptor.Checksum = 0
	f.Descriptor.ContentSize = 0
	_ = f.Blocks.close(f, num)
	f.Checksum = 0
}

func (f *Frame) InitW(dst io.Writer, num int, legacy bool) {
	if legacy {
		f.Magic = frameMagicLegacy
	} else {
		f.Magic = frameMagic
		f.Descriptor.initW()
	}
	f.Blocks.initW(f, dst, num)
	f.checksum.Reset()
	f.size = 0
}

func (f *Frame) CloseW(dst io.Writer, num int) error {
	if err := f.Blocks.close(f, num); err != nil {
		return err
	}
	if f.isLegacy() {
		return nil
	}
	if f.Descriptor.Flags.Size() && f.size != f.Descriptor.ContentSize {
		// Leave the frame without its end mark: it is invalid anyway.
		return fmt.Errorf("%w: wrote %d; expected %d", lz4errors.ErrInvalidContentSize, f.size, f.Descriptor.ContentSize)
	}
	buf := f.buf[:0]
	// End mark (data block size of uint32(0)).
	buf = append(buf, 0, 0, 0, 0)
	if f.Descriptor.Flags.ContentChecksum() {
		buf = f.checksum.Sum(buf)
	}
	_, err := dst.Write(buf)
	return err
}

func (f *Frame) isLegacy() bool {
	return f.Magic == frameMagicLegacy
}

// BlockSizeIndex returns the block size of the frame; legacy frames
// have no descriptors and return 8Mb.
func (f *Frame) BlockSizeIndex() lz4block.BlockSizeIndex {
	if f.isLegacy() {
		return lz4block.Index(lz4block.Block8Mb)
	}
	return f.Descriptor.Flags.BlockSizeIndex()
}

func (f *Frame) ParseHeaders(src io.Reader) error {
	if f.Magic > 0 {
		// Header already read.
		return nil
	}

newFrame:
	var err error
	if f.Magic, err = f.readUint32(src); err != nil {
		return err
	}
	switch m := f.Magic; {
	case m == frameMagic || m == frameMagicLegacy:
	// All 16 values of frameSkipMagic are valid.
	case m>>8 == frameSkipMagic>>8:
		skip, err := f.readUint32(src)
		if err != nil {
			return unexpectedEOF(err)
		}
		if _, err := io.CopyN(io.Discard, src, int64(skip)); err != nil {
			return unexpectedEOF(err)
		}
		goto newFrame
	default:
		return lz4errors.ErrInvalidFrame
	}
	if err := f.Descriptor.initR(f, src); err != nil {
		return unexpectedEOF(err)
	}
	f.checksum.Reset()
	f.size = 0
	return nil
}

func (f *Frame) InitR(src io.Reader, num int) (chan []byte, error) {
	return f.Blocks.initR(f, num, src)
}

func (f *Frame) CloseR(src io.Reader) (err error) {
	if f.isLegacy() {
		return nil
	}
	read := f
	if r := f.Blocks.reader; r != nil {
		// The async reader checksums and counts on its own copy of the
		// frame. Its goroutines are done by the time its channel closes,
		// which is before the caller sees end of stream and gets here.
		read = &r.frame
	}
	if f.Descriptor.Flags.ContentChecksum() {
		if f.Checksum, err = f.readUint32(src); err != nil {
			return unexpectedEOF(err)
		}
		if c := read.checksum.Sum32(); c != f.Checksum {
			return fmt.Errorf("%w: got %x; expected %x", lz4errors.ErrInvalidFrameChecksum, c, f.Checksum)
		}
	}
	if f.Descriptor.Flags.Size() && read.size != f.Descriptor.ContentSize {
		return fmt.Errorf("%w: got %d; expected %d", lz4errors.ErrInvalidContentSize, read.size, f.Descriptor.ContentSize)
	}
	return nil
}

type FrameDescriptor struct {
	Flags       DescriptorFlags
	ContentSize uint64
	Checksum    uint8
}

func (fd *FrameDescriptor) initW() {
	fd.Flags.VersionSet(1)
	fd.Flags.BlockIndependenceSet(true)
}

func (fd *FrameDescriptor) Write(f *Frame, dst io.Writer) error {
	if fd.Checksum > 0 {
		// Header already written.
		return nil
	}

	buf := f.buf[:4]
	// Write the magic number here even though it belongs to the Frame.
	binary.LittleEndian.PutUint32(buf, f.Magic)
	if !f.isLegacy() {
		buf = buf[:4+2]
		binary.LittleEndian.PutUint16(buf[4:], uint16(fd.Flags))

		if fd.Flags.Size() {
			buf = buf[:4+2+8]
			binary.LittleEndian.PutUint64(buf[4+2:], fd.ContentSize)
		}
		fd.Checksum = descriptorChecksum(buf[4:])
		buf = append(buf, fd.Checksum)
	}

	_, err := dst.Write(buf)
	return err
}

func (fd *FrameDescriptor) initR(f *Frame, src io.Reader) error {
	if f.isLegacy() {
		fd.Flags = 0 // no descriptor to read: clear whatever the previous frame left
		return nil
	}
	// Read the flags and the checksum, hoping that there is not content size.
	buf := f.buf[:3]
	if _, err := io.ReadFull(src, buf); err != nil {
		return err
	}
	descr := binary.LittleEndian.Uint16(buf)
	fd.Flags = DescriptorFlags(descr)
	// The optional fields follow the flags, before the checksum.
	extra := 0
	if fd.Flags.Size() {
		extra += 8
	}
	if fd.Flags.dictID() {
		extra += 4
	}
	if extra > 0 {
		buf = buf[:3+extra]
		if _, err := io.ReadFull(src, buf[3:]); err != nil {
			return err
		}
	}
	if fd.Flags.Size() {
		fd.ContentSize = binary.LittleEndian.Uint64(buf[2:])
	}
	fd.Checksum = buf[len(buf)-1] // the checksum is the last byte
	buf = buf[:len(buf)-1]        // all descriptor fields except checksum
	if c := descriptorChecksum(buf); fd.Checksum != c {
		return fmt.Errorf("%w: got %x; expected %x", lz4errors.ErrInvalidHeaderChecksum, c, fd.Checksum)
	}
	// Validate the elements that can be.
	if v := fd.Flags.Version(); v != 1 {
		return fmt.Errorf("%w: version %d", lz4errors.ErrInvalidFrameDescriptor, v)
	}
	if fd.Flags&descriptorReserved != 0 {
		return fmt.Errorf("%w: reserved bits %#04x set", lz4errors.ErrInvalidFrameDescriptor, uint16(fd.Flags&descriptorReserved))
	}
	if fd.Flags.dictID() {
		// Frame dictionaries are not supported: decoding without one would
		// produce garbage.
		return fmt.Errorf("%w: dictionary ID %#x", lz4errors.ErrInvalidFrameDescriptor, binary.LittleEndian.Uint32(buf[len(buf)-4:]))
	}
	if idx := fd.Flags.BlockSizeIndex(); !idx.IsValid() {
		return lz4errors.ErrOptionInvalidBlockSize
	}
	return nil
}

// Bits of the FLG (low) and BD (high) descriptor bytes that the format
// reserves, and the dictionary ID flag, which the generated DescriptorFlags
// accessors leave out.
const (
	descriptorReserved DescriptorFlags = 1<<1 | 0xF<<8 | 1<<15
	descriptorDictID   DescriptorFlags = 1 << 0
)

func (x DescriptorFlags) dictID() bool { return x&descriptorDictID != 0 }

func descriptorChecksum(buf []byte) byte {
	return byte(xxh32.ChecksumZero(buf) >> 8)
}
