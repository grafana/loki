// Package walsegment provides functionality to read and write individual WAL segment files.
// A WAL segment is a file containing a sequence of records, where each record has a 7-byte header
// followed by data. Records can be fragmented across 32KB pages but never across segment boundaries.
package tsdbwalsegment

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
)

const (
	// PageSize is the size of each page in a WAL segment (32KB)
	PageSize = 32 * 1024
	// RecordHeaderSize is the size of each record header in bytes
	RecordHeaderSize = 7
	// SegmentSize is the default size for a WAL segment (128MB)
	SegmentSize = 128 * 1024 * 1024 // (128MB)
)

// RecordType represents the type of a WAL record (low-level fragmentation)
type RecordType uint8

const (
	// RecordPageTerm indicates the rest of the page is empty
	RecordPageTerm RecordType = 0
	// RecordFull indicates a complete record that fits in the current page
	RecordFull RecordType = 1
	// RecordFirst indicates the first fragment of a record spanning multiple pages
	RecordFirst RecordType = 2
	// RecordMiddle indicates a middle fragment of a record spanning multiple pages
	RecordMiddle RecordType = 3
	// RecordLast indicates the final fragment of a record spanning multiple pages
	RecordLast RecordType = 4
)

// Compression flags
const (
	snappyMask  = 1 << 3
	zstdMask    = 1 << 4
	recTypeMask = snappyMask - 1
)

// String returns a string representation of the record type
func (rt RecordType) String() string {
	switch rt {
	case RecordPageTerm:
		return "PAGE_TERM"
	case RecordFull:
		return "FULL"
	case RecordFirst:
		return "FIRST"
	case RecordMiddle:
		return "MIDDLE"
	case RecordLast:
		return "LAST"
	default:
		return "UNKNOWN"
	}
}

// Record represents a single WAL record with its header information
type Record struct {
	// Header
	Type         RecordType // Low-level fragmentation type (Full, First, Middle, Last)
	Length       uint16
	CRC          uint32
	IsCompressed bool
	IsSnappy     bool
	IsZstd       bool

	// Data
	Data []byte
}

// ParseHeader extracts record type and compression flags from the header byte
func ParseHeader(headerByte byte) (RecordType, bool, bool) {
	recType := RecordType(headerByte & recTypeMask)
	isSnappy := (headerByte & snappyMask) != 0
	isZstd := (headerByte & zstdMask) != 0
	return recType, isSnappy, isZstd
}

// EncodeHeader creates a header byte from record type and compression flags
func EncodeHeader(recType RecordType, isSnappy, isZstd bool) byte {
	header := byte(recType)
	if isSnappy {
		header |= snappyMask
	}
	if isZstd {
		header |= zstdMask
	}
	return header
}

// SegmentReader reads records from a WAL segment file
type SegmentReader struct {
	file          *os.File
	reader        *bufio.Reader
	offset        int64
	err           error
	currentRecord *Record
}

// NewSegmentReader creates a new segment reader for the given file
func NewSegmentReader(filename string) (*SegmentReader, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open segment file: %w", err)
	}

	return &SegmentReader{
		file:   file,
		reader: bufio.NewReader(file),
		offset: 0,
	}, nil
}

// Close closes the segment reader
func (sr *SegmentReader) Close() error {
	if sr.file != nil {
		return sr.file.Close()
	}
	return nil
}

// Offset returns the current read offset in the segment
func (sr *SegmentReader) Offset() int64 {
	return sr.offset
}

// Err returns the last error encountered
func (sr *SegmentReader) Err() error {
	return sr.err
}

// Next reads the next complete record from the segment
func (sr *SegmentReader) Next() bool {
	if sr.err != nil {
		return false
	}

	record, err := sr.readRecord()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return false
		}
		sr.err = err
		return false
	}

	sr.currentRecord = record
	return true
}

// Record returns the current record
func (sr *SegmentReader) Record() *Record {
	return sr.currentRecord
}

var castagnoliTable = crc32.MakeTable(crc32.Castagnoli)

// readRecord reads a complete record, handling fragmentation across pages
func (sr *SegmentReader) readRecord() (*Record, error) {
	var recordData []byte
	var recordType RecordType
	var isSnappy, isZstd bool
	var firstFragment = true

	for {
		// Read record header
		header := make([]byte, RecordHeaderSize)
		n, err := io.ReadFull(sr.reader, header)
		if err != nil {
			return nil, err
		}
		sr.offset += int64(n)

		// Parse header
		recType, snappy, zstd := ParseHeader(header[0])
		length := binary.BigEndian.Uint16(header[1:3])
		crc := binary.BigEndian.Uint32(header[3:7])

		// Handle page termination
		if recType == RecordPageTerm {
			// Skip to next page boundary
			pageOffset := sr.offset % PageSize
			if pageOffset != 0 {
				skipBytes := PageSize - pageOffset
				_, err := sr.reader.Discard(int(skipBytes))
				if err != nil {
					return nil, err
				}
				sr.offset += skipBytes
			}
			continue
		}

		// Read record data
		data := make([]byte, length)
		n, err = io.ReadFull(sr.reader, data)
		if err != nil {
			return nil, err
		}
		sr.offset += int64(n)

		// Verify CRC
		if crc32.Checksum(data, castagnoliTable) != crc {
			return nil, fmt.Errorf("CRC mismatch at offset %d", sr.offset-int64(len(data)))
		}

		// Store compression info from first fragment
		if firstFragment {
			recordType = recType
			isSnappy = snappy
			isZstd = zstd
			firstFragment = false
		}

		// Append data to record
		recordData = append(recordData, data...)

		// Check if record is complete
		switch recType {
		case RecordFull:
			return &Record{
				Type:         recordType,
				Length:       uint16(len(recordData)),
				Data:         recordData,
				IsCompressed: isSnappy || isZstd,
				IsSnappy:     isSnappy,
				IsZstd:       isZstd,
			}, nil
		case RecordLast:
			return &Record{
				Type:         recordType,
				Length:       uint16(len(recordData)),
				Data:         recordData,
				IsCompressed: isSnappy || isZstd,
				IsSnappy:     isSnappy,
				IsZstd:       isZstd,
			}, nil
		case RecordFirst, RecordMiddle:
			// Continue reading fragments
			continue
		default:
			return nil, fmt.Errorf("invalid record type: %d", recType)
		}
	}
}

// SegmentWriter writes records to a WAL segment file
type SegmentWriter struct {
	file       *os.File
	writer     *bufio.Writer
	offset     int64
	pageOffset int64
}

// NewSegmentWriter creates a new segment writer for the given file
func NewSegmentWriter(filename string) (*SegmentWriter, error) {

	file, err := os.Create(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to create segment file: %w", err)
	}

	return &SegmentWriter{
		file:       file,
		writer:     bufio.NewWriter(file),
		offset:     0,
		pageOffset: 0,
	}, nil
}

// Close closes the segment writer and flushes any buffered data
func (sw *SegmentWriter) Close() error {
	if err := sw.writer.Flush(); err != nil {
		return err
	}
	if sw.file != nil {
		return sw.file.Close()
	}
	return nil
}

// Offset returns the current write offset in the segment
func (sw *SegmentWriter) Offset() int64 {
	return sw.offset
}

// WriteRecord writes a record to the segment, handling fragmentation if necessary
func (sw *SegmentWriter) WriteRecord(data []byte, isSnappy, isZstd bool) error {
	if len(data) == 0 {
		return nil
	}

	// Check if record would exceed segment size
	totalSize := int64(len(data)) + RecordHeaderSize
	if sw.offset+totalSize > SegmentSize {
		return fmt.Errorf("record would exceed segment size")
	}

	// Calculate available space in current page
	availableInPage := PageSize - (sw.pageOffset % PageSize)

	// If record fits entirely in current page
	if int64(len(data))+RecordHeaderSize <= availableInPage {
		return sw.writeRecordPart(data, RecordFull, isSnappy, isZstd)
	}

	// Fragment the record across pages
	remaining := data
	isFirst := true

	for len(remaining) > 0 {
		availableInPage = PageSize - (sw.pageOffset % PageSize)
		availableForData := availableInPage - RecordHeaderSize

		if availableForData <= 0 {
			// Fill rest of page with zeros and move to next page
			if err := sw.padToNextPage(); err != nil {
				return err
			}
			continue
		}

		// Determine how much data to write in this fragment
		fragmentSize := int64(len(remaining))
		if fragmentSize > availableForData {
			fragmentSize = availableForData
		}

		fragment := remaining[:fragmentSize]
		remaining = remaining[fragmentSize:]

		// Determine record type
		var recType RecordType
		switch {
		case isFirst && len(remaining) == 0:
			recType = RecordFull
		case isFirst:
			recType = RecordFirst
			isFirst = false
		case len(remaining) == 0:
			recType = RecordLast
		default:
			recType = RecordMiddle
		}

		if err := sw.writeRecordPart(fragment, recType, isSnappy, isZstd); err != nil {
			return err
		}
	}

	return nil
}

// WriteFullRecord writes a complete Record struct to the segment exactly as specified.
// This method honors the Type field from the Record struct and writes the record
// with the exact fragmentation type specified by the user.
func (sw *SegmentWriter) WriteFullRecord(record *Record) error {
	if record == nil {
		return fmt.Errorf("record cannot be nil")
	}

	if len(record.Data) == 0 {
		return nil
	}

	// Check if record would exceed segment size
	totalSize := int64(len(record.Data)) + RecordHeaderSize
	if sw.offset+totalSize > SegmentSize {
		return fmt.Errorf("record would exceed segment size")
	}

	// Write the record part with the exact type specified by the user
	return sw.writeRecordPart(record.Data, record.Type, record.IsSnappy, record.IsZstd)
}

// writeRecordPart writes a single record part with the given type
func (sw *SegmentWriter) writeRecordPart(data []byte, recType RecordType, isSnappy, isZstd bool) error {
	// Create header
	header := make([]byte, RecordHeaderSize)
	header[0] = EncodeHeader(recType, isSnappy, isZstd)
	binary.BigEndian.PutUint16(header[1:3], uint16(len(data)))
	binary.BigEndian.PutUint32(header[3:7], crc32.Checksum(data, castagnoliTable))

	// Write header
	if _, err := sw.writer.Write(header); err != nil {
		return err
	}

	// Write data
	if _, err := sw.writer.Write(data); err != nil {
		return err
	}

	// Update offsets
	written := int64(RecordHeaderSize + len(data))
	sw.offset += written
	sw.pageOffset += written

	return nil
}

// padToNextPage fills the remaining space in the current page with zeros
func (sw *SegmentWriter) padToNextPage() error {
	remainingInPage := PageSize - (sw.pageOffset % PageSize)
	if remainingInPage == PageSize {
		return nil // Already at page boundary
	}

	// Write page termination record
	header := make([]byte, RecordHeaderSize)
	header[0] = byte(RecordPageTerm)
	// Length and CRC are zero for page termination

	if _, err := sw.writer.Write(header); err != nil {
		return err
	}

	// Fill rest of page with zeros
	padding := make([]byte, remainingInPage-RecordHeaderSize)
	if _, err := sw.writer.Write(padding); err != nil {
		return err
	}

	// Update offsets
	sw.offset += remainingInPage
	sw.pageOffset = (sw.pageOffset + remainingInPage) % PageSize

	return nil
}

// Flush flushes any buffered data to disk
func (sw *SegmentWriter) Flush() error {
	return sw.writer.Flush()
}

// Sync flushes buffered data and syncs to disk
func (sw *SegmentWriter) Sync() error {
	if err := sw.writer.Flush(); err != nil {
		return err
	}
	return sw.file.Sync()
}
