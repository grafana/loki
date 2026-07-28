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

func ParseOTLPRequest(userID string, r *http.Request, limits Limits, tenantConfigs *runtime.TenantConfigs, maxRecvMsgSize int, maxDecompressedSize int64, tracker UsageTracker, streamResolver StreamResolver, logger log.Logger) (*logproto.PushRequest, *Stats, error) {
	stats := NewPushStats()
	otlpLogs, err := extractLogs(r, maxRecvMsgSize, maxDecompressedSize, stats)
	if err != nil {
		return nil, nil, err
	}

	req, err := otlpToLokiPushRequest(r.Context(), otlpLogs, userID, limits.OTLPConfig(userID), tenantConfigs, limits.DiscoverServiceName(userID), limits.OTLPDeferStructuredMetadataExpansion(userID), tracker, stats, logger, streamResolver, constants.OTLP)
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

// otlpStream is a wire stream being built out of an OTLP payload, together with the index used
// to deduplicate the sets of its shared structured metadata pool.
//
// Streams are grouped by their labels alone: every resource and every scope that resolves to the
// same label set lands in one stream, and their differing attributes are told apart by the pool
// rather than by splitting the stream. The pool belongs to the stream, since references are only
// meaningful next to the pool of their own stream, so a resource whose entries are split between
// its own label set and a promoted one is pooled once in each of those two streams.
type otlpStream struct {
	stream logproto.Stream
	// refsByHash maps the content hash of a pooled set to the references of every set pooled under
	// that hash. There is normally exactly one; a list is kept so that a hash collision costs a
	// linear scan over two or three candidates rather than degenerating (see ref).
	refsByHash map[uint64][]uint32
}

// ref returns the reference to attrs in the stream's pool, appending attrs to the pool the first
// time the stream sees it. An empty set is never pooled and gets the 0 "no set" reference.
//
// hash must be util.StructuredMetadataHash(attrs). It is passed in rather than computed here
// because a resource or a scope is hashed once and then referenced by each of its entries.
func (s *otlpStream) ref(attrs push.LabelsAdapter, hash uint64) uint32 {
	if len(attrs) == 0 {
		return 0
	}

	candidates := s.refsByHash[hash]
	if ref, ok := lookupSharedRef(s.stream.SharedStructuredMetadataSets, candidates, attrs); ok {
		return ref
	}

	s.stream.SharedStructuredMetadataSets = append(s.stream.SharedStructuredMetadataSets, logproto.SharedStructuredMetadataSet{Attrs: attrs})
	ref := uint32(len(s.stream.SharedStructuredMetadataSets))
	if s.refsByHash == nil {
		s.refsByHash = make(map[uint64][]uint32, 2)
	}
	// Indexed under its hash even when it collided with an already pooled set, so that the next
	// entry carrying these attributes finds it instead of pooling a third copy.
	s.refsByHash[hash] = append(candidates, ref)
	return ref
}

// lookupSharedRef finds attrs among the sets pooled under one content hash.
//
// The content is compared rather than trusted: a hash collision would otherwise hand every entry
// of one resource the attributes of an unrelated one. candidates holds more than one reference
// only when two different sets did collide, so the scan is over one element in practice.
func lookupSharedRef(sets []logproto.SharedStructuredMetadataSet, candidates []uint32, attrs push.LabelsAdapter) (uint32, bool) {
	for _, ref := range candidates {
		if ref == 0 || uint64(ref) > uint64(len(sets)) {
			continue
		}
		if sameAttrs(sets[ref-1].Attrs, attrs) {
			return ref, true
		}
	}
	return 0, false
}

func sameAttrs(a, b push.LabelsAdapter) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Value != b[i].Value {
			return false
		}
	}
	return true
}

func otlpToLokiPushRequest(ctx context.Context, ld plog.Logs, userID string, otlpConfig OTLPConfig, tenantConfigs *runtime.TenantConfigs, discoverServiceName []string, deferStructuredMetadataExpansion bool, tracker UsageTracker, stats *Stats, logger log.Logger, streamResolver StreamResolver, format string) (*logproto.PushRequest, error) {
	if ld.LogRecordCount() == 0 {
		return &logproto.PushRequest{}, nil
	}

	rls := ld.ResourceLogs()
	pushRequestsByStream := make(map[string]*otlpStream, rls.Len())

	ensureStream := func(labelsStr string, lbs labels.Labels) *otlpStream {
		s, ok := pushRequestsByStream[labelsStr]
		if !ok {
			s = &otlpStream{stream: logproto.Stream{Labels: labelsStr}}
			pushRequestsByStream[labelsStr] = s
			stats.StreamLabelsSize += int64(labelsSize(logproto.FromLabelsToLabelAdapters(lbs)))
		}
		return s
	}

	// Track if request used the Loki OTLP exporter label
	var usingLokiExporter bool

	logServiceNameDiscovery := false
	logPushRequestStreams := false
	if tenantConfigs != nil {
		logServiceNameDiscovery = tenantConfigs.LogServiceNameDiscovery(userID)
		logPushRequestStreams = tenantConfigs.LogPushRequestStreams(userID)
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

		resResult, err := otlplabels.ResourceAttrsToStreamLabels(resAttrs, otlpConfig, discoverServiceName)
		if err != nil {
			return nil, err
		}

		resourceAttributesAsStructuredMetadata := resResult.StructuredMetadata
		streamLabels := resResult.StreamLabels

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

		// When expansion is deferred, the resource attributes go into the pool of whichever
		// stream the entries of this resource end up in, and each of those entries points at
		// them. The set is hashed once here and the hash reused for every entry.
		var resourceAttrsHash uint64
		if deferStructuredMetadataExpansion {
			resourceAttrsHash = loki_util.StructuredMetadataHash(resourceAttributesAsStructuredMetadata)
		}

		// Create a stream with the resource labels if there are any
		if len(streamLabels) > 0 {
			ensureStream(labelsStr, lbs)
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

		if _, ok := stats.ResourceAndSourceMetadataLabels[policy]; !ok {
			stats.ResourceAndSourceMetadataLabels[policy] = make(map[time.Duration]push.LabelsAdapter)
		}

		stats.StructuredMetadataBytes[policy][retentionPeriodForUser] += resourceAttributesAsStructuredMetadataSize
		totalBytesReceived += resourceAttributesAsStructuredMetadataSize

		stats.ResourceAndSourceMetadataLabels[policy][retentionPeriodForUser] = append(stats.ResourceAndSourceMetadataLabels[policy][retentionPeriodForUser], resourceAttributesAsStructuredMetadata...)

		for j := 0; j < sls.Len(); j++ {
			logs := sls.At(j).LogRecords()

			scopeResult, err := otlplabels.ScopeAttrsToStructuredMetadata(sls, j, otlpConfig)
			if err != nil {
				return nil, err
			}
			scopeAttributesAsStructuredMetadata := scopeResult.StructuredMetadata

			// Scope attributes are pooled on their own, next to the resource ones rather than
			// concatenated with them, so that two scopes of the same resource share the single
			// pooled copy of that resource's attributes.
			var scopeAttrsHash uint64
			if deferStructuredMetadataExpansion {
				scopeAttrsHash = loki_util.StructuredMetadataHash(scopeAttributesAsStructuredMetadata)
			}

			// it would be rare to have multiple scopes so if the entries slice is empty, pre-allocate it for the number of log entries
			resourceStream, ok := pushRequestsByStream[labelsStr]
			if !ok {
				// A resource that produced no stream labels at all: its entries still need
				// somewhere to go, under a stream with an empty label set.
				resourceStream = &otlpStream{}
				pushRequestsByStream[labelsStr] = resourceStream
			}
			if cap(resourceStream.stream.Entries) == 0 {
				resourceStream.stream.Entries = make([]push.Entry, 0, logs.Len())
			}

			scopeAttributesAsStructuredMetadataSize := int64(loki_util.StructuredMetadataSize(scopeAttributesAsStructuredMetadata))
			stats.StructuredMetadataBytes[policy][retentionPeriodForUser] += scopeAttributesAsStructuredMetadataSize
			totalBytesReceived += scopeAttributesAsStructuredMetadataSize

			stats.ResourceAndSourceMetadataLabels[policy][retentionPeriodForUser] = append(stats.ResourceAndSourceMetadataLabels[policy][retentionPeriodForUser], scopeAttributesAsStructuredMetadata...)
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
				// Entries promoted to their own label set land in a different stream, which pools
				// this resource's and scope's attributes on its own.
				entryStream := resourceStream

				if len(logLabels) > 0 {
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

					entryStream = ensureStream(entryLabelsStr, entryLbs)
				} else {
					entryLabelsStr = labelsStr
					entryLbs = lbs
				}

				// Calculate the entry's own metadata size BEFORE adding resource and scope attributes
				// This preserves the intent of tracking entry-specific metadata separately without requiring subtraction
				entryOwnMetadataSize := int64(loki_util.StructuredMetadataSize(entry.StructuredMetadata))

				// With deferred expansion the resource and scope attributes stay in the stream's
				// pool and the entry keeps only its own attributes, pointing at the two pooled
				// sets. The references are built together with the pool they index, so they are
				// valid by construction; Stream.ValidateSharedRefs is asserted in the tests rather
				// than paid for on this hot path.
				if deferStructuredMetadataExpansion {
					entry.SharedResourceRef = entryStream.ref(resourceAttributesAsStructuredMetadata, resourceAttrsHash)
					entry.SharedScopeRef = entryStream.ref(scopeAttributesAsStructuredMetadata, scopeAttrsHash)
				} else {
					// if entry.StructuredMetadata doesn't have capacity to add resource and scope attributes, make a new slice with enough capacity
					attributesAsStructuredMetadataLen := len(resourceAttributesAsStructuredMetadata) + len(scopeAttributesAsStructuredMetadata)
					if cap(entry.StructuredMetadata) < len(entry.StructuredMetadata)+attributesAsStructuredMetadataLen {
						structuredMetadata := make(push.LabelsAdapter, 0, len(entry.StructuredMetadata)+len(scopeAttributesAsStructuredMetadata)+len(resourceAttributesAsStructuredMetadata))
						structuredMetadata = append(structuredMetadata, entry.StructuredMetadata...)
						entry.StructuredMetadata = structuredMetadata
					}

					entry.StructuredMetadata = append(entry.StructuredMetadata, resourceAttributesAsStructuredMetadata...)
					entry.StructuredMetadata = append(entry.StructuredMetadata, scopeAttributesAsStructuredMetadata...)
				}

				entryStream.stream.Entries = append(entryStream.stream.Entries, entry)

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
				// It is computed arithmetically rather than read back from the entry so that it
				// keeps reporting expanded-equivalent bytes when expansion is deferred and the
				// attributes are only stored once in the stream's pool. The two sizes below are
				// exactly those of the sets this entry references, since its references were just
				// built from them.
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

			if tracker != nil {
				tracker.ReceivedBytesAdd(ctx, userID, retentionPeriodForUser, lbs, float64(totalBytesReceived), format)
			}
		}
	}

	stats.MostRecentEntryTimestamp = mostRecentEntryTimestamp

	pr := &push.PushRequest{
		Streams: make([]push.Stream, 0, len(pushRequestsByStream)),
	}

	// Include all streams that have entries or have labels
	for _, s := range pushRequestsByStream {
		stream := s.stream
		if len(stream.Entries) > 0 || len(stream.Labels) > 0 {
			pr.Streams = append(pr.Streams, stream)
		}
		if logPushRequestStreams {
			mostRecentEntryTimestamp := time.Time{}
			// The pool is stored once for the whole stream, so it is accounted for once here too,
			// no matter how many entries reference its sets.
			streamSizeBytes := int64(loki_util.SharedSetsSize(stream.SharedStructuredMetadataSets))
			// It's difficult to calculate these values inline when we process the payload because promotion of resource attributes or log attributes to labels can change the stream with each entry.
			// So for simplicity and because this logging is typically disabled, we iterate on the entries to calculate these values here.
			for _, entry := range stream.Entries {
				streamSizeBytes += int64(len(entry.Line)) + int64(loki_util.StructuredMetadataSize(entry.StructuredMetadata))
				if entry.Timestamp.After(mostRecentEntryTimestamp) {
					mostRecentEntryTimestamp = entry.Timestamp
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
