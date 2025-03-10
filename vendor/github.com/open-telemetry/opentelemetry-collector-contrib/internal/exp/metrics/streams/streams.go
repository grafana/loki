// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package streams // import "github.com/open-telemetry/opentelemetry-collector-contrib/internal/exp/metrics/streams"

import (
	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/exp/metrics/identity"
)

// Sequence of streams that can be iterated upon
type Seq[T any] func(yield func(identity.Stream, T) bool) bool

// Map defines a collection of items tracked by a stream-id and the operations
// on it
type Map[T any] interface {
	Load(identity.Stream) (T, bool)
	Store(identity.Stream, T) error
	Delete(identity.Stream)
	Items() func(yield func(identity.Stream, T) bool) bool
	Len() int
	Clear()
}

var _ Map[any] = HashMap[any](nil)

type HashMap[T any] map[identity.Stream]T

func (m HashMap[T]) Load(id identity.Stream) (T, bool) {
	v, ok := (map[identity.Stream]T)(m)[id]
	return v, ok
}

func (m HashMap[T]) Store(id identity.Stream, v T) error {
	(map[identity.Stream]T)(m)[id] = v
	return nil
}

func (m HashMap[T]) Delete(id identity.Stream) {
	delete((map[identity.Stream]T)(m), id)
}

func (m HashMap[T]) Items() func(yield func(identity.Stream, T) bool) bool {
	return func(yield func(identity.Stream, T) bool) bool {
		for id, v := range (map[identity.Stream]T)(m) {
			if !yield(id, v) {
				break
			}
		}
		return false
	}
}

func (m HashMap[T]) Len() int {
	return len((map[identity.Stream]T)(m))
}

func (m HashMap[T]) Clear() {
	clear(m)
}

// Evictors remove the "least important" stream based on some strategy such as
// the oldest, least active, etc.
//
// Returns whether a stream was evicted and if so the now gone stream id
type Evictor interface {
	Evict() (gone identity.Stream, ok bool)
}

type DataPointSlice[DP DataPoint[DP]] interface {
	Len() int
	At(i int) DP
	AppendEmpty() DP
}

type DataPoint[Self any] interface {
	Timestamp() pcommon.Timestamp
	Attributes() pcommon.Map
	CopyTo(dest Self)
}
