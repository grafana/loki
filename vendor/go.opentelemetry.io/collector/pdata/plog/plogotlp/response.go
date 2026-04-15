// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package plogotlp // import "go.opentelemetry.io/collector/pdata/plog/plogotlp"

import (
	"slices"

	"go.opentelemetry.io/collector/pdata/internal/json"
)

// MarshalProto marshals ExportResponse into proto bytes.
func (ms ExportResponse) MarshalProto() ([]byte, error) {
	size := ms.orig.SizeProto()
	buf := make([]byte, size)
	_ = ms.orig.MarshalProto(buf)
	return buf, nil
}

// UnmarshalProto unmarshalls ExportResponse from proto bytes.
func (ms ExportResponse) UnmarshalProto(data []byte) error {
	return ms.orig.UnmarshalProto(data)
}

// MarshalJSON marshals ExportResponse into JSON bytes.
func (ms ExportResponse) MarshalJSON() ([]byte, error) {
	dest := json.BorrowStream(nil)
	defer json.ReturnStream(dest)
	ms.orig.MarshalJSON(dest)
	return slices.Clone(dest.Buffer()), dest.Error()
}

// UnmarshalJSON unmarshalls ExportResponse from JSON bytes.
func (ms ExportResponse) UnmarshalJSON(data []byte) error {
	iter := json.BorrowIterator(data)
	defer json.ReturnIterator(iter)
	ms.orig.UnmarshalJSON(iter)
	return iter.Error()
}
