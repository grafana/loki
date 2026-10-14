package fixtures

import (
	"io"
	"slices"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/scratch"
)

var defaultLogSectionConfig = logSectionConfig{
	builderOpts: &logs.BuilderOptions{
		PageSizeHint:              1024 * 1024,
		PageMaxRowCount:           10000,
		BufferSize:                16 * 1024 * 1024,
		StripeMergeLimit:          2,
		AppendStrategy:            logs.AppendOrdered,
		EstimatedCompressionRatio: 10,
		SortOrder:                 logs.SortStreamASC,
		SchemaLabels:              []string{"label:service_name"},
		StreamOrder:               logs.StreamOrderStableHashV1,
		ShardCount:                streams.ShardFactor,
		DropDuplicates:            false, // never modify the requested input
	},
}

// WithBuilderOptions enables customization of the builder options for the logs section.
func WithBuilderOptions(builderOptions *logs.BuilderOptions) LogSectionOpt {
	return func(opts *logSectionConfig) {
		opts.builderOpts = builderOptions
	}
}

type LogSectionOpt func(*logSectionConfig)

type logSectionConfig struct {
	builderOpts *logs.BuilderOptions
}

// WithSchemaLabels computes the schema sort key for each log record from its
// stream labels. Without this option, SchemaKey is left unset.
func WithSchemaLabels(schemaLabels ...string) LogFixtureOpt {
	return func(b *LogFixtureBuilder) {
		b.schemaLabels = slices.Clone(schemaLabels)
	}
}

// LogFixtureOpt customizes a LogFixtureBuilder.
type LogFixtureOpt func(*LogFixtureBuilder)

// LogFixtureBuilder builds matching stream and log records for section fixtures.
// IDs are assigned in first-seen stream order; log records retain insertion order.
// Sort keys are computed while building each record; they are used to define order but are not persisted.
type LogFixtureBuilder struct {
	t             *testing.T
	schemaLabels  []string
	streamIndexes map[string]int
	streams       []streams.Stream
	logs          []logs.Record
}

// NewLogsFixtureBuilder returns a fluid builder for creating matching sets of Streams and Log Records, with StreamIDs assigned automatically.
// IDs are assigned in first-seen stream order and log records are returned in insertion order.
//
// Callers may use either the ForStream(labelString).Entry(ts, md, msg) syntax or the more verbose Entry(stream, ts, md, msg) syntax.
// Use WithSchemaLabels to choose the schema key used while building records.
// The physical shard bucket always comes from the stable hash of stream labels.
func NewLogsFixtureBuilder(t *testing.T, opts ...LogFixtureOpt) *LogFixtureBuilder {
	b := &LogFixtureBuilder{
		t: t,
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

type StreamFixture struct {
	labels labels.Labels
	parent *LogFixtureBuilder
}

// ForStream returns a pre-parsed helper which enables easier addition of log messages for the same stream
func (b *LogFixtureBuilder) ForStream(labels string) *StreamFixture {
	lbs, err := syntax.ParseLabels(labels)
	require.NoError(b.t, err)

	return &StreamFixture{
		labels: lbs,
		parent: b,
	}
}

// Entry adds a single log message to the dataset using the pre-defined stream and provided timestamp and structured metadata.
func (f *StreamFixture) Entry(tsSeconds int, structuredMetadata string, logMessage string) *StreamFixture {
	f.parent.Entry(f.labels, tsSeconds, structuredMetadata, logMessage)
	return f
}

// Entry adds a single log message to the dataset with the provided stream, timestamp, and structured metadata.
func (b *LogFixtureBuilder) Entry(stream labels.Labels, tsSeconds int, structuredMetadata string, logMessage string) {
	if b.streamIndexes == nil {
		b.streamIndexes = make(map[string]int)
	}
	ts := time.Unix(int64(tsSeconds), 0).UTC()
	key := stream.String()
	streamHash := labels.StableHash(stream)
	index, ok := b.streamIndexes[key]
	if !ok {
		index = len(b.streams)
		b.streamIndexes[key] = index
		b.streams = append(b.streams, streams.Stream{
			ID: int64(index + 1), Labels: stream,
			MinTimestamp: ts, MaxTimestamp: ts, ShardBucket: int64(streams.ShardBucketFromHash(streamHash)),
		})
	}
	s := &b.streams[index]
	if ts.Before(s.MinTimestamp) {
		s.MinTimestamp = ts
	}
	if ts.After(s.MaxTimestamp) {
		s.MaxTimestamp = ts
	}
	s.Rows++
	s.UncompressedSize += int64(len(logMessage))
	smLabels, err := syntax.ParseLabels(structuredMetadata)
	require.NoError(b.t, err)
	smLabels.Range(func(l labels.Label) { s.UncompressedSize += int64(len(l.Value)) })

	var schemaKey string
	if b.schemaLabels != nil {
		schemaKey, err = logsobj.ComputeSchemaKey(stream, b.schemaLabels)
		require.NoError(b.t, err)
	}
	b.logs = append(b.logs, logs.Record{
		StreamID: s.ID, Timestamp: ts, Metadata: smLabels, Line: []byte(logMessage),
		SchemaKey: schemaKey, ShardBucket: uint32(s.ShardBucket), StreamHash: streamHash,
	})
}

// Logs returns the raw logs.Records held by this builder. They can be passed directly to LogsSection to build a dataobject section.
func (b *LogFixtureBuilder) Logs() []logs.Record {
	return b.logs
}

// Streams returns the raw streams.Streams held by this builder. They can be passed directly to StreamsSection to build a dataobject section.
func (b *LogFixtureBuilder) Streams() []streams.Stream {
	return b.streams
}

// LogsSection returns a logs.Builder populated with the given rows and assigned the specified tenant
// Additional opts control the construction of the Logs section. If no Opts are provided, suitable defaults are used.
// The default log ordering is SortStreamASC (StreamID ASC, Timestamp DESC)
func LogsSection(t *testing.T, tenant string, rows []logs.Record, opts ...LogSectionOpt) dataobj.SectionBuilder {
	t.Helper()
	config := defaultLogSectionConfig
	for _, opt := range opts {
		opt(&config)
	}

	sectionBuilder := logs.NewBuilder(logs.NewMetrics(), *config.builderOpts)
	sectionBuilder.SetTenant(tenant)

	for _, record := range rows {
		sectionBuilder.Append(record)
	}

	return sectionBuilder
}

type streamSectionConfig struct {
	pageSize     int
	pageRowCount int
}

var defaultStreamSectionConfig = streamSectionConfig{
	pageSize:     1024 * 1024, // 1MB
	pageRowCount: 10000,
}

// StreamsSection returns a streams.Builder populated with the given streams and assigned the specified tenant.
func StreamsSection(t *testing.T, tenant string, labelSets []streams.Stream) dataobj.SectionBuilder {
	t.Helper()

	sectionBuilder := streams.NewBuilder(streams.NewMetrics(), defaultStreamSectionConfig.pageSize, defaultStreamSectionConfig.pageRowCount)
	sectionBuilder.SetTenant(tenant)

	for _, stream := range labelSets {
		sectionBuilder.AppendValue(stream)
	}

	return sectionBuilder
}

// ReadTenantStreams reads the tenant's streams across all streams sections in
// object order. Returned labels are copied so the streams remain valid after
// the iterator advances.
func ReadTenantStreams(t *testing.T, obj *dataobj.Object, tenant string) []streams.Stream {
	t.Helper()
	var result []streams.Stream
	for _, section := range obj.Sections().Filter(streams.CheckSection) {
		if section.Tenant != tenant {
			continue
		}
		opened, err := streams.Open(t.Context(), section)
		require.NoError(t, err)
		for item := range streams.IterSection(t.Context(), opened) {
			stream, err := item.Value()
			require.NoError(t, err)
			stream.Labels = stream.Labels.Copy()
			result = append(result, stream)
		}
	}
	return result
}

// ReadTenantLogs reads the tenant's records across all logs sections in object
// order, preserving their physical read order. Records are copied because the
// logs iterator reuses its buffers.
func ReadTenantLogs(t *testing.T, obj *dataobj.Object, tenant string) []logs.Record {
	t.Helper()
	var result []logs.Record
	for i, section := range obj.Sections().Filter(logs.CheckSection) {
		if section.Tenant != tenant {
			continue
		}
		result = append(result, ReadTenantLogSection(t, obj, tenant, i)...)
	}
	return result
}

// ReadTenantLogSection reads one logs-relative section in physical row order.
// The index counts every logs section in the object, including other tenants'.
// Returned records are independent of the iterator's reused buffers.
func ReadTenantLogSection(t *testing.T, obj *dataobj.Object, tenant string, sectionIndex int) []logs.Record {
	t.Helper()
	for i, section := range obj.Sections().Filter(logs.CheckSection) {
		if i != sectionIndex {
			continue
		}
		require.Equal(t, tenant, section.Tenant, "logs section %d belongs to another tenant", sectionIndex)
		opened, err := logs.Open(t.Context(), section)
		require.NoError(t, err)
		var records []logs.Record
		for item := range logs.IterSection(t.Context(), opened) {
			record, err := item.Value()
			require.NoError(t, err)
			records = append(records, record.Copy())
		}
		return records
	}
	t.Fatalf("logs section %d does not exist", sectionIndex)
	return nil
}

// DataObject returns a dataobj.Object populated with the given sections.
func DataObject(t *testing.T, sections ...dataobj.SectionBuilder) (*dataobj.Object, io.Closer) {
	t.Helper()
	objBuilder := dataobj.NewBuilder(scratch.NewMemory())
	for _, section := range sections {
		err := objBuilder.Append(section)
		require.NoError(t, err)
	}

	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	return obj, closer
}

// StoredDataObject builds a data object from the given sections, writes it to bucket, and returns
// the path it was written to.
func StoredDataObject(t *testing.T, bucket objstore.Bucket, sections ...dataobj.SectionBuilder) string {
	t.Helper()

	obj, closer := DataObject(t, sections...)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	path, err := uploader.New(uploader.Config{SHAPrefixSize: 2}, bucket, log.NewNopLogger()).Upload(t.Context(), obj)
	require.NoError(t, err)
	return path
}
