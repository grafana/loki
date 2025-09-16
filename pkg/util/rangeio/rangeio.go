// Package rangeio provides basic interfaces and utilities for reading ranges of
// data.
package rangeio

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"io"
	"runtime"
	"slices"
	"sort"
	"time"

	"github.com/dustin/go-humanize"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"
)

var tracer = otel.Tracer("pkg/util/rangeio")

// Range represents a range of data to be read.
type Range struct {
	// Data to read into; exactly len(Data) bytes will be read, or an error will
	// be returned.
	Data []byte

	// Offset to start reading from.
	Offset int64
}

// Len returns the length of the range.
func (r Range) Len() int64 { return int64(len(r.Data)) }

// Reader is the interface that wraps the basic ReadRange method. Reader is
// similar to [io.ReaderAt], but allows providing a [context.Context] for
// canceling the operation.
type Reader interface {
	// ReadRange reads len(r.Data) bytes into r.Data starting at r.Offset in the
	// underlying input source.
	//
	// It returns the number of bytes read (0 <= n <= len(r.Data)) and any error
	// encountered.
	//
	// When ReadRange returns n < len(r.Data), it returns a non-nil error
	// explaining why more bytes were not returned. The error must be [io.EOF]
	// when reading beyond the end of the input source.
	//
	// ReadRange may use all of r.Data as scratch space during the call, even if
	// less than len(r.Data) bytes are read. If some data is available but not
	// len(r.Data) bytes, ReadRange blocks until either all the data is
	// available or an error occurs.
	//
	// Implementations are recommended but not required to immediately respond
	// to the cancellation of ctx; for example, cancellation may not occur
	// immediately when using disk-based I/O.
	//
	// If the len(r.Data) bytes returned by ReadRange are at the end of the
	// input source, ReadRange may return either err == [io.EOF] or err == nil.
	//
	// If ReadRange is reading from an input source with a seek offset,
	// ReadRange should not affect nor be affected by the underlying seek
	// offset.
	//
	// It is safe to call ReadRange concurrently from multiple goroutines.
	//
	// Implementations must not retain r.Data after the call returns.
	ReadRange(ctx context.Context, r Range) (int, error)
}

// Config configures the behavior of [ReadRanges].
type Config struct {
	// MaxParallelism is the maximum number of goroutines that may be used to
	// read ranges in parallel. If MaxParallelism <= 0, [runtime.NumCPU] is
	// used.
	//
	// Ranges are split into smaller ranges (no smaller than MinRangeSize) to
	// get as close as possible to MaxParallelism.
	//
	// If MaxParallelism is 1, ReadRanges will read each range sequentially.
	MaxParallelism int `yaml:"max_parallelism" category:"experimental"`

	// CoalesceSize determines the maximum size (in bytes) of a gap between each
	// pair of ranges that causes them to be coalesced into a single range.
	CoalesceSize int `yaml:"coalesce_size" category:"experimental"`

	// MaxRangeSize determines the maximum size (in bytes) of a range. Ranges
	// won't be coalesced if they exceed this size, and existing ranges will be
	// split if they exceed this size (down to MinRangeSize).
	MaxRangeSize int `yaml:"max_range_size" category:"experimental"`

	// MinRangeSize determines the minimum size (in bytes) of a range. When a
	// range is split, it won't be split into units smaller than MinRangeSize.
	MinRangeSize int `yaml:"min_range_size" category:"experimental"`
}

func (cfg *Config) RegisterFlags(prefix string, fs *flag.FlagSet) {
	fs.IntVar(&cfg.MaxParallelism, prefix+"max-parallelism", DefaultConfig.MaxParallelism, "Experimental: maximum number of parallel reads")
	fs.IntVar(&cfg.CoalesceSize, prefix+"coalesce-size", DefaultConfig.CoalesceSize, "Experimental: maximum distance (in bytes) between ranges that causes them to be coalesced into a single range")
	fs.IntVar(&cfg.MaxRangeSize, prefix+"max-range-size", DefaultConfig.MaxRangeSize, "Experimental: maximum size of a byte range")
	fs.IntVar(&cfg.MinRangeSize, prefix+"min-range-size", DefaultConfig.MinRangeSize, "Experimental: minimum size of a byte range")
}

// effectiveParallelism returns the effective parallelism limit.
func (cfg *Config) effectiveParallelism() int {
	if cfg.MaxParallelism <= 0 {
		return runtime.NumCPU()
	}
	return cfg.MaxParallelism
}

// DefaultConfig holds the default values for [Config].
var DefaultConfig = Config{
	// Benchmarks of GCS and S3 revealed that more parallelism is always better.
	// However, too much parallelism (especially parallel [ReadRanges] calls)
	// can eventually saturate the network. MaxParallelism of 10 appears to
	// provide a good balance of throughput without saturating the network.
	MaxParallelism: 10,

	// Coalesce ranges no further than 1 MiB apart. 1 MiB is a good balance
	// between combining ranges without introducing too many "wasted" bytes.
	CoalesceSize: 1 << 20, // 1 MiB

	// Constrain ranges to be no longer than 8 MiB. Benchmarks of GCS and S3
	// revealed that 8 MiB typically offers the best throughput when there's
	// enough ranges to fill MaxParallelism.
	MaxRangeSize: 8 * (1 << 20), // 8 MiB

	// Constrain split ranges to be no smaller than 1 MiB.
	MinRangeSize: 1 << 20, // 1 MiB
}

// ReadRanges reads the set of ranges from the provided Reader, populating Data
// for each element in ranges.
//
// ReadRanges makes a copy of ranges and optimizes them for performance:
// coalescing ranges that are close together and splitting ranges that are too
// large. The optimized set of ranges are read in parallel.
//
// The optimization behavior of ReadRanges can be controlled by providing a
// context injected with custom configuration by [WithConfig]. If there is no
// custom configuration in the context, [DefaultConfig] is used.
//
// ReadRanges returns an error if any call to r.ReadRange returns an error.
// ReadRanges only returns [io.EOF] if one of the ranges is beyond the end of
// the input source.
func ReadRanges(ctx context.Context, r Reader, ranges []Range) error {
	// We store our own start time so we can calculate read throughput at the
	// end.
	startTime := time.Now()
	ctx, span := tracer.Start(ctx, "ReadRanges", trace.WithTimestamp(startTime))
	defer span.End()

	cfg := configFromContext(ctx)
	if cfg == nil {
		cfg = &DefaultConfig
		span.SetAttributes(attribute.Bool("config.default", true))
	}
	optimized := optimizeRanges(cfg, ranges)
	span.AddEvent("optimized ranges")

	// Once we optimized the ranges we can set up the rest of our attributes.
	span.SetAttributes(readRangesAttributes(cfg, ranges, optimized)...)
	defer injectThroughputAttribute(span, startTime, optimized)

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(cfg.effectiveParallelism())

	var gotEOF atomic.Bool

	for _, targetRange := range optimized {
		// Ignore ranges that happened to be empty.
		if len(targetRange.Data) == 0 {
			continue
		}

		g.Go(func() error {
			tr := tracedReader{inner: r}
			n, err := tr.ReadRange(ctx, targetRange)

			// ReadRange must return a non-nil error if it read fewer than the
			// requested amount of bytes.
			//
			// In the case of io.EOF (because we tried reading beyond the end of
			// the input source), we don't want to cancel the other goroutines,
			// so we store the EOF marker for later.
			if n < len(targetRange.Data) {
				if errors.Is(err, io.EOF) {
					gotEOF.Store(true)
					return nil
				}
				return err
			}

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	span.AddEvent("finished reading ranges")

	// Now that we read the ranges, we can copy the data back into the original
	// slice.
	for _, r := range ranges {
		// Ignore ranges that happened to be empty.
		if len(r.Data) == 0 {
			continue
		}

		// Our range may have been split across smaller ranges, so we need to
		// gradually copy the data back into the original slice by finding each
		// subslice in optimized.
		offset := r.Offset
		output := r.Data

		for len(output) > 0 {
			// Find the first slice that ends after the offset we're looking
			// for.
			i := sort.Search(len(optimized), func(i int) bool {
				endByte := optimized[i].Offset + int64(len(optimized[i].Data))
				return endByte > offset
			})
			if i == len(optimized) {
				// This can't ever happen; our ranges are always inside the
				// optimized slice.
				return errors.New("requested offset missing from coalesced ranges")
			}

			copied := copy(output, optimized[i].Data[offset-optimized[i].Offset:])

			// Move our offset and output forward by the amount of copied data.
			offset += int64(copied)
			output = output[copied:]
		}
	}

	span.AddEvent("copied data to inputs")
	span.SetStatus(codes.Ok, "") // Even if we got [io.EOF], we treat the operation as successful here.

	if gotEOF.Load() {
		return io.EOF
	}
	return nil
}

// optimizeRanges optimizes the set of ranges based on cfg. The returned slice
// of ranges is sorted.
//
// If permitParallelismSplit is true, ranges will be also be split to try to
// reach at least cfg.MaxParallelism ranges.
//
// If cfg is nil, [DefaultConfig] is used.
func optimizeRanges(cfg *Config, in []Range) []Range {
	if cfg == nil {
		cfg = &DefaultConfig
	}

	// chunk is a [Range] but without the allocated data slice.
	type chunk struct {
		Offset int64
		Length int
	}

	sorted := make([]chunk, len(in))
	for i := range in {
		sorted[i].Offset = in[i].Offset
		sorted[i].Length = len(in[i].Data)
	}
	slices.SortFunc(sorted, func(a, b chunk) int { return cmp.Compare(a.Offset, b.Offset) })

	var coalescedChunks []chunk

	for i := 0; i < len(sorted); {
		coalescedOffset := sorted[i].Offset
		coalescedEnd := coalescedOffset + int64(sorted[i].Length)

		// Look at ranges after i to see if we can coalesce them.
		peekIndex := i + 1
		for peekIndex < len(sorted) {
			peekOffset := sorted[peekIndex].Offset
			peekEnd := sorted[peekIndex].Offset + int64(sorted[peekIndex].Length)

			if coalescedEnd > peekOffset {
				// Coalesce overlapping ranges, regardless of the size. They can
				// be split in the follow up logic after this loop.
				goto Coalesce
			}

			if peekOffset-coalescedEnd > int64(cfg.CoalesceSize) {
				// Gap between the current coalesced range and the peek range is
				// too big; stop.
				break
			} else if peekEnd-coalescedOffset > int64(cfg.MaxRangeSize) {
				// Coalescing the peeked range would cause the range to exceed
				// the max size; stop.
				break
			}

		Coalesce:
			coalescedEnd = max(coalescedEnd, peekEnd)

			peekIndex++
		}

		// Our coalesced range is now [coalescedOffset, coalescedEnd). This
		// may exceed our max range size in two cases:
		//
		//   1. We merged overlapping ranges
		//   2. We received a range which was already larger than the max range
		//      size.
		//
		// Ranges which are too big should be split into halves, unless those
		// halves would be smaller than our minimum size; it's preferable to
		// have something too big than too small.
		targetLength := coalescedEnd - coalescedOffset
		if targetLength > int64(cfg.MaxRangeSize) && targetLength >= int64(cfg.MinRangeSize*2) {
			targetLength = max(targetLength/2, int64(cfg.MinRangeSize))
		}

		// NOTE(rfratto): This loop will only run once if targetLength was left
		// unchanged.
		for off := coalescedOffset; off < coalescedEnd; off += targetLength {
			splitEndOffset := min(off+targetLength, coalescedEnd)
			splitLength := int(splitEndOffset - off)

			coalescedChunks = append(coalescedChunks, chunk{
				Length: splitLength,
				Offset: off,
			})
		}

		// The next iteration should start where the previous loop stopped.
		i = peekIndex
	}

	// Final pass: after the ranges have been coalesced, we may have ended up
	// with fewer ranges than the amount of parallelism. In this case, it's
	// better to have smaller ranges that spread out the work, so we'll split
	// ranges until we have enough.
	//
	// This is a no-op if we already have enough ranges.
	for i := 0; i < len(coalescedChunks) && len(coalescedChunks) < cfg.effectiveParallelism(); {
		// Ignore ranges which are too small or where splitting would cause them
		// to become too small.
		if coalescedChunks[i].Length < (cfg.MinRangeSize * 2) {
			i++
			continue
		}

		orig := coalescedChunks[i]

		targetLength := max(orig.Length/2, cfg.MinRangeSize)

		// Split the range in-order so we don't have to re-sort at the end.
		coalescedChunks = slices.Replace(coalescedChunks, i, i+1, chunk{
			Offset: orig.Offset,
			Length: targetLength,
		}, chunk{
			Offset: orig.Offset + int64(targetLength),
			Length: orig.Length - targetLength,
		})

		i += 2 // Skip over the range we just inserted.
	}

	// Convert our chunks into target ranges.
	out := make([]Range, len(coalescedChunks))
	for i := range coalescedChunks {
		// TODO(rfratto): Should the slices here be pooled? The allocated memory
		// here becomes unreferenced after returning from [ReadRanges].
		out[i] = Range{
			Data:   make([]byte, coalescedChunks[i].Length),
			Offset: coalescedChunks[i].Offset,
		}
	}
	return out
}

func rangesSize(ranges []Range) uint64 {
	var total uint64
	for _, r := range ranges {
		total += uint64(r.Len())
	}
	return total
}

// readRangesAttributes retrieves attributes about [ReadRanges] to be injected in spans.
func readRangesAttributes(cfg *Config, ranges, optimizedRanges []Range) []attribute.KeyValue {
	origSize := rangesSize(ranges)
	optimizedSize := rangesSize(optimizedRanges)

	return []attribute.KeyValue{
		attribute.Int("config.max_paralleism", cfg.MaxParallelism),
		attribute.Stringer("config.coalesce_size", bytesStringer(uint64(cfg.CoalesceSize))),
		attribute.Stringer("config.max_range_size", bytesStringer(uint64(cfg.MaxRangeSize))),
		attribute.Stringer("config.min_range_size", bytesStringer(uint64(cfg.MinRangeSize))),
		attribute.Int("config.effective_parlalelism", cfg.effectiveParallelism()),

		attribute.Int("input.ranges.count", len(ranges)),
		attribute.Stringer("input.ranges.size", bytesStringer(origSize)),

		attribute.Int("optimized.ranges.count", len(optimizedRanges)),
		attribute.Stringer("optimized.ranges.size", bytesStringer(optimizedSize)),
	}
}

func injectThroughputAttribute(span trace.Span, startTime time.Time, optimizedRanges []Range) {
	size := rangesSize(optimizedRanges)

	bytesPerSec := float64(size) / time.Since(startTime).Seconds()
	span.SetAttributes(attribute.Stringer("optimized.ranges.throughput", bytesStringer(uint64(bytesPerSec))))
}

type bytesStringer uint64

func (s bytesStringer) String() string {
	return humanize.Bytes(uint64(s))
}

// tracedReader injects span events after reading a range.
type tracedReader struct {
	inner Reader
}

func (tr tracedReader) ReadRange(ctx context.Context, r Range) (int, error) {
	start := time.Now()
	span := trace.SpanFromContext(ctx)

	n, err := tr.inner.ReadRange(ctx, r)

	if span.IsRecording() {
		bytesPerSec := float64(r.Len()) / time.Since(start).Seconds()

		span.AddEvent("read optimized range", trace.WithAttributes(
			attribute.Int64("offset", r.Offset),
			attribute.Int64("len", r.Len()),
			attribute.Int("read.size", n),
			attribute.Stringer("read.duration", time.Since(start)),
			attribute.Stringer("read.throughput", bytesStringer(uint64(bytesPerSec))),
		))
	}

	return n, err
}
