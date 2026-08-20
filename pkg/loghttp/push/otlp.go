package push

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/klauspost/compress/zstd"
	"github.com/pierrec/lz4/v4"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"

	"github.com/grafana/loki/v3/pkg/loghttp/push/otlpattrs"
	"github.com/grafana/loki/v3/pkg/loghttp/push/otlplabels"
	"github.com/grafana/loki/v3/pkg/util/constants"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/runtime"
	loki_util "github.com/grafana/loki/v3/pkg/util"
)

const (
	pbContentType       = "application/x-protobuf"
	gzipContentEncoding = "gzip"
	zstdContentEncoding = "zstd"
	lz4ContentEncoding  = "lz4"

	OTLPSeverityNumber = otlplabels.OTLPSeverityNumber
	OTLPSeverityText   = otlplabels.OTLPSeverityText
	OTLPEventName      = otlplabels.OTLPEventName

	messageSizeLargerErrFmt = "%w than max (%d vs %d)"
)

func ParseOTLPRequest(userID string, r *http.Request, limits Limits, tenantConfigs *runtime.TenantConfigs, maxRecvMsgSize int, maxDecompressedSize int64, tracker UsageTracker, streamResolver StreamResolver, logger log.Logger, deferExpansion bool) (*logproto.InternalPushRequest, *Stats, error) {
	stats := NewPushStats()
	otlpLogs, err := extractLogs(r, maxRecvMsgSize, maxDecompressedSize, stats)
	if err != nil {
		return nil, nil, err
	}

	req, err := otlpToLokiPushRequest(r.Context(), otlpLogs, userID, limits.OTLPConfig(userID), tenantConfigs, limits.DiscoverServiceName(userID), tracker, stats, logger, streamResolver, constants.OTLP, deferExpansion)
	return req, stats, err
}

func extractLogs(r *http.Request, maxRecvMsgSize int, maxDecompressedSize int64, pushStats *Stats) (plog.Logs, error) {
	pushStats.ContentEncoding = r.Header.Get(contentEnc)
	// bodySize should always reflect the compressed size of the request body
	bodySize := loki_util.NewSizeReader(r.Body)
	var body io.Reader = bodySize
	if maxRecvMsgSize > 0 {
		// Read from LimitReader with limit max+1. So if the underlying
		// reader is over limit, the result will be bigger than max.
		body = io.LimitReader(bodySize, int64(maxRecvMsgSize)+1)
	}
	switch pushStats.ContentEncoding {
	case gzipContentEncoding:
		r, err := gzip.NewReader(body)
		if err != nil {
			return plog.NewLogs(), err
		}
		body = r
		defer func(reader *gzip.Reader) {
			_ = reader.Close()
		}(r)
		if maxDecompressedSize > 0 {
			body = io.LimitReader(body, maxDecompressedSize+1)
		}

	case zstdContentEncoding:
		var err error
		body, err = zstd.NewReader(body)
		if err != nil {
			return plog.NewLogs(), err
		}
		if maxDecompressedSize > 0 {
			body = io.LimitReader(body, maxDecompressedSize+1)
		}
	case lz4ContentEncoding:
		body = io.NopCloser(lz4.NewReader(body))
		if maxDecompressedSize > 0 {
			body = io.LimitReader(body, maxDecompressedSize+1)
		}
	case "":
		// no content encoding, use the body as is
	default:
		return plog.NewLogs(), errors.Errorf("unsupported content encoding %s: only gzip, lz4 and zstd are supported", pushStats.ContentEncoding)
	}
	buf, err := io.ReadAll(body)
	if err != nil {
		return plog.NewLogs(), err
	}

	// Check the size of the compressed body
	if size := bodySize.Size(); size > int64(maxRecvMsgSize) && maxRecvMsgSize > 0 {
		return plog.NewLogs(), fmt.Errorf(messageSizeLargerErrFmt, loki_util.ErrMessageSizeTooLarge, size, maxRecvMsgSize)
	}
	// Check the size of the decompressed body
	if int64(len(buf)) > maxDecompressedSize && maxDecompressedSize > 0 {
		return plog.NewLogs(), fmt.Errorf(messageSizeLargerErrFmt, loki_util.ErrMessageDecompressedSizeTooLarge, len(buf), maxDecompressedSize)
	}

	pushStats.BodySize = bodySize.Size()

	req := plogotlp.NewExportRequest()

	pushStats.ContentType = r.Header.Get(contentType)
	switch pushStats.ContentType {
	case pbContentType:
		err := req.UnmarshalProto(buf)
		if err != nil {
			return plog.NewLogs(), err
		}
	case applicationJSON:
		err := req.UnmarshalJSON(buf)
		if err != nil {
			return plog.NewLogs(), err
		}
	default:
		return plog.NewLogs(),
			errors.Errorf(
				"content type: %s is not supported",
				r.Header.Get("Content-Type"),
			)
	}

	return req.Logs(), nil
}

// deferExpansion decides the shape this produces. When false, the resource and scope
// attributes are copied onto every entry and each stream comes out as a single group with
// nothing shared — byte for byte what the flattened form has always carried. When true, each
// attribute set is recorded once on the group that owns it.
func otlpToLokiPushRequest(ctx context.Context, ld plog.Logs, userID string, otlpConfig OTLPConfig, tenantConfigs *runtime.TenantConfigs, discoverServiceName []string, tracker UsageTracker, stats *Stats, logger log.Logger, streamResolver StreamResolver, format string, deferExpansion bool) (*logproto.InternalPushRequest, error) {
	if ld.LogRecordCount() == 0 {
		return &logproto.InternalPushRequest{}, nil
	}

	rls := ld.ResourceLogs()
	pushRequestsByStream := make(map[string]*streamBuilder, rls.Len())

	// Track if request used the Loki OTLP exporter label
	var usingLokiExporter bool

	logServiceNameDiscovery := false
	logPushRequestStreams := false
	logOTLPAttributeExpansion := false
	if tenantConfigs != nil {
		logServiceNameDiscovery = tenantConfigs.LogServiceNameDiscovery(userID)
		logPushRequestStreams = tenantConfigs.LogPushRequestStreams(userID)
		logOTLPAttributeExpansion = tenantConfigs.LogOTLPAttributeExpansion(userID)
	}

	var attrAccumulator *otlpattrs.Accumulator
	if logOTLPAttributeExpansion {
		attrAccumulator = otlpattrs.NewAccumulator()
		stats.OTLPAttributes = attrAccumulator
	}

	// If this is a backfill push (X-Loki-Backfill-Shard header), every stream gets the internal
	// backfill labels added below. Done here (not via OTLP attribute promotion) so a tenant's OTLP
	// config cannot drop them.
	backfillShard := ExtractBackfillShardContext(ctx)

	mostRecentEntryTimestamp := time.Time{}
	for i := 0; i < rls.Len(); i++ {
		sls := rls.At(i).ScopeLogs()
		res := rls.At(i).Resource()
		resAttrs := res.Attributes()

		resourceRecords := 0
		resResult, err := otlplabels.ResourceAttrsToStreamLabels(resAttrs, otlpConfig, discoverServiceName)
		if err != nil {
			return nil, err
		}

		resourceAttributesAsStructuredMetadata := resResult.StructuredMetadata
		streamLabels := resResult.StreamLabels

		// The backfill labels are reserved for Loki: they may only be added below, from the
		// X-Loki-Backfill-Shard header, so clients cannot spoof them to bypass validation.
		if hasReservedBackfillLabels(streamLabels) {
			return nil, errReservedBackfillLabels()
		}

		if backfillShard != "" {
			streamLabels[constants.BackfillLabel] = "true"
			streamLabels[constants.BackfillShardLabel] = model.LabelValue(backfillShard)
		}

		var pushedLabels model.LabelSet
		if logServiceNameDiscovery {
			pushedLabels = make(model.LabelSet, len(streamLabels))
			for k, v := range streamLabels {
				pushedLabels[k] = v
			}
		}

		// this must be pushed to the end after log lines are also evaluated
		if logServiceNameDiscovery {
			var sb strings.Builder
			sb.WriteString("{")
			labels := make([]string, 0, len(pushedLabels))
			for name, value := range pushedLabels {
				labels = append(labels, fmt.Sprintf(`%s="%s"`, name, value))
			}
			sb.WriteString(strings.Join(labels, ", "))
			sb.WriteString("}")

			level.Debug(logger).Log(
				"msg", "OTLP push request stream before service name discovery",
				"stream", sb.String(),
				"service_name", streamLabels[model.LabelName(LabelServiceName)],
			)
		}

		if err := streamLabels.Validate(); err != nil {
			stats.Errs = append(stats.Errs, fmt.Errorf("invalid labels: %w", err))
			continue
		}
		labelsStr := streamLabels.String()

		if len(labelsStr) > maxStreamLabelsSize {
			return nil, fmt.Errorf("%w: stream labels size %s exceeds limit of %s", ErrRequestBodyTooLarge, humanize.Bytes(uint64(len(labelsStr))), humanize.Bytes(maxStreamLabelsSize))
		}

		lbs := modelLabelsSetToLabelsList(streamLabels)
		totalBytesReceived := int64(0)

		// Create a stream with the resource labels if there are any
		if len(streamLabels) > 0 {
			if _, ok := pushRequestsByStream[labelsStr]; !ok {
				pushRequestsByStream[labelsStr] = &streamBuilder{labels: labelsStr}
				stats.StreamLabelsSize += int64(labelsSize(logproto.FromLabelsToLabelAdapters(lbs)))
			}
		}

		// Calculate resource attributes metadata size for stats
		resourceAttributesAsStructuredMetadataSize := int64(loki_util.StructuredMetadataSize(resourceAttributesAsStructuredMetadata))
		retentionPeriodForUser := streamResolver.RetentionPeriodFor(lbs)
		policy := streamResolver.PolicyFor(ctx, lbs)

		// Check if the stream has the exporter=OTLP label; set flag instead of incrementing per stream
		if value, ok := streamLabels[model.LabelName("exporter")]; ok && value == "OTLP" {
			usingLokiExporter = true
		}

		if _, ok := stats.StructuredMetadataBytes[policy]; !ok {
			stats.StructuredMetadataBytes[policy] = make(map[time.Duration]int64)
		}

		// We group by retention period to later be able to map bytes ingested to each retention period.
		// Ex: 10GB ingested has 30d retention and 1GB has 365d retention.
		stats.StructuredMetadataBytes[policy][retentionPeriodForUser] += resourceAttributesAsStructuredMetadataSize
		totalBytesReceived += resourceAttributesAsStructuredMetadataSize

		for j := 0; j < sls.Len(); j++ {
			logs := sls.At(j).LogRecords()

			scopeRecords := 0

			scopeResult, err := otlplabels.ScopeAttrsToStructuredMetadata(sls, j, otlpConfig)
			if err != nil {
				return nil, err
			}
			scopeAttributesAsStructuredMetadata := scopeResult.StructuredMetadata

			scopeAttributesAsStructuredMetadataSize := int64(loki_util.StructuredMetadataSize(scopeAttributesAsStructuredMetadata))
			stats.StructuredMetadataBytes[policy][retentionPeriodForUser] += scopeAttributesAsStructuredMetadataSize
			totalBytesReceived += scopeAttributesAsStructuredMetadataSize

			for k := 0; k < logs.Len(); k++ {
				log := logs.At(k)

				// Use the existing function that already handles log attributes properly
				logLabels, entry, err := otlpLogToPushEntry(log, otlpConfig, logServiceNameDiscovery, pushedLabels)
				if err != nil {
					return nil, err
				}
				if entry.Timestamp.After(mostRecentEntryTimestamp) {
					mostRecentEntryTimestamp = entry.Timestamp
				}

				// Combine resource labels with log labels if any log attributes were indexed
				var entryLabelsStr string
				var entryLbs labels.Labels

				if len(logLabels) > 0 {
					// Log attributes promoted to index labels must not smuggle in the reserved
					// backfill labels either (they would overwrite the header-injected ones).
					if hasReservedBackfillLabels(logLabels) {
						return nil, errReservedBackfillLabels()
					}

					// Combine resource labels with log attributes
					combinedLabels := make(model.LabelSet, len(streamLabels)+len(logLabels))
					for k, v := range streamLabels {
						combinedLabels[k] = v
					}
					for k, v := range logLabels {
						combinedLabels[k] = v
					}

					if err := combinedLabels.Validate(); err != nil {
						stats.Errs = append(stats.Errs, fmt.Errorf("invalid labels with log attributes: %w", err))
						continue
					}

					entryLabelsStr = combinedLabels.String()
					if len(entryLabelsStr) > maxStreamLabelsSize {
						return nil, fmt.Errorf("%w: stream labels size %s exceeds limit of %s", ErrRequestBodyTooLarge, humanize.Bytes(uint64(len(entryLabelsStr))), humanize.Bytes(maxStreamLabelsSize))
					}
					entryLbs = modelLabelsSetToLabelsList(combinedLabels)

					if _, ok := pushRequestsByStream[entryLabelsStr]; !ok {
						pushRequestsByStream[entryLabelsStr] = &streamBuilder{labels: entryLabelsStr}
						stats.StreamLabelsSize += int64(labelsSize(logproto.FromLabelsToLabelAdapters(entryLbs)))
					}
				} else {
					entryLabelsStr = labelsStr
					entryLbs = lbs
				}

				// Calculate the entry's own metadata size BEFORE adding resource and scope attributes
				// This preserves the intent of tracking entry-specific metadata separately without requiring subtraction
				entryOwnMetadataSize := int64(loki_util.StructuredMetadataSize(entry.StructuredMetadata))

				if deferExpansion {
					// The resource and scope attributes are not copied onto the entry. They
					// are recorded once on the group the entry is placed in, and every reader
					// recovers them through logproto.AppendEffectiveMetadata.
					pushRequestsByStream[entryLabelsStr].append(
						i, j,
						resourceAttributesAsStructuredMetadata,
						scopeAttributesAsStructuredMetadata,
						entry, logs.Len(),
					)
				} else {
					// Copy them onto the entry, as before. Everything then lands in one group
					// with no shared attributes, so the two size measures agree, containment
					// order is arrival order, and a time shard sorts the whole stream — all
					// exactly as they do today.
					attributesAsStructuredMetadataLen := len(resourceAttributesAsStructuredMetadata) + len(scopeAttributesAsStructuredMetadata)
					if cap(entry.StructuredMetadata) < len(entry.StructuredMetadata)+attributesAsStructuredMetadataLen {
						structuredMetadata := make(push.LabelsAdapter, 0, len(entry.StructuredMetadata)+attributesAsStructuredMetadataLen)
						structuredMetadata = append(structuredMetadata, entry.StructuredMetadata...)
						entry.StructuredMetadata = structuredMetadata
					}
					entry.StructuredMetadata = append(entry.StructuredMetadata, resourceAttributesAsStructuredMetadata...)
					entry.StructuredMetadata = append(entry.StructuredMetadata, scopeAttributesAsStructuredMetadata...)

					pushRequestsByStream[entryLabelsStr].append(0, 0, nil, nil, entry, logs.Len())
				}
				scopeRecords++

				entryRetentionPeriod := streamResolver.RetentionPeriodFor(entryLbs)
				entryPolicy := streamResolver.PolicyFor(ctx, entryLbs)

				if _, ok := stats.StructuredMetadataBytes[entryPolicy]; !ok {
					stats.StructuredMetadataBytes[entryPolicy] = make(map[time.Duration]int64)
				}
				// Use the entry's own metadata size (calculated before adding resource/scope attributes)
				// This keeps the same accounting intention without risk of negative values
				stats.StructuredMetadataBytes[entryPolicy][entryRetentionPeriod] += entryOwnMetadataSize

				lineSize := int64(len(entry.Line))
				if _, ok := stats.LogLinesBytes[entryPolicy]; !ok {
					stats.LogLinesBytes[entryPolicy] = make(map[time.Duration]int64)
				}
				stats.LogLinesBytes[entryPolicy][entryRetentionPeriod] += lineSize

				// Track the expanded entry size including the resource and scope attributes that are copied into the entry's structured metadata.
				// This is the actual size of the entry that will be ingested.
				stats.TotalExpandedEntriesSize += lineSize + entryOwnMetadataSize + resourceAttributesAsStructuredMetadataSize + scopeAttributesAsStructuredMetadataSize

				totalBytesReceived += entryOwnMetadataSize
				totalBytesReceived += lineSize

				stats.PolicyNumLines[entryPolicy]++
				if entry.Timestamp.After(stats.MostRecentEntryTimestamp) {
					stats.MostRecentEntryTimestamp = entry.Timestamp
				}

				if tracker != nil && len(logLabels) > 0 {
					tracker.ReceivedBytesAdd(ctx, userID, entryRetentionPeriod, entryLbs, float64(totalBytesReceived), format)
				}
			}

			if attrAccumulator != nil {
				attrAccumulator.IncRecords(scopeRecords)
				attrAccumulator.Observe(otlpattrs.KindScope, scopeAttributesAsStructuredMetadata, scopeRecords)
			}
			resourceRecords += scopeRecords

			if tracker != nil {
				tracker.ReceivedBytesAdd(ctx, userID, retentionPeriodForUser, lbs, float64(totalBytesReceived), format)
			}
		}

		if attrAccumulator != nil {
			attrAccumulator.Observe(otlpattrs.KindResource, resourceAttributesAsStructuredMetadata, resourceRecords)
		}
	}

	stats.MostRecentEntryTimestamp = mostRecentEntryTimestamp

	pr := &logproto.InternalPushRequest{
		Streams: make([]logproto.InternalStreamAdapter, 0, len(pushRequestsByStream)),
	}

	// Include all streams that have entries or have labels
	for _, builder := range pushRequestsByStream {
		stream := builder.stream()
		if stream.EntryCount() > 0 || len(stream.Labels) > 0 {
			pr.Streams = append(pr.Streams, stream)
		}
		if logPushRequestStreams {
			mostRecentEntryTimestamp := time.Time{}
			streamSizeBytes := int64(0)
			// It's difficult to calculate these values inline when we process the payload because promotion of resource attributes or log attributes to labels can change the stream with each entry.
			// So for simplicity and because this logging is typically disabled, we iterate on the entries to calculate these values here.
			//
			// The size counts each entry's effective metadata, resource and scope
			// attributes included, so the number logged is the same as when those
			// attributes were copied onto every entry.
			for i := range stream.ResourceLogs {
				res := &stream.ResourceLogs[i]
				for j := range res.ScopeLogs {
					scope := &res.ScopeLogs[j]
					for k := range scope.Entries {
						entry := &scope.Entries[k]
						streamSizeBytes += int64(len(entry.Line)) + int64(logproto.EffectiveMetadataSize(res.Attrs, scope.Attrs, entry))
						if entry.Timestamp.After(mostRecentEntryTimestamp) {
							mostRecentEntryTimestamp = entry.Timestamp
						}
					}
				}
			}
			stats.MostRecentEntryTimestampPerStream[stream.Labels] = mostRecentEntryTimestamp
			stats.StreamSizeBytes[stream.Labels] = streamSizeBytes
		}
	}

	// Increment exporter streams metric once per request if seen
	if usingLokiExporter {
		otlpExporterStreams.WithLabelValues(userID).Inc()
	}

	return pr, nil
}

// otlpLogToPushEntry converts an OTLP log record to a Loki push.Entry.
func otlpLogToPushEntry(log plog.LogRecord, otlpConfig OTLPConfig, logServiceNameDiscovery bool, pushedLabels model.LabelSet) (model.LabelSet, push.Entry, error) {
	logResult, err := otlplabels.LogAttrsToLabels(log, otlpConfig)
	if err != nil {
		return nil, push.Entry{}, err
	}

	if logServiceNameDiscovery && pushedLabels != nil {
		for k, v := range logResult.IndexLabels {
			pushedLabels[k] = v
		}
	}

	return logResult.IndexLabels, push.Entry{
		Timestamp:          timestampFromLogRecord(log),
		Line:               log.Body().AsString(),
		StructuredMetadata: logResult.StructuredMetadata,
	}, nil
}

func timestampFromLogRecord(lr plog.LogRecord) time.Time {
	if lr.Timestamp() != 0 {
		return time.Unix(0, int64(lr.Timestamp()))
	}

	if lr.ObservedTimestamp() != 0 {
		return time.Unix(0, int64(lr.ObservedTimestamp()))
	}

	return time.Unix(0, time.Now().UnixNano())
}

func labelsSize(lbls push.LabelsAdapter) int {
	size := 0
	for _, lbl := range lbls {
		size += len(lbl.Name) + len(lbl.Value)
	}

	return size
}

func modelLabelsSetToLabelsList(m model.LabelSet) labels.Labels {
	builder := labels.NewScratchBuilder(len(m))
	for lName, lValue := range m {
		builder.Add(string(lName), string(lValue))
	}
	builder.Sort()
	return builder.Labels()
}

// streamBuilder accumulates one stream's entries, grouped by the resource and the scope they
// arrived under so that each attribute set is stored once rather than on every entry.
//
// OTLP is walked resource-major then scope-major, so the entries destined for any one stream
// arrive in contiguous runs and the builder only ever appends to the group it opened last.
// It has to be per stream rather than per resource because promoting a log attribute to an
// index label moves an individual entry to a different stream, so one resource can feed many
// streams and one stream can be fed by many resources.
type streamBuilder struct {
	labels   string
	groups   []logproto.ResourceLogs
	resIdx   int
	scopeIdx int
	open     bool
}

// append places an entry under the given resource and scope, opening a new group or scope
// when the walk has moved on from the last one. prealloc is a hint for how many entries the
// scope holds, which is exact when no log attribute is promoted.
//
// The attribute slices are stored, not copied: several streams can reference the same
// resource's attributes, so a later pass must copy before rewriting them.
func (b *streamBuilder) append(resIdx, scopeIdx int, resAttrs, scopeAttrs push.LabelsAdapter, entry push.Entry, prealloc int) {
	switch {
	case !b.open || b.resIdx != resIdx:
		b.groups = append(b.groups, logproto.ResourceLogs{
			Attrs: resAttrs,
			ScopeLogs: []logproto.ScopeLogs{{
				Attrs:   scopeAttrs,
				Entries: make([]push.Entry, 0, prealloc),
			}},
		})
		b.resIdx, b.scopeIdx, b.open = resIdx, scopeIdx, true

	case b.scopeIdx != scopeIdx:
		group := &b.groups[len(b.groups)-1]
		group.ScopeLogs = append(group.ScopeLogs, logproto.ScopeLogs{
			Attrs:   scopeAttrs,
			Entries: make([]push.Entry, 0, prealloc),
		})
		b.scopeIdx = scopeIdx
	}

	group := &b.groups[len(b.groups)-1]
	scope := &group.ScopeLogs[len(group.ScopeLogs)-1]
	scope.Entries = append(scope.Entries, entry)
}

func (b *streamBuilder) stream() logproto.InternalStreamAdapter {
	return logproto.InternalStreamAdapter{
		Labels:       b.labels,
		ResourceLogs: b.groups,
	}
}
