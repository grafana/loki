// Ordered map container
// Works like the Golang `map` built-in, but preserves order that key/value
// pairs were added when iterating.

package orderedmap

import (
	"context"
	"fmt"
	"iter"
	"reflect"
	"slices"
	"strings"

	wk8orderedmap "github.com/pb33f/ordered-map/v2"
)

// Pair represents a key/value pair in an ordered map returned for iteration.
type Pair[K comparable, V any] interface {
	Key() K
	KeyPtr() *K
	Value() V
	ValuePtr() *V
	Next() Pair[K, V]
}

// Map represents an ordered map where the key must be a comparable type, the ordering is based on insertion order.
type Map[K comparable, V any] struct {
	*wk8orderedmap.OrderedMap[K, V]
}

type wrapPair[K comparable, V any] struct {
	*wk8orderedmap.Pair[K, V]
}

// New creates an ordered map generic object.
func New[K comparable, V any]() *Map[K, V] {
	return &Map[K, V]{
		OrderedMap: wk8orderedmap.New[K, V](),
	}
}

// GetKeyType returns the reflection type of the key.
func (o *Map[K, V]) GetKeyType() reflect.Type {
	return reflect.TypeOf(new(K))
}

// GetValueType returns the reflection type of the value.
func (o *Map[K, V]) GetValueType() reflect.Type {
	return reflect.TypeOf(new(V))
}

// GetOrZero will return the value for the key if it exists, otherwise it will return the zero value for the value type.
func (o *Map[K, V]) GetOrZero(k K) V {
	v, ok := o.OrderedMap.Get(k)
	if !ok {
		var zero V
		return zero
	}
	return v
}

// First returns the first pair in the map useful for iteration.
func (o *Map[K, V]) First() Pair[K, V] {
	if o == nil {
		return nil
	}

	pair := o.OrderedMap.Oldest()
	if pair == nil {
		return nil
	}
	return &wrapPair[K, V]{
		Pair: pair,
	}
}

// FromOldest returns an iterator that yields the oldest key-value pair in the map.
func (o *Map[K, V]) FromOldest() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		if o == nil {
			return
		}

		for k, v := range o.OrderedMap.FromOldest() {
			if !yield(k, v) {
				return
			}
		}
	}
}

// FromNewest returns an iterator that yields the newest key-value pair in the map.
func (o *Map[K, V]) FromNewest() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		if o == nil {
			return
		}

		for k, v := range o.OrderedMap.FromNewest() {
			if !yield(k, v) {
				return
			}
		}
	}
}

// FromNewest returns an iterator that yields the newest key-value pair in the map.
func (o *Map[K, V]) KeysFromOldest() iter.Seq[K] {
	return func(yield func(K) bool) {
		if o == nil {
			return
		}

		for k := range o.OrderedMap.KeysFromOldest() {
			if !yield(k) {
				return
			}
		}
	}
}

// KeysFromNewest returns an iterator that yields the newest key in the map.
func (o *Map[K, V]) KeysFromNewest() iter.Seq[K] {
	return func(yield func(K) bool) {
		if o == nil {
			return
		}

		for k := range o.OrderedMap.KeysFromNewest() {
			if !yield(k) {
				return
			}
		}
	}
}

// ValuesFromOldest returns an iterator that yields the oldest value in the map.
func (o *Map[K, V]) ValuesFromOldest() iter.Seq[V] {
	return func(yield func(V) bool) {
		if o == nil {
			return
		}
		for v := range o.OrderedMap.ValuesFromOldest() {
			if !yield(v) {
				return
			}
		}
	}
}

// ValuesFromNewest returns an iterator that yields the newest value in the map.
func (o *Map[K, V]) ValuesFromNewest() iter.Seq[V] {
	return func(yield func(V) bool) {
		if o == nil {
			return
		}
		for v := range o.OrderedMap.ValuesFromNewest() {
			if !yield(v) {
				return
			}
		}
	}
}

// From creates a new ordered map from an iterator.
func From[K comparable, V any](iter iter.Seq2[K, V]) *Map[K, V] {
	return &Map[K, V]{
		OrderedMap: wk8orderedmap.From(iter),
	}
}

// NewPair instantiates a `Pair` object for use with `FromPairs()`.
func NewPair[K comparable, V any](key K, value V) Pair[K, V] {
	return &wrapPair[K, V]{
		Pair: &wk8orderedmap.Pair[K, V]{
			Key:   key,
			Value: value,
		},
	}
}

// FromPairs creates an `OrderedMap` from an array of pairs.
// Use `NewPair()` to generate input parameters.
func FromPairs[K comparable, V any](pairs ...Pair[K, V]) *Map[K, V] {
	om := New[K, V]()
	for _, pair := range pairs {
		om.Set(pair.Key(), pair.Value())
	}
	return om
}

// IsZero is required to support `omitempty` tag for YAML/JSON marshaling.
func (o *Map[K, V]) IsZero() bool {
	return Len(o) == 0
}

// Next returns the next pair in the map when iterating.
func (p *wrapPair[K, V]) Next() Pair[K, V] {
	next := p.Pair.Next()
	if next == nil {
		return nil
	}
	return &wrapPair[K, V]{
		Pair: next,
	}
}

// Key returns the key of the pair.
func (p *wrapPair[K, V]) Key() K {
	return p.Pair.Key
}

// KeyPtr returns a pointer to the key of the pair.
func (p *wrapPair[K, V]) KeyPtr() *K {
	return &p.Pair.Key
}

// Value returns the value of the pair.
func (p *wrapPair[K, V]) Value() V {
	return p.Pair.Value
}

// ValuePtr returns a pointer to the value of the pair.
func (p *wrapPair[K, V]) ValuePtr() *V {
	return &p.Pair.Value
}

// Len returns the length of a container implementing a `Len()` method.
// Safely returns zero on nil pointer.
func Len[K comparable, V any](m *Map[K, V]) int {
	if m == nil {
		return 0
	}
	return m.Len()
}

// Iterate the map in order.
// Safely handles nil pointer.
// Be sure to iterate to end or cancel the context when done to release
// resources.
func Iterate[K comparable, V any](ctx context.Context, m *Map[K, V]) <-chan Pair[K, V] {
	c := make(chan Pair[K, V])
	if Len(m) == 0 {
		close(c)
		return c
	}
	go func() {
		defer close(c)
		for pair := First(m); pair != nil; pair = pair.Next() {
			select {
			case c <- pair:
			case <-ctx.Done():
				return
			}
		}
	}()
	return c
}

// ToOrderedMap converts a `map` to `OrderedMap`.
func ToOrderedMap[K comparable, V any](m map[K]V) *Map[K, V] {
	om := New[K, V]()
	for k, v := range m {
		om.Set(k, v)
	}
	return SortAlpha(om)
}

// First returns map's first pair for iteration.
// Safely handles nil pointer.
func First[K comparable, V any](m *Map[K, V]) Pair[K, V] {
	if m == nil {
		return nil
	}
	return m.First()
}

// Cast converts `any` to `Map`.
func Cast[K comparable, V any](v any) *Map[K, V] {
	if v == nil {
		return nil
	}

	m, ok := v.(*Map[K, V])
	if !ok {
		return nil
	}

	return m
}

// SortAlpha sorts the map by keys in alphabetical order.
func SortAlpha[K comparable, V any](m *Map[K, V]) *Map[K, V] {
	if m == nil {
		return nil
	}

	om := New[K, V]()

	type key struct {
		key string
		k   K
	}

	keys := []key{}
	for pair := First(m); pair != nil; pair = pair.Next() {
		keys = append(keys, key{
			key: fmt.Sprintf("%v", pair.Key()),
			k:   pair.Key(),
		})
	}

	slices.SortFunc(keys, func(a, b key) int {
		return strings.Compare(a.key, b.key)
	})

	for _, k := range keys {
		om.Set(k.k, m.GetOrZero(k.k))
	}

	return om
}
