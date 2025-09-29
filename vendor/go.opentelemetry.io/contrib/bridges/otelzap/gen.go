// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package otelzap // import "go.opentelemetry.io/contrib/bridges/otelzap"

// Generate convert:
//go:generate gotmpl --body=../../internal/shared/logutil/convert_test.go.tmpl "--data={ \"pkg\": \"otelzap\" }" --out=convert_test.go
//go:generate gotmpl --body=../../internal/shared/logutil/convert.go.tmpl "--data={ \"pkg\": \"otelzap\" }" --out=convert.go
