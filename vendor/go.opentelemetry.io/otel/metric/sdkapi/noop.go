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

package sdkapi // import "go.opentelemetry.io/otel/metric/sdkapi"

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric/number"
)

<<<<<<< HEAD
type noopInstrument struct{}
=======
type noopInstrument struct {
	descriptor Descriptor
}
>>>>>>> main
type noopSyncInstrument struct{ noopInstrument }
type noopAsyncInstrument struct{ noopInstrument }

var _ SyncImpl = noopSyncInstrument{}
var _ AsyncImpl = noopAsyncInstrument{}

// NewNoopSyncInstrument returns a No-op implementation of the
// synchronous instrument interface.
func NewNoopSyncInstrument() SyncImpl {
<<<<<<< HEAD
	return noopSyncInstrument{}
=======
	return noopSyncInstrument{
		noopInstrument{
			descriptor: Descriptor{
				instrumentKind: CounterInstrumentKind,
			},
		},
	}
>>>>>>> main
}

// NewNoopAsyncInstrument returns a No-op implementation of the
// asynchronous instrument interface.
func NewNoopAsyncInstrument() AsyncImpl {
<<<<<<< HEAD
	return noopAsyncInstrument{}
=======
	return noopAsyncInstrument{
		noopInstrument{
			descriptor: Descriptor{
				instrumentKind: CounterObserverInstrumentKind,
			},
		},
	}
>>>>>>> main
}

func (noopInstrument) Implementation() interface{} {
	return nil
}

<<<<<<< HEAD
func (noopInstrument) Descriptor() Descriptor {
	return Descriptor{}
=======
func (n noopInstrument) Descriptor() Descriptor {
	return n.descriptor
>>>>>>> main
}

func (noopSyncInstrument) RecordOne(context.Context, number.Number, []attribute.KeyValue) {
}
