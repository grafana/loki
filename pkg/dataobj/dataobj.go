// Package dataobj holds utilities for working with data objects.
package dataobj

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"

	"github.com/grafana/dskit/flagext"
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/prometheus/client_golang/prometheus"
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
	SHAPrefixSize int `yaml:"sha_prefix_size"`

	// TargetPageSize configures a target size for encoded pages within the data
	// object. TargetPageSize accounts for encoding, but not for compression.
	TargetPageSize flagext.Bytes `yaml:"target_page_size"`

	// TODO(rfratto): We need an additional parameter for TargetMetadataSize, as
	// metadata payloads can't be split and must be downloaded in a single
	// request.
	//
	// At the moment, we don't have a good mechanism for implementing a metadata
	// size limit (we need to support some form of section splitting or column
	// combinations), so the option is omitted for now.

	// TargetObjectSize configures a target size for data objects.
	TargetObjectSize flagext.Bytes `yaml:"target_object_size"`
}

// RegisterFlagsWithPrefix registers flags with the given prefix.
func (cfg *BuilderConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	_ = cfg.TargetPageSize.Set("2MB")
	_ = cfg.TargetObjectSize.Set("1GB")

	f.IntVar(&cfg.SHAPrefixSize, prefix+"sha-prefix-size", 2, "The size of the SHA prefix to use for the data object builder.")
	f.Var(&cfg.TargetPageSize, prefix+"target-page-size", "The size of the target page to use for the data object builder.")
	f.Var(&cfg.TargetObjectSize, prefix+"target-object-size", "The size of the target object to use for the data object builder.")
}

// Validate validates the BuilderConfig.
func (cfg *BuilderConfig) Validate() error {
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
	metrics  *metrics
	bucket   objstore.Bucket
	tenantID string

	labelCache *lru.Cache[string, labels.Labels]

	currentSizeEstimate int
	state               builderState

	streams *streams.Streams
	logs    *logs.Logs

	flushBuffer *bytes.Buffer
	encoder     *encoding.Encoder
}

type builderState int

const (
	// builderStateReady indicates the builder is empty and ready to accept new data.
	builderStateEmpty builderState = iota

	// builderStateDirty indicates the builder has been modified since the last flush.
	builderStateDirty

	// builderStateFlushing indicates the builder has data to flush.
	builderStateFlush
)

// NewBuilder creates a new Builder which stores data objects for the specified
// tenant in a bucket.
//
// NewBuilder returns an error if BuilderConfig is invalid.
func NewBuilder(cfg BuilderConfig, bucket objstore.Bucket, tenantID string) (*Builder, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	labelCache, err := lru.New[string, labels.Labels](5000)
	if err != nil {
		return nil, fmt.Errorf("failed to create LRU cache: %w", err)
	}

	var (
		metrics = newMetrics()

		flushBuffer = bytes.NewBuffer(make([]byte, 0, int(cfg.TargetObjectSize)))
		encoder     = encoding.NewEncoder(flushBuffer)
	)
	metrics.ObserveConfig(cfg)

	return &Builder{
		cfg:      cfg,
		metrics:  metrics,
		bucket:   bucket,
		tenantID: tenantID,

		labelCache: labelCache,

		streams: streams.New(metrics.streams, int(cfg.TargetPageSize)),
		logs:    logs.New(metrics.logs, int(cfg.TargetPageSize)),

		flushBuffer: flushBuffer,
		encoder:     encoder,
	}, nil
}

// Append buffers a stream to be written to a data object. Append returns an
// error if the stream labels cannot be parsed or [ErrBufferFull] if the
// builder is full.
//
// Once a Builder is full, call [Builder.Flush] to flush the buffered data,
// then call Append again with the same entry.
func (b *Builder) Append(stream logproto.Stream) error {
	// Don't allow appending to a builder that has data to be flushed.
	if b.state == builderStateFlush {
		return ErrBufferFull
	}

	ls, err := b.parseLabels(stream.Labels)
	if err != nil {
		return err
	}

	// Check whether the buffer is full before a stream can be appended; this is
	// tends to overestimate, but we may still go over our target size.
	//
	// Since this check only happens after the first call to Append,
	// b.currentSizeEstimate will always be updated to reflect the size following
	// the previous append.
	if b.state != builderStateEmpty && b.currentSizeEstimate+labelsEstimate(ls)+streamSizeEstimate(stream) > int(b.cfg.TargetObjectSize) {
		return ErrBufferFull
	}

	timer := prometheus.NewTimer(b.metrics.appendTime)
	defer timer.ObserveDuration()

	for _, entry := range stream.Entries {
		streamID := b.streams.Record(ls, entry.Timestamp)

		b.logs.Append(logs.Record{
			StreamID:  streamID,
			Timestamp: entry.Timestamp,
			Metadata:  entry.StructuredMetadata,
			Line:      entry.Line,
		})
	}

	b.currentSizeEstimate = b.estimatedSize()
	b.state = builderStateDirty
	return nil
}

func (b *Builder) parseLabels(labelString string) (labels.Labels, error) {
	labels, ok := b.labelCache.Get(labelString)
	if ok {
		return labels, nil
	}

	labels, err := syntax.ParseLabels(labelString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse labels: %w", err)
	}
	b.labelCache.Add(labelString, labels)
	return labels, nil
}

func (b *Builder) estimatedSize() int {
	var size int
	size += b.streams.EstimatedSize()
	size += b.logs.EstimatedSize()
	b.metrics.sizeEstimate.Set(float64(size))
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
//
// If Flush builds an object but fails to upload it to object storage, the
// built object is cached and can be retried. [Builder.Reset] can be called to
// discard any pending data and allow new data to be appended.
func (b *Builder) Flush(ctx context.Context) error {
	switch b.state {
	case builderStateEmpty:
		return nil // Nothing to flush
	case builderStateDirty:
		if err := b.buildObject(); err != nil {
			return fmt.Errorf("building object: %w", err)
		}
		b.state = builderStateFlush
	}

	timer := prometheus.NewTimer(b.metrics.flushTime)
	defer timer.ObserveDuration()

	sum := sha256.Sum224(b.flushBuffer.Bytes())
	sumStr := hex.EncodeToString(sum[:])

	objectPath := fmt.Sprintf("tenant-%s/objects/%s/%s", b.tenantID, sumStr[:b.cfg.SHAPrefixSize], sumStr[b.cfg.SHAPrefixSize:])
	if err := b.bucket.Upload(ctx, objectPath, bytes.NewReader(b.flushBuffer.Bytes())); err != nil {
		return err
	}

	b.Reset()
	return nil
}

func (b *Builder) buildObject() error {
	timer := prometheus.NewTimer(b.metrics.buildTime)
	defer timer.ObserveDuration()

	// We reset after a successful flush, but we also reset the buffer before
	// building for safety.
	b.flushBuffer.Reset()

	if err := b.streams.EncodeTo(b.encoder); err != nil {
		return fmt.Errorf("encoding streams: %w", err)
	} else if err := b.logs.EncodeTo(b.encoder); err != nil {
		return fmt.Errorf("encoding logs: %w", err)
	} else if err := b.encoder.Flush(); err != nil {
		return fmt.Errorf("encoding object: %w", err)
	}

	// We pass context.Background() below to avoid allowing building an object to
	// time out; timing out on build would discard anything we built and would
	// cause data loss.
	dec := encoding.ReadSeekerDecoder(bytes.NewReader(b.flushBuffer.Bytes()))
	return b.metrics.encoding.Observe(context.Background(), dec)
}

// Reset discards pending data and resets the builder to an empty state.
func (b *Builder) Reset() {
	b.logs.Reset()
	b.streams.Reset()

	b.state = builderStateEmpty
	b.flushBuffer.Reset()
	b.metrics.sizeEstimate.Set(0)
}

// RegisterMetrics registers metrics about builder to report to reg. All
// metrics will have a tenant label set to the tenant ID of the Builder.
//
// If multiple Builders for the same tenant are running in the same process,
// reg must contain additional labels to differentiate between them.
func (b *Builder) RegisterMetrics(reg prometheus.Registerer) error {
	reg = prometheus.WrapRegistererWith(prometheus.Labels{"tenant": b.tenantID}, reg)
	return b.metrics.Register(reg)
}

// UnregisterMetrics unregisters metrics about builder from reg.
func (b *Builder) UnregisterMetrics(reg prometheus.Registerer) {
	reg = prometheus.WrapRegistererWith(prometheus.Labels{"tenant": b.tenantID}, reg)
	b.metrics.Unregister(reg)
}
