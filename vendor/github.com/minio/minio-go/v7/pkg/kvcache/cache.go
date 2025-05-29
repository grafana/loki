/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2015-2025 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package kvcache

import "sync"

// Cache - Provides simple mechanism to hold any key value in memory
// wrapped around via sync.Map but typed with generics.
type Cache[K comparable, V any] struct {
	m sync.Map
}

// Delete delete the key
func (r *Cache[K, V]) Delete(key K) {
	r.m.Delete(key)
}

// Get - Returns a value of a given key if it exists.
func (r *Cache[K, V]) Get(key K) (value V, ok bool) {
	return r.load(key)
}

// Set - Will persist a value into cache.
func (r *Cache[K, V]) Set(key K, value V) {
	r.store(key, value)
}

func (r *Cache[K, V]) load(key K) (V, bool) {
	value, ok := r.m.Load(key)
	if !ok {
		var zero V
		return zero, false
	}
	return value.(V), true
}

func (r *Cache[K, V]) store(key K, value V) {
	r.m.Store(key, value)
}
