# 2025 revival

For IEEE checksums AVX512 can be used to speed up CRC32 checksums by approximately 2x.

Castagnoli checksums (CRC32C) can also be computer with AVX512, 
but the performance gain is not as significant enough for the downsides of using it at this point.

# crc32

This package is a drop-in replacement for the standard library `hash/crc32` package, 
that features AVX 512 optimizations on x64 platforms, for a 2x speedup for IEEE CRC32 checksums.

# usage

Install using `go get github.com/klauspost/crc32`. This library is based on Go 1.24

Replace `import "hash/crc32"` with `import "github.com/klauspost/crc32"` and you are good to go.

# changes
* 2025: Revived and updated to Go 1.24, with AVX 512 optimizations.

# performance

AVX512 are enabled above 1KB input size. This rather high limit is due to AVX512 may be slower to ramp up than 
the regular SSE4 implementation for smaller inputs. This is not reflected in the benchmarks below.

| Benchmark                                     | Old MB/s | New MB/s | Speedup |
|-----------------------------------------------|----------|----------|---------|
| BenchmarkCRC32/poly=IEEE/size=512/align=0-32  | 17996.39 | 17969.94 | 1.00x   |
| BenchmarkCRC32/poly=IEEE/size=512/align=1-32  | 18021.48 | 17945.55 | 1.00x   |
| BenchmarkCRC32/poly=IEEE/size=1kB/align=0-32  | 19921.70 | 45613.77 | 2.29x   |
| BenchmarkCRC32/poly=IEEE/size=1kB/align=1-32  | 19946.60 | 46819.09 | 2.35x   |
| BenchmarkCRC32/poly=IEEE/size=4kB/align=0-32  | 21538.65 | 48600.93 | 2.26x   |
| BenchmarkCRC32/poly=IEEE/size=4kB/align=1-32  | 21449.20 | 48477.84 | 2.26x   |
| BenchmarkCRC32/poly=IEEE/size=32kB/align=0-32 | 21785.49 | 46013.10 | 2.11x   |
| BenchmarkCRC32/poly=IEEE/size=32kB/align=1-32 | 21946.47 | 45954.10 | 2.09x   |

cpu: AMD Ryzen 9 9950X 16-Core Processor

# license

Standard Go license. See [LICENSE](LICENSE) for details.
