// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package syncint64 // import "go.opentelemetry.io/otel/metric/instrument/syncint64"

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric/instrument"
)

// InstrumentProvider provides access to individual instruments.
type InstrumentProvider interface {
	// Counter creates an instrument for recording increasing values.
	Counter(name string, opts ...instrument.Option) (Counter, error)
	// UpDownCounter creates an instrument for recording changes of a value.
	UpDownCounter(name string, opts ...instrument.Option) (UpDownCounter, error)
	// Histogram creates an instrument for recording a distribution of values.
	Histogram(name string, opts ...instrument.Option) (Histogram, error)
}

// Counter is an instrument that records increasing values.
type Counter interface {
	// Add records a change to the counter.
	Add(ctx context.Context, incr int64, attrs ...attribute.KeyValue)

	instrument.Synchronous
}

// UpDownCounter is an instrument that records increasing or decresing values.
type UpDownCounter interface {
	// Add records a change to the counter.
	Add(ctx context.Context, incr int64, attrs ...attribute.KeyValue)

	instrument.Synchronous
}

// Histogram is an instrument that records a distribution of values.
type Histogram interface {
	// Record adds an additional value to the distribution.
	Record(ctx context.Context, incr int64, attrs ...attribute.KeyValue)

	instrument.Synchronous
}
