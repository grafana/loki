package push

import (
	"compress/gzip"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"time"

	prometheustranslator "github.com/open-telemetry/opentelemetry-collector-contrib/pkg/translator/prometheus"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/push"
)

const (
	pbContentType       = "application/x-protobuf"
	gzipContentEncoding = "gzip"
)

var blessedAttributes = []string{
	"service.name",
	"service.namespace",
	"service.instance.id",
	"deployment.environment",
	"cloud.region",
	"cloud.availability_zone",
	"k8s.cluster.name",
	"k8s.namespace.name",
	"k8s.pod.name",
	"k8s.container.name",
	"container.name",
	"k8s.replicaset.name",
	"k8s.deployment.name",
	"k8s.statefulset.name",
	"k8s.daemonset.name",
	"k8s.cronjob.name",
	"k8s.job.name",
}

type pushStats struct {
	errs                     []error
	numLines                 int64
	logLinesBytes            map[time.Duration]int64
	structuredMetadataBytes  map[time.Duration]int64
	streamLabelsSize         int64
	mostRecentEntryTimestamp time.Time
}

func newPushStats() *pushStats {
	return &pushStats{
		logLinesBytes:           map[time.Duration]int64{},
		structuredMetadataBytes: map[time.Duration]int64{},
	}
}

func extractLogs(r *http.Request) (plog.Logs, error) {
	reader := r.Body
	if r.Header.Get("Content-Encoding") == gzipContentEncoding {
		r, err := gzip.NewReader(reader)
		if err != nil {
			return plog.NewLogs(), err
		}
		reader = r
		defer func(reader *gzip.Reader) {
			_ = reader.Close()
		}(r)
	}
	buf, err := io.ReadAll(reader)
	if err != nil {
		return plog.NewLogs(), err
	}

	req := plogotlp.NewExportRequest()

	switch r.Header.Get("Content-Type") {
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

func otlpToLokiPushRequest(ld plog.Logs, userID string, tenantsRetention TenantsRetention) (*logproto.PushRequest, *pushStats) {
	stats := newPushStats()

	if ld.LogRecordCount() == 0 {
		return &logproto.PushRequest{}, stats
	}

	pushRequestsByStream := map[string]logproto.Stream{}

	rls := ld.ResourceLogs()
	for i := 0; i < rls.Len(); i++ {
		sls := rls.At(i).ScopeLogs()

		streamLabels := model.LabelSet{}
		flattenedResourceAttributes := labels.NewBuilder(logproto.FromLabelAdaptersToLabels(flattenAttributes(rls.At(i).Resource().Attributes(), "")))
		// service.name is a required Resource Attribute. If it is not present, we will set it to "unknown_service".
		if flattenedResourceAttributes.Get("service_name") == "" {
			flattenedResourceAttributes = flattenedResourceAttributes.Set("service_name", "unknown_service")
		}

		// copy blessed attributes to stream labels
		for _, ba := range blessedAttributes {
			normalizedBlessedAttribute := prometheustranslator.NormalizeLabel(ba)
			v := flattenedResourceAttributes.Get(normalizedBlessedAttribute)
			if v == "" {
				continue
			}
			streamLabels[model.LabelName(normalizedBlessedAttribute)] = model.LabelValue(v)

			// remove the blessed attributes copied to stream labels
			flattenedResourceAttributes.Del(normalizedBlessedAttribute)
		}

		if err := streamLabels.Validate(); err != nil {
			stats.errs = append(stats.errs, fmt.Errorf("invalid labels: %w", err))
			continue
		}
		labelsStr := streamLabels.String()

		// convert the remaining resource attributes to structured metadata
		resourceAttributesAsStructuredMetadata := logproto.FromLabelsToLabelAdapters(flattenedResourceAttributes.Labels())

		lbs, err := syntax.ParseLabels(labelsStr)
		if err != nil {
			stats.errs = append(stats.errs, fmt.Errorf("couldn't parse labels: %w", err))
			continue
		}

		if _, ok := pushRequestsByStream[labelsStr]; !ok {
			pushRequestsByStream[labelsStr] = logproto.Stream{
				Labels: labelsStr,
			}
			stats.streamLabelsSize += int64(labelsSize(logproto.FromLabelsToLabelAdapters(lbs)))
		}

		resourceAttributesAsStructuredMetadataSize := labelsSize(resourceAttributesAsStructuredMetadata)
		stats.structuredMetadataBytes[tenantsRetention.RetentionPeriodFor(userID, lbs)] += int64(resourceAttributesAsStructuredMetadataSize)

		for j := 0; j < sls.Len(); j++ {
			scope := sls.At(j).Scope()
			logs := sls.At(j).LogRecords()

			// use fields and attributes from scope as structured metadata
			scopeAttributesAsStructuredMetadata := flattenAttributes(scope.Attributes(), "")

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
			stats.structuredMetadataBytes[tenantsRetention.RetentionPeriodFor(userID, lbs)] += int64(scopeAttributesAsStructuredMetadataSize)
			for k := 0; k < logs.Len(); k++ {
				log := logs.At(k)

				entry := otlpLogToPushEntry(log)
				entry.StructuredMetadata = append(entry.StructuredMetadata, resourceAttributesAsStructuredMetadata...)
				entry.StructuredMetadata = append(entry.StructuredMetadata, scopeAttributesAsStructuredMetadata...)
				stream := pushRequestsByStream[labelsStr]
				stream.Entries = append(stream.Entries, entry)
				pushRequestsByStream[labelsStr] = stream

				stats.structuredMetadataBytes[tenantsRetention.RetentionPeriodFor(userID, lbs)] += int64(labelsSize(entry.StructuredMetadata) - resourceAttributesAsStructuredMetadataSize - scopeAttributesAsStructuredMetadataSize)
				stats.logLinesBytes[tenantsRetention.RetentionPeriodFor(userID, lbs)] += int64(len(entry.Line))
				stats.numLines++
				if entry.Timestamp.After(stats.mostRecentEntryTimestamp) {
					stats.mostRecentEntryTimestamp = entry.Timestamp
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

	return pr, stats
}

// otlpLogToPushEntry converts an OTLP log record to a Loki push.Entry.
func otlpLogToPushEntry(log plog.LogRecord) push.Entry {
	// copy log attributes and all the fields from log(except log.Body) to structured metadata
	structuredMetadata := flattenAttributes(log.Attributes(), "")

	// if log.Timestamp() is 0, we would have already stored log.ObservedTimestamp as log timestamp so no need to store again in structured metadata
	if log.Timestamp() != 0 && log.ObservedTimestamp() != 0 {
		structuredMetadata = append(structuredMetadata, push.LabelAdapter{
			Name:  "observed_timestamp",
			Value: fmt.Sprintf("%d", log.ObservedTimestamp().AsTime().UnixNano()),
		})
	}

	if severityNum := log.SeverityNumber(); severityNum != plog.SeverityNumberUnspecified {
		structuredMetadata = append(structuredMetadata, push.LabelAdapter{
			Name:  "severity_number",
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

func flattenAttributes(val pcommon.Map, prefix string) push.LabelsAdapter {
	var labelsAdapter push.LabelsAdapter
	val.Range(func(k string, v pcommon.Value) bool {
		keyWithPrefix := prometheustranslator.NormalizeLabel(k)
		if prefix != "" {
			keyWithPrefix = prefix + "_" + k
		}

		typ := v.Type()
		if typ == pcommon.ValueTypeMap {
			labelsAdapter = append(labelsAdapter, flattenAttributes(v.Map(), keyWithPrefix)...)
		} else {
			labelsAdapter = append(labelsAdapter, push.LabelAdapter{Name: keyWithPrefix, Value: v.AsString()})
		}

		return true
	})

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
