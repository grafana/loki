// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package ptrace // import "go.opentelemetry.io/collector/pdata/ptrace"

import (
	"slices"

	"go.opentelemetry.io/collector/pdata/internal/json"
	"go.opentelemetry.io/collector/pdata/internal/otlp"
)

// JSONMarshaler marshals Traces to JSON bytes using the OTLP/JSON format.
type JSONMarshaler struct{}

// MarshalTraces to the OTLP/JSON format.
func (*JSONMarshaler) MarshalTraces(td Traces) ([]byte, error) {
	dest := json.BorrowStream(nil)
	defer json.ReturnStream(dest)
	td.getOrig().MarshalJSON(dest)
	if dest.Error() != nil {
		return nil, dest.Error()
	}
	return slices.Clone(dest.Buffer()), nil
}

// JSONUnmarshaler unmarshals OTLP/JSON formatted-bytes to Traces.
type JSONUnmarshaler struct {
	// prevent unkeyed literal initialization
	_ struct{}
	// DisallowUnknownFields causes UnmarshalTraces to return an error when the
	// input contains JSON object fields that are not defined by the OTLP
	// schema. When false (the default), unknown fields are silently ignored.
	//
	// Warning: enabling this option breaks forwards compatibility with future
	// evolutions of the OTLP format, as fields added to the format in newer
	// versions will be rejected as unknown.
	DisallowUnknownFields bool
}

// UnmarshalTraces from OTLP/JSON format into Traces.
func (u *JSONUnmarshaler) UnmarshalTraces(buf []byte) (Traces, error) {
	iter := json.BorrowIterator(buf)
	defer json.ReturnIterator(iter)
	iter.SetDisallowUnknownFields(u.DisallowUnknownFields)
	td := NewTraces()
	td.getOrig().UnmarshalJSON(iter)
	if iter.Error() != nil {
		return Traces{}, iter.Error()
	}
	otlp.MigrateTraces(td.getOrig().ResourceSpans)
	return td, nil
}
