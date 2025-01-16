// Package dataobj holds utilities for working with data objects.
package dataobj

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/sections/streams"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// ErrBufferFull is returned by [Builder.Append] when the buffer is full and
// needs to flush; call [Builder.Flush] to flush it.
var ErrBufferFull = errors.New("buffer full")

// BuilderConfig configures a data object [Builder].
type BuilderConfig struct {
	// SHAPrefixSize sets the number of bytes of the SHA filename to use as a
	// folder path.
	SHAPrefixSize int

	// TargetPageSize configures a target size for encoded pages within the data
	// object. TargetPageSize accounts for encoding, but not for compression.
	TargetPageSize flagext.Bytes

	// TODO(rfratto): We need an additional parameter for TargetMetadataSize, as
	// metadata payloads can't be split and must be downloaded in a single
	// request.
	//
	// At the moment, we don't have a good mechanism for implementing a metadata
	// size limit (we need to support some form of section splitting or column
	// combinations), so the option is omitted for now.

	// TargetObjectSize configures a target size for data objects.
	TargetObjectSize flagext.Bytes
}

func (cfg *BuilderConfig) validate() error {
	var errs []error

	if cfg.SHAPrefixSize <= 0 {
		errs = append(errs, errors.New("SHAPrefixSize must be greater than 0"))
	}

	if cfg.TargetPageSize <= 0 {
		errs = append(errs, errors.New("TargetPageSize must be greater than 0"))
	} else if cfg.TargetPageSize >= cfg.TargetObjectSize {
		errs = append(errs, errors.New("TargetPageSize must be less than TargetObjectSize"))
	}

	if cfg.TargetObjectSize <= 0 || cfg.TargetObjectSize > 3_000_000_000 {
		errs = append(errs, errors.New("TargetObjectSize must be greater than 0 and less than 3GB"))
	}

	return errors.Join(errs...)
}

// A Builder builds data objects from a set of incoming log data. Log data is
// appended to a builder by calling [Builder.Append]. Buffered log data is
// flushed manually by calling [Builder.Flush].
//
// Methods on Builder are not goroutine-safe; callers are responsible for
// synchronizing calls.
type Builder struct {
	cfg      BuilderConfig
	bucket   objstore.Bucket
	tenantID string

	dirty bool // Whether the builder has been modified since the last flush.

	streams *streams.Streams
	logs    *logs.Logs
}

// NewBuilder creates a new Builder which stores data objects for the specified
// tenant in a bucket.
//
// NewBuilder returns an error if BuilderConfig is invalid.
func NewBuilder(cfg BuilderConfig, bucket objstore.Bucket, tenantID string) (*Builder, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &Builder{
		cfg:      cfg,
		bucket:   bucket,
		tenantID: tenantID,

		streams: streams.New(int(cfg.TargetPageSize)),
		logs:    logs.New(int(cfg.TargetPageSize)),
	}, nil
}

// Append buffers a stream to be written to a data object. Append returns an
// error if the stream labels cannot be parsed or [ErrBufferFull] if the
// builder is full.
//
// Once a Builder is full, call [Builder.Flush] to flush the buffered data,
// then call Append again with the same entry.
func (b *Builder) Append(stream logproto.Stream) error {
	ls, err := syntax.ParseLabels(stream.Labels)
	if err != nil {
		return err
	}

	// Check whether the buffer is full before a stream can be appended; this is
	// tends to overestimate, but we may still go over our target size.
	if b.dirty && b.estimatedSize()+labelsEstimate(ls)+streamSizeEstimate(stream) > int(b.cfg.TargetObjectSize) {
		return ErrBufferFull
	}

	for _, entry := range stream.Entries {
		streamID := b.streams.Record(ls, entry.Timestamp)

		b.logs.Append(logs.Record{
			StreamID:  streamID,
			Timestamp: entry.Timestamp,
			Metadata:  entry.StructuredMetadata,
			Line:      entry.Line,
		})
	}

	b.dirty = true
	return nil
}

func (b *Builder) estimatedSize() int {
	var size int
	size += b.streams.EstimatedSize()
	size += b.logs.EstimatedSize()
	return size
}

// labelsEstimate estimates the size of a set of labels in bytes.
func labelsEstimate(ls labels.Labels) int {
	var (
		keysSize   int
		valuesSize int
	)

	for _, l := range ls {
		keysSize += len(l.Name)
		valuesSize += len(l.Value)
	}

	// Keys are stored as columns directly, while values get compressed. We'll
	// underestimate a 2x compression ratio.
	return keysSize + valuesSize/2
}

// streamSizeEstimate estimates the size of a stream in bytes.
func streamSizeEstimate(stream logproto.Stream) int {
	var size int
	for _, entry := range stream.Entries {
		// We only check the size of the line and metadata. Timestamps and IDs
		// encode so well that they're unlikely to make a singificant impact on our
		// size estimate.
		size += len(entry.Line) / 2 // Line with 2x compression ratio
		for _, md := range entry.StructuredMetadata {
			size += len(md.Name) + len(md.Value)/2
		}
	}
	return size
}

// Flush flushes all buffered data to object storage. Calling Flush can result
// in a no-op if there is no buffered data to flush.
func (b *Builder) Flush(ctx context.Context) error {
	if !b.dirty {
		return nil
	}
	defer b.reset()

	buf := bytesBufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bytesBufferPool.Put(buf)

	enc := encoding.NewEncoder(buf)

	if err := b.streams.EncodeTo(enc); err != nil {
		return fmt.Errorf("encoding streams: %w", err)
	} else if err := b.logs.EncodeTo(enc); err != nil {
		return fmt.Errorf("encoding logs: %w", err)
	} else if err := enc.Flush(); err != nil {
		return fmt.Errorf("encoding object: %w", err)
	}

	sum := sha256.Sum224(buf.Bytes())
	sumStr := hex.EncodeToString(sum[:])

	path := fmt.Sprintf("tenant-%s/objects/%s/%s", b.tenantID, sumStr[:b.cfg.SHAPrefixSize], sumStr[b.cfg.SHAPrefixSize:])
	return b.bucket.Upload(ctx, path, bytes.NewReader(buf.Bytes()))
}

func (b *Builder) reset() {
	b.dirty = false
}
