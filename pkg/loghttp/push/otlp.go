package push

import (
	"compress/gzip"
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage/remote/otlptranslator/prometheus"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/logproto"
	loki_util "github.com/grafana/loki/v3/pkg/util"
)

const (
	pbContentType       = "application/x-protobuf"
	gzipContentEncoding = "gzip"
	attrServiceName     = "service.name"

	OTLPSeverityNumber = "severity_number"
)

func newPushStats() *Stats {
	return &Stats{
		LogLinesBytes:                   map[time.Duration]int64{},
		StructuredMetadataBytes:         map[time.Duration]int64{},
		ResourceAndSourceMetadataLabels: map[time.Duration]push.LabelsAdapter{},
	}
}

func ParseOTLPRequest(userID string, r *http.Request, tenantsRetention TenantsRetention, limits Limits, tracker UsageTracker) (*logproto.PushRequest, *Stats, error) {
	stats := newPushStats()
	otlpLogs, err := extractLogs(r, stats)
	if err != nil {
		return nil, nil, err
	}

	req := otlpToLokiPushRequest(r.Context(), otlpLogs, userID, tenantsRetention, limits.OTLPConfig(userID), limits.DiscoverServiceName(userID), tracker, stats)
	return req, stats, nil
}

func extractLogs(r *http.Request, pushStats *Stats) (plog.Logs, error) {
	pushStats.ContentEncoding = r.Header.Get(contentEnc)
	// bodySize should always reflect the compressed size of the request body
	bodySize := loki_util.NewSizeReader(r.Body)
	var body io.Reader = bodySize
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

func otlpToLokiPushRequest(ctx context.Context, ld plog.Logs, userID string, tenantsRetention TenantsRetention, otlpConfig OTLPConfig, discoverServiceName []string, tracker UsageTracker, stats *Stats) *logproto.PushRequest {
	if ld.LogRecordCount() == 0 {
		return &logproto.PushRequest{}
	}

	rls := ld.ResourceLogs()
	pushRequestsByStream := make(map[string]logproto.Stream, rls.Len())

	for i := 0; i < rls.Len(); i++ {
		sls := rls.At(i).ScopeLogs()
		res := rls.At(i).Resource()
		resAttrs := res.Attributes()

		resourceAttributesAsStructuredMetadata := make(push.LabelsAdapter, 0, resAttrs.Len())
		streamLabels := make(model.LabelSet, 30) // we have a default labels limit of 30 so just initialize the map of same size

		shouldDiscoverServiceName := len(discoverServiceName) > 0 && !stats.IsAggregatedMetric
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

		if err := streamLabels.Validate(); err != nil {
			stats.Errs = append(stats.Errs, fmt.Errorf("invalid labels: %w", err))
			continue
		}
		labelsStr := streamLabels.String()

		lbs := modelLabelsSetToLabelsList(streamLabels)

		if _, ok := pushRequestsByStream[labelsStr]; !ok {
			pushRequestsByStream[labelsStr] = logproto.Stream{
				Labels: labelsStr,
			}
			stats.StreamLabelsSize += int64(labelsSize(logproto.FromLabelsToLabelAdapters(lbs)))
		}

		resourceAttributesAsStructuredMetadataSize := labelsSize(resourceAttributesAsStructuredMetadata)
		retentionPeriodForUser := tenantsRetention.RetentionPeriodFor(userID, lbs)

		stats.StructuredMetadataBytes[retentionPeriodForUser] += int64(resourceAttributesAsStructuredMetadataSize)
		if tracker != nil {
			tracker.ReceivedBytesAdd(ctx, userID, retentionPeriodForUser, lbs, float64(resourceAttributesAsStructuredMetadataSize))
		}

		stats.ResourceAndSourceMetadataLabels[retentionPeriodForUser] = append(stats.ResourceAndSourceMetadataLabels[retentionPeriodForUser], resourceAttributesAsStructuredMetadata...)

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

			scopeAttributesAsStructuredMetadataSize := labelsSize(scopeAttributesAsStructuredMetadata)
			stats.StructuredMetadataBytes[retentionPeriodForUser] += int64(scopeAttributesAsStructuredMetadataSize)
			if tracker != nil {
				tracker.ReceivedBytesAdd(ctx, userID, retentionPeriodForUser, lbs, float64(scopeAttributesAsStructuredMetadataSize))
			}

			stats.ResourceAndSourceMetadataLabels[retentionPeriodForUser] = append(stats.ResourceAndSourceMetadataLabels[retentionPeriodForUser], scopeAttributesAsStructuredMetadata...)
			for k := 0; k < logs.Len(); k++ {
				log := logs.At(k)

				entry := otlpLogToPushEntry(log, otlpConfig)

				// if entry.StructuredMetadata doesn't have capacity to add resource and scope attributes, make a new slice with enough capacity
				attributesAsStructuredMetadataLen := len(resourceAttributesAsStructuredMetadata) + len(scopeAttributesAsStructuredMetadata)
				if cap(entry.StructuredMetadata) < len(entry.StructuredMetadata)+attributesAsStructuredMetadataLen {
					structuredMetadata := make(push.LabelsAdapter, 0, len(entry.StructuredMetadata)+len(scopeAttributesAsStructuredMetadata)+len(resourceAttributesAsStructuredMetadata))
					structuredMetadata = append(structuredMetadata, entry.StructuredMetadata...)
					entry.StructuredMetadata = structuredMetadata
				}

				entry.StructuredMetadata = append(entry.StructuredMetadata, resourceAttributesAsStructuredMetadata...)
				entry.StructuredMetadata = append(entry.StructuredMetadata, scopeAttributesAsStructuredMetadata...)
				stream := pushRequestsByStream[labelsStr]
				stream.Entries = append(stream.Entries, entry)
				pushRequestsByStream[labelsStr] = stream

				metadataSize := int64(labelsSize(entry.StructuredMetadata) - resourceAttributesAsStructuredMetadataSize - scopeAttributesAsStructuredMetadataSize)
				stats.StructuredMetadataBytes[retentionPeriodForUser] += metadataSize
				stats.LogLinesBytes[retentionPeriodForUser] += int64(len(entry.Line))

				if tracker != nil {
					tracker.ReceivedBytesAdd(ctx, userID, retentionPeriodForUser, lbs, float64(len(entry.Line)))
					tracker.ReceivedBytesAdd(ctx, userID, retentionPeriodForUser, lbs, float64(metadataSize))
				}

				stats.NumLines++
				if entry.Timestamp.After(stats.MostRecentEntryTimestamp) {
					stats.MostRecentEntryTimestamp = entry.Timestamp
				}
			}
		}
	}

	pr := &push.PushRequest{
		Streams: make([]push.Stream, 0, len(pushRequestsByStream)),
	}

	for _, stream := range pushRequestsByStream {
		pr.Streams = append(pr.Streams, stream)
	}

	return pr
}

// otlpLogToPushEntry converts an OTLP log record to a Loki push.Entry.
func otlpLogToPushEntry(log plog.LogRecord, otlpConfig OTLPConfig) push.Entry {
	// copy log attributes and all the fields from log(except log.Body) to structured metadata
	logAttrs := log.Attributes()
	structuredMetadata := make(push.LabelsAdapter, 0, logAttrs.Len()+7)
	logAttrs.Range(func(k string, v pcommon.Value) bool {
		action := otlpConfig.ActionForLogAttribute(k)
		if action == Drop {
			return true
		}

		attributeAsLabels := attributeToLabels(k, v, "")
		if action == StructuredMetadata {
			structuredMetadata = append(structuredMetadata, attributeAsLabels...)
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
		structuredMetadata = append(structuredMetadata, push.LabelAdapter{
			Name:  "severity_text",
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

	return push.Entry{
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
	keyWithPrefix = prometheus.NormalizeLabel(keyWithPrefix)

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
