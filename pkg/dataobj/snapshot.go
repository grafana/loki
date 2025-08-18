package dataobj

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/grafana/loki/v3/pkg/scratch"
)

// A snapshot represents a complete data object where all sections are stored in
// a [scratch.Store].
//
// snapshot allows for reading the data object without materializing the entire
// object into memory. When using a disk-basked scratch store, reads are served by
// opening and reading the underlying files on demand.
type snapshot struct {
	store scratch.Store

	// Regions to read from (sorted by offset).
	regions []snapshotRegion

	header   []byte
	sections []sectionInfo
	tailer   []byte
}

type sectionInfo struct {
	Type SectionType

	Data, Metadata         scratch.Handle
	DataSize, MetadataSize int

	Tenant string // Owning tenant of the section, if any.

	// ExtensionData holds additional encoded info about the section, written to
	// the file-level metadata.
	ExtensionData []byte
}

// newSnapshot creates a new snapshot for the given data. [sectionHandle]s
// passed to the newSnapshot are owned by the snapshot, and are deleted
// by the snapshot when calling [snapshot.Close].
func newSnapshot(store scratch.Store, header []byte, sections []sectionInfo, tailer []byte) (*snapshot, error) {
	s := &snapshot{
		store:    store,
		header:   header,
		sections: sections,
		tailer:   tailer,
	}

	if err := s.initRegions(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *snapshot) initRegions() error {
	s.addRegion(int64(len(s.header)), func() (io.ReadSeekCloser, error) {
		return nopReadSeekerCloser{bytes.NewReader(s.header)}, nil
	})

	for _, section := range s.sections {
		s.addRegion(int64(section.DataSize), func() (io.ReadSeekCloser, error) { return s.store.Read(section.Data) })
		s.addRegion(int64(section.MetadataSize), func() (io.ReadSeekCloser, error) { return s.store.Read(section.Metadata) })
	}

	s.addRegion(int64(len(s.tailer)), func() (io.ReadSeekCloser, error) {
		return nopReadSeekerCloser{bytes.NewReader(s.tailer)}, nil
	})

	return nil
}

type nopReadSeekerCloser struct{ io.ReadSeeker }

func (nopReadSeekerCloser) Close() error { return nil }

func (s *snapshot) addRegion(size int64, newReader func() (io.ReadSeekCloser, error)) {
	s.regions = append(s.regions, snapshotRegion{
		offset:    s.Size(), // Use current size as offset
		length:    size,
		NewReader: newReader,
	})
}

// ReadAt implements [io.ReaderAt], returning bytes over the entire encoded
// data object.
func (s *snapshot) ReadAt(p []byte, off int64) (n int, err error) {
	if off < 0 {
		return 0, fmt.Errorf("invalid offset: %d", off)
	}

	for len(p) > 0 {
		absoluteOffset := off + int64(n)

		// Binary search to find the first region that ends after absoluteOffset.
		regionIdx := sort.Search(len(s.regions), func(i int) bool {
			return s.regions[i].offset+s.regions[i].length > absoluteOffset
		})
		if regionIdx == len(s.regions) {
			return n, io.EOF
		}

		region := s.regions[regionIdx]
		localOffset := absoluteOffset - region.offset

		// Open our region at the correct offset.
		reader, err := region.NewReader()
		if err != nil {
			return n, fmt.Errorf("creating region reader: %w", err)
		} else if _, err := reader.Seek(localOffset, io.SeekStart); err != nil {
			return n, fmt.Errorf("seeking region reader: %w", err)
		}

		readSize := min(len(p), int(region.length-localOffset))
		readCount, err := readFullAndClose(reader, p[:readSize])

		n += readCount

		if err != nil {
			return n, fmt.Errorf("reading region: %w", err)
		}

		p = p[readCount:]
	}

	return n, nil
}

func readFullAndClose(r io.ReadCloser, dst []byte) (int, error) {
	defer r.Close()
	return io.ReadFull(r, dst)
}

// Size returns the total size of the snapshot in bytes.
func (s *snapshot) Size() int64 {
	if len(s.regions) == 0 {
		return 0
	}

	lastRegion := s.regions[len(s.regions)-1]
	return lastRegion.offset + lastRegion.length
}

// Close releases resources associated with the snapsshot, removing all section
// handles from the backing scratch store.
func (s *snapshot) Close() error {
	var errs []error
	for _, section := range s.sections {
		errs = append(errs, s.store.Remove(section.Data))
		errs = append(errs, s.store.Remove(section.Metadata))
	}
	return errors.Join(errs...)
}

// snapshotRegion is a contiguous region of a snapshot's data.
type snapshotRegion struct {
	offset int64
	length int64

	// NewReader attempts to open a reader for the region's data.
	NewReader func() (io.ReadSeekCloser, error)
}
