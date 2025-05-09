package push

import (
	"compress/gzip"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/otlptranslator"
	"github.com/prometheus/prometheus/model/labels"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/runtime"
	loki_util "github.com/grafana/loki/v3/pkg/util"
)

const (
	pbContentType       = "application/x-protobuf"
	gzipContentEncoding = "gzip"
	attrServiceName     = "service.name"

	OTLPSeverityNumber = "severity_number"
	OTLPSeverityText   = "severity_text"

	messageSizeLargerErrFmt = "%w than max (%d vs %d)"
)

func ParseOTLPRequest(userID string, r *http.Request, limits Limits, tenantConfigs *runtime.TenantConfigs, maxRecvMsgSize int, tracker UsageTracker, streamResolver StreamResolver, logger log.Logger) (*logproto.PushRequest, *Stats, error) {
	stats := NewPushStats()
	otlpLogs, err := extractLogs(r, maxRecvMsgSize, stats)
	if err != nil {
		return nil, nil, err
	}

	req := otlpToLokiPushRequest(r.Context(), otlpLogs, userID, limits.OTLPConfig(userID), tenantConfigs, limits.DiscoverServiceName(userID), tracker, stats, logger, streamResolver)
	return req, stats, nil
}

func extractLogs(r *http.Request, maxRecvMsgSize int, pushStats *Stats) (plog.Logs, error) {
	pushStats.ContentEncoding = r.Header.Get(contentEnc)
	// bodySize should always reflect the compressed size of the request body
	bodySize := loki_util.NewSizeReader(r.Body)
	var body io.Reader = bodySize
	if maxRecvMsgSize > 0 {
		// Read from LimitReader with limit max+1. So if the underlying
		// reader is over limit, the result will be bigger than max.
		body = io.LimitReader(bodySize, int64(maxRecvMsgSize)+1)
	}
	if pushStats.ContentEncoding == gzipContentEncoding {
		r, err := gzip.NewReader(bodySize)
		if err != nil {
			return plog.NewLogs(), err
		}
		body = r
		defer func(reader *gzip.Reader) {
			_ = reader.Close()
		}(r)
	}
	buf, err := io.ReadAll(body)
	if err != nil {
		if size := bodySize.Size(); size > int64(maxRecvMsgSize) && maxRecvMsgSize > 0 {
			return plog.NewLogs(), fmt.Errorf(messageSizeLargerErrFmt, loki_util.ErrMessageSizeTooLarge, size, maxRecvMsgSize)
		}
		return plog.NewLogs(), err
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

func otlpToLokiPushRequest(ctx context.Context, ld plog.Logs, userID string, otlpConfig OTLPConfig, tenantConfigs *runtime.TenantConfigs, discoverServiceName []string, tracker UsageTracker, stats *Stats, logger log.Logger, streamResolver StreamResolver) *logproto.PushRequest {
	if ld.LogRecordCount() == 0 {
		return &logproto.PushRequest{}
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

		resourceAttributesAsStructuredMetadata := make(push.LabelsAdapter, 0, resAttrs.Len())
		streamLabels := make(model.LabelSet, 30) // we have a default labels limit of 30 so just initialize the map of same size
		var pushedLabels model.LabelSet
		if logServiceNameDiscovery {
			pushedLabels = make(model.LabelSet, 30)
		}

		shouldDiscoverServiceName := len(discoverServiceName) > 0 && !stats.IsInternalStream
		hasServiceName := false
		if v, ok := resAttrs.Get(attrServiceName); ok && v.AsString() != "" {
			hasServiceName = true
		}
		resAttrs.Range(func(k string, v pcommon.Value) bool {
			action := otlpConfig.ActionForResourceAttribute(k)
			if action == Drop {
				return true
			}

			attributeAsLabels := attributeToLabels(k, v, "")
			if action == IndexLabel {
				for _, lbl := range attributeAsLabels {
					streamLabels[model.LabelName(lbl.Name)] = model.LabelValue(lbl.Value)
					if logServiceNameDiscovery && pushedLabels != nil {
						pushedLabels[model.LabelName(lbl.Name)] = model.LabelValue(lbl.Value)
					}

					if !hasServiceName && shouldDiscoverServiceName {
						for _, labelName := range discoverServiceName {
							if lbl.Name == labelName {
								streamLabels[model.LabelName(LabelServiceName)] = model.LabelValue(lbl.Value)
								hasServiceName = true
								break
							}
						}
					}
				}
			} else if action == StructuredMetadata {
				resourceAttributesAsStructuredMetadata = append(resourceAttributesAsStructuredMetadata, attributeAsLabels...)
			}

			return true
		})

		if !hasServiceName && shouldDiscoverServiceName {
			streamLabels[model.LabelName(LabelServiceName)] = model.LabelValue(ServiceUnknown)
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
		policy := streamResolver.PolicyFor(lbs)

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
			scope := sls.At(j).Scope()
			logs := sls.At(j).LogRecords()
			scopeAttrs := scope.Attributes()

			// it would be rare to have multiple scopes so if the entries slice is empty, pre-allocate it for the number of log entries
			if cap(pushRequestsByStream[labelsStr].Entries) == 0 {
				stream := pushRequestsByStream[labelsStr]
				stream.Entries = make([]push.Entry, 0, logs.Len())
				pushRequestsByStream[labelsStr] = stream
			}

			// use fields and attributes from scope as structured metadata
			scopeAttributesAsStructuredMetadata := make(push.LabelsAdapter, 0, scopeAttrs.Len()+3)
			scopeAttrs.Range(func(k string, v pcommon.Value) bool {
				action := otlpConfig.ActionForScopeAttribute(k)
				if action == Drop {
					return true
				}

				attributeAsLabels := attributeToLabels(k, v, "")
				if action == StructuredMetadata {
					scopeAttributesAsStructuredMetadata = append(scopeAttributesAsStructuredMetadata, attributeAsLabels...)
				}

				return true
			})

			if scopeName := scope.Name(); scopeName != "" {
				scopeAttributesAsStructuredMetadata = append(scopeAttributesAsStructuredMetadata, push.LabelAdapter{
					Name:  "scope_name",
					Value: scopeName,
				})
			}
			if scopeVersion := scope.Version(); scopeVersion != "" {
				scopeAttributesAsStructuredMetadata = append(scopeAttributesAsStructuredMetadata, push.LabelAdapter{
					Name:  "scope_version",
					Value: scopeVersion,
				})
			}
			if scopeDroppedAttributesCount := scope.DroppedAttributesCount(); scopeDroppedAttributesCount != 0 {
				scopeAttributesAsStructuredMetadata = append(scopeAttributesAsStructuredMetadata, push.LabelAdapter{
					Name:  "scope_dropped_attributes_count",
					Value: fmt.Sprintf("%d", scopeDroppedAttributesCount),
				})
			}

			scopeAttributesAsStructuredMetadataSize := loki_util.StructuredMetadataSize(scopeAttributesAsStructuredMetadata)
			stats.StructuredMetadataBytes[policy][retentionPeriodForUser] += int64(scopeAttributesAsStructuredMetadataSize)
			totalBytesReceived += int64(scopeAttributesAsStructuredMetadataSize)

			stats.ResourceAndSourceMetadataLabels[policy][retentionPeriodForUser] = append(stats.ResourceAndSourceMetadataLabels[policy][retentionPeriodForUser], scopeAttributesAsStructuredMetadata...)
			for k := 0; k < logs.Len(); k++ {
				log := logs.At(k)

				// Use the existing function that already handles log attributes properly
				logLabels, entry := otlpLogToPushEntry(log, otlpConfig, logServiceNameDiscovery, pushedLabels)
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
				entryPolicy := streamResolver.PolicyFor(entryLbs)

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
					tracker.ReceivedBytesAdd(ctx, userID, entryRetentionPeriod, entryLbs, float64(totalBytesReceived))
				}
			}

			if tracker != nil {
				tracker.ReceivedBytesAdd(ctx, userID, retentionPeriodForUser, lbs, float64(totalBytesReceived))
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

	return pr
}

// otlpLogToPushEntry converts an OTLP log record to a Loki push.Entry.
func otlpLogToPushEntry(log plog.LogRecord, otlpConfig OTLPConfig, logServiceNameDiscovery bool, pushedLabels model.LabelSet) (model.LabelSet, push.Entry) {
	// copy log attributes and all the fields from log(except log.Body) to structured metadata
	logAttrs := log.Attributes()
	structuredMetadata := make(push.LabelsAdapter, 0, logAttrs.Len()+7)
	logLabels := make(model.LabelSet)

	logAttrs.Range(func(k string, v pcommon.Value) bool {
		action := otlpConfig.ActionForLogAttribute(k)
		if action == Drop {
			return true
		}

		attributeAsLabels := attributeToLabels(k, v, "")
		if action == StructuredMetadata {
			structuredMetadata = append(structuredMetadata, attributeAsLabels...)
		}

		if action == IndexLabel {
			for _, lbl := range attributeAsLabels {
				logLabels[model.LabelName(lbl.Name)] = model.LabelValue(lbl.Value)
				if logServiceNameDiscovery && pushedLabels != nil {
					pushedLabels[model.LabelName(lbl.Name)] = model.LabelValue(lbl.Value)
				}
			}
		}

		return true
	})

	// if log.Timestamp() is 0, we would have already stored log.ObservedTimestamp as log timestamp so no need to store again in structured metadata
	if log.Timestamp() != 0 && log.ObservedTimestamp() != 0 {
		structuredMetadata = append(structuredMetadata, push.LabelAdapter{
			Name:  "observed_timestamp",
			Value: fmt.Sprintf("%d", log.ObservedTimestamp().AsTime().UnixNano()),
		})
	}

	if severityNum := log.SeverityNumber(); severityNum != plog.SeverityNumberUnspecified {
		structuredMetadata = append(structuredMetadata, push.LabelAdapter{
			Name:  OTLPSeverityNumber,
			Value: fmt.Sprintf("%d", severityNum),
		})
	}
	if severityText := log.SeverityText(); severityText != "" {
		// Add severity_text as an index label if configured
		if otlpConfig.SeverityTextAsLabel {
			logLabels[model.LabelName(OTLPSeverityText)] = model.LabelValue(severityText)
			if logServiceNameDiscovery && pushedLabels != nil {
				pushedLabels[model.LabelName(OTLPSeverityText)] = model.LabelValue(severityText)
			}
		}

		// Always add severity_text as structured metadata
		structuredMetadata = append(structuredMetadata, push.LabelAdapter{
			Name:  OTLPSeverityText,
			Value: severityText,
		})
	}

	if droppedAttributesCount := log.DroppedAttributesCount(); droppedAttributesCount != 0 {
		structuredMetadata = append(structuredMetadata, push.LabelAdapter{
			Name:  "dropped_attributes_count",
			Value: fmt.Sprintf("%d", droppedAttributesCount),
		})
	}
	if logRecordFlags := log.Flags(); logRecordFlags != 0 {
		structuredMetadata = append(structuredMetadata, push.LabelAdapter{
			Name:  "flags",
			Value: fmt.Sprintf("%d", logRecordFlags),
		})
	}

	if traceID := log.TraceID(); !traceID.IsEmpty() {
		structuredMetadata = append(structuredMetadata, push.LabelAdapter{
			Name:  "trace_id",
			Value: hex.EncodeToString(traceID[:]),
		})
	}
	if spanID := log.SpanID(); !spanID.IsEmpty() {
		structuredMetadata = append(structuredMetadata, push.LabelAdapter{
			Name:  "span_id",
			Value: hex.EncodeToString(spanID[:]),
		})
	}

	return logLabels, push.Entry{
		Timestamp:          timestampFromLogRecord(log),
		Line:               log.Body().AsString(),
		StructuredMetadata: structuredMetadata,
	}
}

func attributesToLabels(attrs pcommon.Map, prefix string) push.LabelsAdapter {
	labelsAdapter := make(push.LabelsAdapter, 0, attrs.Len())
	if attrs.Len() == 0 {
		return labelsAdapter
	}

	attrs.Range(func(k string, v pcommon.Value) bool {
		labelsAdapter = append(labelsAdapter, attributeToLabels(k, v, prefix)...)
		return true
	})

	return labelsAdapter
}

func attributeToLabels(k string, v pcommon.Value, prefix string) push.LabelsAdapter {
	var labelsAdapter push.LabelsAdapter

	keyWithPrefix := k
	if prefix != "" {
		keyWithPrefix = prefix + "_" + k
	}
	keyWithPrefix = otlptranslator.NormalizeLabel(keyWithPrefix)

	typ := v.Type()
	if typ == pcommon.ValueTypeMap {
		mv := v.Map()
		labelsAdapter = make(push.LabelsAdapter, 0, mv.Len())
		mv.Range(func(k string, v pcommon.Value) bool {
			labelsAdapter = append(labelsAdapter, attributeToLabels(k, v, keyWithPrefix)...)
			return true
		})
	} else {
		labelsAdapter = push.LabelsAdapter{
			push.LabelAdapter{Name: keyWithPrefix, Value: v.AsString()},
		}
	}

	return labelsAdapter
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
	l := make(labels.Labels, 0, len(m))
	for lName, lValue := range m {
		l = append(l, labels.Label{
			Name:  string(lName),
			Value: string(lValue),
		})
	}

	sort.Sort(l)
	return l
}
