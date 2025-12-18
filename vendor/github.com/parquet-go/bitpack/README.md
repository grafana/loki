# bitpack

[![Go Reference](https://pkg.go.dev/badge/github.com/parquet-go/bitpack.svg)](https://pkg.go.dev/github.com/parquet-go/bitpack)

A high-performance Go library for bit packing and unpacking integers of various bit widths. Part of
the [parquet-go](https://github.com/parquet-go/parquet-go) ecosystem.

Includes AMD64 assembly optimizations with pure Go fallback for portability.

```bash
go get github.com/parquet-go/bitpack
```

## Usage

```go
import "github.com/parquet-go/bitpack"

// Pack int32 values with 3-bit width
values := []int32{1, 2, 3, 4, 5}
bitWidth := uint(3)
packedSize := bitpack.ByteCount(uint(len(values)) * bitWidth)
dst := make([]byte, packedSize+bitpack.PaddingInt32)
bitpack.PackInt32(dst, values, bitWidth)

// Unpack int32 values
unpacked := make([]int32, len(values))
bitpack.UnpackInt32(unpacked, dst, bitWidth)
```

For complete working examples, see the [examples](./examples) directory.
