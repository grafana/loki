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

	req, err := otlpToLokiPushRequest(r.Context(), otlpLogs, userID, limits.OTLPConfig(userID), tenantConfigs, limits.DiscoverServiceName(userID), tracker, stats, logger, streamResolver, constants.OTLP)
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

func otlpToLokiPushRequest(ctx context.Context, ld plog.Logs, userID string, otlpConfig OTLPConfig, tenantConfigs *runtime.TenantConfigs, discoverServiceName []string, tracker UsageTracker, stats *Stats, logger log.Logger, streamResolver StreamResolver, format string) (*logproto.PushRequest, error) {
	if ld.LogRecordCount() == 0 {
		return &logproto.PushRequest{}, nil
	}

	rls := ld.ResourceLogs()
	pushRequestsByStream := make(map[string]logproto.Stream, rls.Len())

	// Track if request used the Loki OTLP exporter label
	var usingLokiExporter bool

	logServiceNameDiscovery := false
	logPushRequestStreams := false
	if tenantConfigs != nil {
		logServiceNameDiscovery = tenantConfigs.LogServiceNameDiscovery(userID)
		logPushRequestStreams = tenantConfigs.LogPushRequestStreams(userID)
	}

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
				pushRequestsByStream[labelsStr] = logproto.Stream{
					Labels: labelsStr,
				}
				stats.StreamLabelsSize += int64(labelsSize(logproto.FromLabelsToLabelAdapters(lbs)))
			}
		}

		// Calculate resource attributes metadata size for stats
		resourceAttributesAsStructuredMetadataSize := loki_util.StructuredMetadataSize(resourceAttributesAsStructuredMetadata)
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

		stats.StructuredMetadataBytes[policy][retentionPeriodForUser] += int64(resourceAttributesAsStructuredMetadataSize)
		totalBytesReceived += int64(resourceAttributesAsStructuredMetadataSize)

		stats.ResourceAndSourceMetadataLabels[policy][retentionPeriodForUser] = append(stats.ResourceAndSourceMetadataLabels[policy][retentionPeriodForUser], resourceAttributesAsStructuredMetadata...)

		for j := 0; j < sls.Len(); j++ {
			logs := sls.At(j).LogRecords()

			// it would be rare to have multiple scopes so if the entries slice is empty, pre-allocate it for the number of log entries
			if cap(pushRequestsByStream[labelsStr].Entries) == 0 {
				stream := pushRequestsByStream[labelsStr]
				stream.Entries = make([]push.Entry, 0, logs.Len())
				pushRequestsByStream[labelsStr] = stream
			}

			scopeResult, err := otlplabels.ScopeAttrsToStructuredMetadata(sls, j, otlpConfig)
			if err != nil {
				return nil, err
			}
			scopeAttributesAsStructuredMetadata := scopeResult.StructuredMetadata

			scopeAttributesAsStructuredMetadataSize := loki_util.StructuredMetadataSize(scopeAttributesAsStructuredMetadata)
			stats.StructuredMetadataBytes[policy][retentionPeriodForUser] += int64(scopeAttributesAsStructuredMetadataSize)
			totalBytesReceived += int64(scopeAttributesAsStructuredMetadataSize)

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

					if _, ok := pushRequestsByStream[entryLabelsStr]; !ok {
						pushRequestsByStream[entryLabelsStr] = logproto.Stream{
							Labels: entryLabelsStr,
						}
						stats.StreamLabelsSize += int64(labelsSize(logproto.FromLabelsToLabelAdapters(entryLbs)))
					}
				} else {
					entryLabelsStr = labelsStr
					entryLbs = lbs
				}

				// Calculate the entry's own metadata size BEFORE adding resource and scope attributes
				// This preserves the intent of tracking entry-specific metadata separately without requiring subtraction
				entryOwnMetadataSize := int64(loki_util.StructuredMetadataSize(entry.StructuredMetadata))

				// if entry.StructuredMetadata doesn't have capacity to add resource and scope attributes, make a new slice with enough capacity
				attributesAsStructuredMetadataLen := len(resourceAttributesAsStructuredMetadata) + len(scopeAttributesAsStructuredMetadata)
				if cap(entry.StructuredMetadata) < len(entry.StructuredMetadata)+attributesAsStructuredMetadataLen {
					structuredMetadata := make(push.LabelsAdapter, 0, len(entry.StructuredMetadata)+len(scopeAttributesAsStructuredMetadata)+len(resourceAttributesAsStructuredMetadata))
					structuredMetadata = append(structuredMetadata, entry.StructuredMetadata...)
					entry.StructuredMetadata = structuredMetadata
				}

				entry.StructuredMetadata = append(entry.StructuredMetadata, resourceAttributesAsStructuredMetadata...)
				entry.StructuredMetadata = append(entry.StructuredMetadata, scopeAttributesAsStructuredMetadata...)
				stream := pushRequestsByStream[entryLabelsStr]
				stream.Entries = append(stream.Entries, entry)
				pushRequestsByStream[entryLabelsStr] = stream

				entryRetentionPeriod := streamResolver.RetentionPeriodFor(entryLbs)
				entryPolicy := streamResolver.PolicyFor(ctx, entryLbs)

				if _, ok := stats.StructuredMetadataBytes[entryPolicy]; !ok {
					stats.StructuredMetadataBytes[entryPolicy] = make(map[time.Duration]int64)
				}
				// Use the entry's own metadata size (calculated before adding resource/scope attributes)
				// This keeps the same accounting intention without risk of negative values
				stats.StructuredMetadataBytes[entryPolicy][entryRetentionPeriod] += entryOwnMetadataSize

				if _, ok := stats.LogLinesBytes[entryPolicy]; !ok {
					stats.LogLinesBytes[entryPolicy] = make(map[time.Duration]int64)
				}
				stats.LogLinesBytes[entryPolicy][entryRetentionPeriod] += int64(len(entry.Line))

				totalBytesReceived += entryOwnMetadataSize
				totalBytesReceived += int64(len(entry.Line))

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
	for _, stream := range pushRequestsByStream {
		if len(stream.Entries) > 0 || len(stream.Labels) > 0 {
			pr.Streams = append(pr.Streams, stream)
		}
		if logPushRequestStreams {
			mostRecentEntryTimestamp := time.Time{}
			streamSizeBytes := int64(0)
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
