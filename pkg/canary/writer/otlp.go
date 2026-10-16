package writer

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
)

const (
	// PushProtocolLoki sends Snappy-compressed Loki push requests.
	PushProtocolLoki = "loki"
	// PushProtocolOTLP sends gzip-compressed OTLP/HTTP protobuf requests.
	PushProtocolOTLP = "otlp"
)

func (p *Push) buildOTLPPayload(entries []entry) ([]byte, error) {
	req := plogotlp.NewExportRequest()
	resource := req.Logs().ResourceLogs().AppendEmpty()
	attrs := resource.Resource().Attributes()
	attrs.PutStr("service.name", "loki-canary")
	// Keep the selector keys identical to native push. The tenant must promote
	// these resource attributes to index labels (see the canary documentation).
	attrs.PutStr(p.labelName, p.labelValue)
	attrs.PutStr(p.streamName, p.streamValue)
	scope := resource.ScopeLogs().AppendEmpty()
	scope.Scope().SetName("loki-canary")
	records := scope.LogRecords()
	records.EnsureCapacity(len(entries))
	for _, e := range entries {
		record := records.AppendEmpty()
		record.SetTimestamp(pcommon.NewTimestampFromTime(e.ts))
		record.Body().SetStr(e.entry)
		// A per-record integer exercises OTLP attribute conversion and lets the
		// reader detect missing, truncated or incorrectly associated metadata.
		record.Attributes().PutInt("canary_timestamp", e.ts.UnixNano())
	}
	payload, err := req.MarshalProto()
	if err != nil {
		return nil, err
	}

	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write(payload); err != nil {
		return nil, fmt.Errorf("compress OTLP payload: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("finish compressing OTLP payload: %w", err)
	}
	return compressed.Bytes(), nil
}

func validateOTLPResponse(body io.Reader) error {
	// OTLP may return HTTP 200 with rejected records. Surface this as a
	// non-retryable error: retrying the entire batch would duplicate accepted logs.
	const maxResponseSize = 64 * 1024
	data, err := io.ReadAll(io.LimitReader(body, maxResponseSize+1))
	if err != nil {
		return fmt.Errorf("read OTLP response: %w", err)
	}
	if len(data) > maxResponseSize {
		return fmt.Errorf("OTLP response exceeds %d bytes", maxResponseSize)
	}
	response := plogotlp.NewExportResponse()
	if err := response.UnmarshalProto(data); err != nil {
		return fmt.Errorf("decode OTLP response: %w", err)
	}
	partial := response.PartialSuccess()
	if partial.RejectedLogRecords() != 0 || partial.ErrorMessage() != "" {
		return fmt.Errorf("OTLP partial success: rejected %d log records: %s", partial.RejectedLogRecords(), partial.ErrorMessage())
	}
	return nil
}
