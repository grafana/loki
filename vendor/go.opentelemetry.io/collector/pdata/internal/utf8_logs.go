// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/pdata/internal"

// SanitizeUTF8ExportLogsServiceRequest replaces invalid UTF-8 byte sequences in every string field of req
// with the Unicode replacement character U+FFFD.
func SanitizeUTF8ExportLogsServiceRequest(req *ExportLogsServiceRequest) {
	for _, rl := range req.ResourceLogs {
		SanitizeUTF8Resource(&rl.Resource)
		rl.SchemaUrl = SanitizeUTF8String(rl.SchemaUrl)
		sanitizeUTF8ScopeLogsSlice(rl.ScopeLogs)
		// DeprecatedScopeLogs is normally emptied by otlp.MigrateLogs, but JSON unmarshalling can still
		// populate it, so it is walked as well.
		sanitizeUTF8ScopeLogsSlice(rl.DeprecatedScopeLogs)
	}
}

func sanitizeUTF8ScopeLogsSlice(sls []*ScopeLogs) {
	for _, sl := range sls {
		SanitizeUTF8InstrumentationScope(&sl.Scope)
		sl.SchemaUrl = SanitizeUTF8String(sl.SchemaUrl)
		for _, lr := range sl.LogRecords {
			sanitizeUTF8LogRecord(lr)
		}
	}
}

func sanitizeUTF8LogRecord(lr *LogRecord) {
	lr.SeverityText = SanitizeUTF8String(lr.SeverityText)
	lr.EventName = SanitizeUTF8String(lr.EventName)
	SanitizeUTF8AnyValue(&lr.Body)
	SanitizeUTF8KeyValueSlice(lr.Attributes)
}
