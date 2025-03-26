// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package telemetry // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/deltatocumulativeprocessor/internal/telemetry"

import "go.opentelemetry.io/otel/attribute"

type Attributes []attribute.KeyValue

func (a *Attributes) Set(attr attribute.KeyValue) {
	*a = append(*a, attr)
}
