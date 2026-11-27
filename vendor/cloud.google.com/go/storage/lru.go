// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package storage

import (
	"container/list"
)

// lruCache is a generic Least Recently Used cache. It is not thread-safe.
type lruCache[K comparable, V any] struct {
	entries   map[K]*list.Element
	evictList *list.List
	limit     int
}

type cacheEntry[K comparable, V any] struct {
	key   K
	value V
}

func newLRUCache[K comparable, V any](limit int) *lruCache[K, V] {
	return &lruCache[K, V]{
		entries:   make(map[K]*list.Element),
		evictList: list.New(),
		limit:     limit,
	}
}

func (c *lruCache[K, V]) get(key K) (V, bool) {
	if c == nil {
		var zero V
		return zero, false
	}

	if elem, ok := c.entries[key]; ok {
		c.evictList.MoveToFront(elem)
		return elem.Value.(*cacheEntry[K, V]).value, true
	}
	var zero V
	return zero, false
}

func (c *lruCache[K, V]) put(key K, val V) {
	if c == nil {
		return
	}

	// If element already exists, update the value and promote it.
	if elem, ok := c.entries[key]; ok {
		c.evictList.MoveToFront(elem)
		elem.Value.(*cacheEntry[K, V]).value = val
		return
	}

	// Add new element to front
	elem := c.evictList.PushFront(&cacheEntry[K, V]{key: key, value: val})
	c.entries[key] = elem

	// Evict oldest element if limit exceeded.
	if c.evictList.Len() > c.limit {
		c.removeOldest()
	}
}

func (c *lruCache[K, V]) evict(key K) {
	if c == nil {
		return
	}

	if elem, ok := c.entries[key]; ok {
		c.removeElement(elem)
	}
}

func (c *lruCache[K, V]) removeOldest() {
	elem := c.evictList.Back()
	if elem != nil {
		c.removeElement(elem)
	}
}

func (c *lruCache[K, V]) removeElement(elem *list.Element) {
	c.evictList.Remove(elem)
	kv := elem.Value.(*cacheEntry[K, V])
	delete(c.entries, kv.key)
}
