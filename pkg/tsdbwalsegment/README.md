# WAL Segment Package

This package provides functionality to read and write individual WAL (Write-Ahead Log) segment files according to the [Prometheus TSDB WAL format specification](./wal-segment-format.md).

This package is mainly used to create custom WAL segments for test purposes (i.e., create corrupted segments).

## Overview

The package supports:
- Reading and writing WAL segment files
- Automatic record fragmentation across 32KB pages
- Compression support (Snappy and Zstd)
- Fine-grained control over record types for testing

## Usage

### Basic Writing with WriteRecord

The `WriteRecord` method automatically handles record fragmentation and determines the appropriate record types:

```go
import "github.com/grafana/loki/v3/pkg/tsdbwalsegment"

// Create a new segment writer
writer, err := tsdbwalsegment.NewSegmentWriter("segment_000001")
if err != nil {
    log.Fatal(err)
}
defer writer.Close()

// Write raw data with automatic fragmentation
data := []byte("Hello, WAL!")
err = writer.WriteRecord(data, false, false) // no compression
if err != nil {
    log.Fatal(err)
}

// Write compressed data
compressedData := []byte("This will be compressed")
err = writer.WriteRecord(compressedData, true, false) // Snappy compression
if err != nil {
    log.Fatal(err)
}

// Write with Zstd compression
zstdData := []byte("This will be Zstd compressed")
err = writer.WriteRecord(zstdData, false, true) // Zstd compression
if err != nil {
    log.Fatal(err)
}
```

### Precise Control with WriteFullRecord

The `WriteFullRecord` method gives you complete control over the record type and compression flags. This is particularly useful for testing scenarios where you need specific record fragmentation:

```go
// Create a record with specific type and compression settings
record := &tsdbwalsegment.Record{
    Type:         tsdbwalsegment.RecordFirst, // Exact type will be preserved
    Data:         []byte("First part of a fragmented record"),
    IsCompressed: true,
    IsSnappy:     true,
    IsZstd:       false,
}

// Write the record directly with the exact type specified
err := writer.WriteFullRecord(record)
if err != nil {
    log.Fatal(err)
}

// Write the corresponding last fragment
lastRecord := &tsdbwalsegment.Record{
    Type:         tsdbwalsegment.RecordLast,
    Data:         []byte(" and this completes the record."),
    IsCompressed: true,
    IsSnappy:     true,
    IsZstd:       false,
}

err = writer.WriteFullRecord(lastRecord)
if err != nil {
    log.Fatal(err)
}
```

### Reading Records

```go
// Open a segment for reading
reader, err := tsdbwalsegment.NewSegmentReader("segment_000001")
if err != nil {
    log.Fatal(err)
}
defer reader.Close()

// Read all records (fragmented records are automatically assembled)
for reader.Next() {
    record := reader.Record()
    fmt.Printf("Type: %s, TSDB Type: %s, Length: %d, Data: %s\n", 
        record.Type.String(), 
        record.TSDBType.String(),
        record.Length, 
        string(record.Data))
}

if err := reader.Err(); err != nil {
    log.Fatal(err)
}
```
### Header Manipulation

```go
// Parse a header byte
recordType, isSnappy, isZstd := tsdbwalsegment.ParseHeader(0x09)
fmt.Printf("Type: %s, Snappy: %t, Zstd: %t\n", recordType.String(), isSnappy, isZstd)

// Create a header byte
headerByte := tsdbwalsegment.EncodeHeader(tsdbwalsegment.RecordFull, true, false)
fmt.Printf("Header byte: 0x%02X\n", headerByte)
```


## Key Differences Between Methods

### WriteRecord vs WriteFullRecord

- **WriteRecord**: Automatically determines record types based on data size and page boundaries. Best for normal usage.
- **WriteFullRecord**: Uses the exact `Type` field from the Record struct. Essential for testing scenarios where you need specific fragmentation patterns.

Example of the difference:

```go
// WriteRecord: Automatically fragments large data
largeData := make([]byte, 50000) // 50KB
err := writer.WriteRecord(largeData, false, false)
// This will create RecordFirst + RecordMiddle + RecordLast fragments automatically

// WriteFullRecord: Uses exact type specified
record := &tsdbwalsegment.Record{
    Type: tsdbwalsegment.RecordFirst, // This exact type will be used
    Data: largeData,
}
err := writer.WriteFullRecord(record)
// This will create exactly one RecordFirst record (may be invalid if data is too large)
```

## WAL Segment Format

Each WAL segment consists of:
- 32KB pages containing records
- 7-byte headers per record: [Type+Flags][Length][CRC32]
- Variable-length data following each header
- Automatic fragmentation for records spanning multiple pages

### Record Header Format
```
Byte 0: [Type (3 bits)] [Compression flags (5 bits)]
Bytes 1-2: Data length (big-endian uint16)
Bytes 3-6: CRC32 checksum (big-endian uint32)
```

### Compression Flags
- Bit 3: Snappy compression flag
- Bit 4: Zstd compression flag

## Testing and Corruption Scenarios

This package is particularly useful for creating test scenarios:

```go
tornRecord := &tsdbwalsegment.Record{
    Type: tsdbwalsegment.RecordFirst, // Indicates more fragments follow
    Data: []byte("This record will never be completed"),
}
writer.WriteFullRecord(tornRecord)
writer.Close() // Close without writing RecordLast

```



