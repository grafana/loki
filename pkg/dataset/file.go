package dataset

import (
	"context"
	"fmt"
	"io"
	"math"
	"sort"

	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/dataset/encoding"
	"github.com/grafana/loki/v3/pkg/memory"
)

// File is a region-based virtual reader over a serialized dataset.
type File struct {
	regions []fileRegion
	size    int64
}

var (
	_ encoding.RangeReader = (*File)(nil)
	_ io.ReaderAt          = (*File)(nil)
)

type fileRegion struct {
	offset int64
	length int64
	read   func(ctx context.Context, p []byte, off int64) (int, error)
}

func newFile(header []byte, postings []encoding.BufferPosting, source buffer.Source) (*File, error) {
	var (
		headerLength = int64(len(header))
		regions      = make([]fileRegion, 0, len(postings)+2)
	)

	regions = append(regions, fileRegion{
		offset: 0,
		length: headerLength,
		read:   bytesRegionReader(header),
	})

	var bodyLength int64
	for _, posting := range postings {
		if posting.Offset != bodyLength {
			return nil, fmt.Errorf("buffer %d has offset %d, expected %d", posting.BufferID, posting.Offset, bodyLength)
		}
		if posting.Length < 0 || posting.Offset > math.MaxInt64-posting.Length {
			return nil, fmt.Errorf("buffer %d has invalid offset %d and length %d", posting.BufferID, posting.Offset, posting.Length)
		}
		bodyLength += posting.Length
		if posting.Length == 0 {
			continue
		}
		if headerLength > math.MaxInt64-posting.Offset {
			return nil, fmt.Errorf("buffer %d file offset overflows int64", posting.BufferID)
		}
		regions = append(regions, fileRegion{
			offset: headerLength + posting.Offset,
			length: posting.Length,
			read:   bufferRegionReader(source, posting.BufferID),
		})
	}

	if headerLength > math.MaxInt64-bodyLength-fileTrailerSize {
		return nil, fmt.Errorf("file size overflows int64")
	}
	trailerOffset := headerLength + bodyLength
	regions = append(regions, fileRegion{
		offset: trailerOffset,
		length: fileTrailerSize,
		read:   bytesRegionReader(fileMagic[:]),
	})

	return &File{
		regions: regions,
		size:    trailerOffset + fileTrailerSize,
	}, nil
}

// ReadRange reads len(p) bytes starting at byte offset off.
func (f *File) ReadRange(ctx context.Context, p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, fmt.Errorf("dataset: invalid offset: %d", off)
	}
	if len(p) > 0 && off >= f.size {
		return 0, io.EOF
	}

	for len(p) > 0 {
		var (
			absoluteOffset = off + int64(n)
			regionIndex    = sort.Search(len(f.regions), func(i int) bool {
				return f.regions[i].offset+f.regions[i].length > absoluteOffset
			})
		)

		if regionIndex == len(f.regions) {
			return n, io.EOF
		}

		region := f.regions[regionIndex]
		if absoluteOffset < region.offset {
			return n, fmt.Errorf("dataset: no file region at offset %d", absoluteOffset)
		}

		var (
			localOffset = absoluteOffset - region.offset
			readSize    = min(int64(len(p)), region.length-localOffset)
		)

		nn, err := region.read(ctx, p[:readSize], localOffset)
		if nn < 0 || int64(nn) > readSize {
			return n, fmt.Errorf("dataset: invalid read count %d for region read of %d bytes", nn, readSize)
		}
		n += nn
		p = p[nn:]
		if err != nil {
			return n, err
		}
		if nn == 0 {
			return n, io.ErrNoProgress
		}
	}
	return n, nil
}

// ReadAt implements [io.ReaderAt].
func (f *File) ReadAt(p []byte, off int64) (int, error) {
	return f.ReadRange(context.Background(), p, off)
}

// Len returns the total size of the file in bytes.
func (f *File) Len() int64 { return f.size }

func bytesRegionReader(data []byte) func(context.Context, []byte, int64) (int, error) {
	return func(_ context.Context, p []byte, off int64) (int, error) {
		if off < 0 {
			return 0, fmt.Errorf("invalid offset: %d", off)
		}
		if off >= int64(len(data)) {
			return 0, io.EOF
		}
		n := copy(p, data[off:])
		if n < len(p) {
			return n, io.EOF
		}
		return n, nil
	}
}

func bufferRegionReader(source buffer.Source, id buffer.ID) func(context.Context, []byte, int64) (int, error) {
	return func(ctx context.Context, p []byte, off int64) (int, error) {
		var alloc memory.Allocator
		defer alloc.Free()

		data, err := source.ReadBuffers(ctx, &alloc, []buffer.ID{id})
		if err != nil {
			return 0, err
		}
		if len(data) != 1 {
			return 0, fmt.Errorf("reading buffer %d returned %d buffers", id, len(data))
		}
		return bytesRegionReader(data[0])(ctx, p, off)
	}
}
