// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/pdata/internal"

import (
	"go.opentelemetry.io/collector/pdata/internal/data"
	"go.opentelemetry.io/collector/pdata/internal/json"
)

func MarshalJSONOrigTraceID(id *data.TraceID, dest *json.Stream) {
	id.MarshalJSONStream(dest)
}

func MarshalJSONOrigSpanID(id *data.SpanID, dest *json.Stream) {
	id.MarshalJSONStream(dest)
}

func MarshalJSONOrigProfileID(id *data.ProfileID, dest *json.Stream) {
	id.MarshalJSONStream(dest)
}

func SizeProtoOrigTraceID(id *data.TraceID) int {
	return id.Size()
}

func SizeProtoOrigSpanID(id *data.SpanID) int {
	return id.Size()
}

func SizeProtoOrigProfileID(id *data.ProfileID) int {
	return id.Size()
}

func MarshalProtoOrigTraceID(id *data.TraceID, buf []byte) int {
	size := id.Size()
	_, _ = id.MarshalTo(buf[len(buf)-size:])
	return size
}

func MarshalProtoOrigSpanID(id *data.SpanID, buf []byte) int {
	size := id.Size()
	_, _ = id.MarshalTo(buf[len(buf)-size:])
	return size
}

func MarshalProtoOrigProfileID(id *data.ProfileID, buf []byte) int {
	size := id.Size()
	_, _ = id.MarshalTo(buf[len(buf)-size:])
	return size
}
