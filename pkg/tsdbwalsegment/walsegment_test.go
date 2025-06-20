package tsdbwalsegment

import (
	"encoding/binary"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Test_Write tests writing a record using the segment writer and verifies
// the binary content of the file to ensure the header bytes and data are correct
func Test_Write(t *testing.T) {
	// Create a temporary directory for the test
	tmpDir, err := os.MkdirTemp("", "wal_write_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	segmentFile := filepath.Join(tmpDir, "000000")

	writer, err := NewSegmentWriter(segmentFile)
	require.NoError(t, err)

	// Test data
	testData := []byte("WAL")
	isSnappy := true
	isZstd := false

	// Write the record
	err = writer.WriteRecord(testData, isSnappy, isZstd)
	require.NoError(t, err)

	// Close the writer to flush data
	err = writer.Close()
	require.NoError(t, err)

	// Read the file content as binary
	fileContent, err := os.ReadFile(segmentFile)
	require.NoError(t, err)

	// Verify the file has the expected minimum size (header + data)
	expectedMinSize := RecordHeaderSize + len(testData)
	require.GreaterOrEqual(t, len(fileContent), expectedMinSize,
		"File size too small: got %d, expected at least %d", len(fileContent), expectedMinSize)

	// Parse and verify the header (first 7 bytes)
	header := fileContent[:RecordHeaderSize]

	// Byte 0: Record type and compression flags
	headerByte := header[0]
	recordType, snappy, zstd := ParseHeader(headerByte)

	require.Equal(t, RecordFull, recordType,
		"Expected record type RecordFull (%d), got %s (%d)", RecordFull, recordType.String(), recordType)
	require.Equal(t, isSnappy, snappy,
		"Expected Snappy flag %t, got %t", isSnappy, snappy)
	require.Equal(t, isZstd, zstd,
		"Expected Zstd flag %t, got %t", isZstd, zstd)

	// Bytes 1-2: Data length (big-endian uint16)
	dataLength := binary.BigEndian.Uint16(header[1:3])
	require.Equal(t, uint16(len(testData)), dataLength,
		"Expected data length %d, got %d", len(testData), dataLength)

	// Bytes 3-6: CRC32 checksum (big-endian uint32)
	expectedCRC := crc32.Checksum(testData, crc32.MakeTable(crc32.Castagnoli))
	actualCRC := binary.BigEndian.Uint32(header[3:7])
	require.Equal(t, expectedCRC, actualCRC,
		"Expected CRC32 %d, got %d", expectedCRC, actualCRC)

	// Verify the data content
	actualData := fileContent[RecordHeaderSize : RecordHeaderSize+len(testData)]
	require.Equal(t, string(testData), string(actualData),
		"Expected data %q, got %q", string(testData), string(actualData))
}

// Test_WriteFullRecordAndRead tests writing a full record using WriteFullRecord
// and then reading it back using the segment reader to ensure round-trip consistency
func Test_WriteFullRecordAndRead(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "wal_full_record_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	segmentFile := filepath.Join(tmpDir, "000002")

	writer, err := NewSegmentWriter(segmentFile)
	require.NoError(t, err)

	// Create test records with different types and compression settings
	testRecords := []*Record{
		{
			Type:         RecordFull,
			Data:         []byte("Uncompressed full record"),
			IsCompressed: false,
			IsSnappy:     false,
			IsZstd:       false,
		},
		{
			Type:         RecordFull,
			Data:         []byte("Snappy compressed record"),
			IsCompressed: true,
			IsSnappy:     true,
			IsZstd:       false,
		},
		{
			Type:         RecordFull,
			Data:         []byte("Zstd compressed record"),
			IsCompressed: true,
			IsSnappy:     false,
			IsZstd:       true,
		},
	}

	// Write all test records
	for i, record := range testRecords {
		err := writer.WriteFullRecord(record)
		require.NoError(t, err, "Failed to write full record %d", i)
	}

	err = writer.Close()
	require.NoError(t, err)

	// Create a reader to read back the records
	reader, err := NewSegmentReader(segmentFile)
	require.NoError(t, err)
	defer reader.Close()

	// Read back and verify each record
	recordIndex := 0
	for reader.Next() {
		require.Less(t, recordIndex, len(testRecords),
			"Read more records than expected: got record %d", recordIndex)

		actualRecord := reader.Record()
		expectedRecord := testRecords[recordIndex]

		// Verify record type
		require.Equal(t, expectedRecord.Type, actualRecord.Type,
			"Record %d: Expected type %s, got %s",
			recordIndex, expectedRecord.Type.String(), actualRecord.Type.String())

		// Verify compression flags
		require.Equal(t, expectedRecord.IsCompressed, actualRecord.IsCompressed,
			"Record %d: Expected IsCompressed %t, got %t",
			recordIndex, expectedRecord.IsCompressed, actualRecord.IsCompressed)
		require.Equal(t, expectedRecord.IsSnappy, actualRecord.IsSnappy,
			"Record %d: Expected IsSnappy %t, got %t",
			recordIndex, expectedRecord.IsSnappy, actualRecord.IsSnappy)
		require.Equal(t, expectedRecord.IsZstd, actualRecord.IsZstd,
			"Record %d: Expected IsZstd %t, got %t",
			recordIndex, expectedRecord.IsZstd, actualRecord.IsZstd)

		// Verify data length
		require.Equal(t, uint16(len(expectedRecord.Data)), actualRecord.Length,
			"Record %d: Expected length %d, got %d",
			recordIndex, len(expectedRecord.Data), actualRecord.Length)

		// Verify data content
		require.Equal(t, string(expectedRecord.Data), string(actualRecord.Data),
			"Record %d: Expected data %q, got %q",
			recordIndex, string(expectedRecord.Data), string(actualRecord.Data))

		recordIndex++
	}

	// Check for reading errors
	require.NoError(t, reader.Err(), "Error reading segment")

	// Verify we read the expected number of records
	require.Equal(t, len(testRecords), recordIndex,
		"Expected to read %d records, but read %d", len(testRecords), recordIndex)
}
