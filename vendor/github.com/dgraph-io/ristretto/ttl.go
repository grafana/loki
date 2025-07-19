/*
 * Copyright 2020 Dgraph Labs, Inc. and Contributors
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

package ristretto

import (
	"sync"
	"time"
)

var (
	// TODO: find the optimal value or make it configurable.
	bucketDurationSecs = int64(5)
)

func storageBucket(t time.Time) int64 {
	return (t.Unix() / bucketDurationSecs) + 1
}

func cleanupBucket(t time.Time) int64 {
	// The bucket to cleanup is always behind the storage bucket by one so that
	// no elements in that bucket (which might not have expired yet) are deleted.
	return storageBucket(t) - 1
}

// bucket type is a map of key to conflict.
type bucket map[uint64]uint64

// expirationMap is a map of bucket number to the corresponding bucket.
type expirationMap struct {
	sync.RWMutex
	buckets map[int64]bucket
}

func newExpirationMap() *expirationMap {
	return &expirationMap{
		buckets: make(map[int64]bucket),
	}
}

func (m *expirationMap) add(key, conflict uint64, expiration time.Time) {
	if m == nil {
		return
	}

	// Items that don't expire don't need to be in the expiration map.
	if expiration.IsZero() {
		return
	}

	bucketNum := storageBucket(expiration)
	m.Lock()
	defer m.Unlock()

	b, ok := m.buckets[bucketNum]
	if !ok {
		b = make(bucket)
		m.buckets[bucketNum] = b
	}
	b[key] = conflict
}

func (m *expirationMap) update(key, conflict uint64, oldExpTime, newExpTime time.Time) {
	if m == nil {
		return
	}

	m.Lock()
	defer m.Unlock()

	oldBucketNum := storageBucket(oldExpTime)
	oldBucket, ok := m.buckets[oldBucketNum]
	if ok {
		delete(oldBucket, key)
	}

	newBucketNum := storageBucket(newExpTime)
	newBucket, ok := m.buckets[newBucketNum]
	if !ok {
		newBucket = make(bucket)
		m.buckets[newBucketNum] = newBucket
	}
	newBucket[key] = conflict
}

func (m *expirationMap) del(key uint64, expiration time.Time) {
	if m == nil {
		return
	}

	bucketNum := storageBucket(expiration)
	m.Lock()
	defer m.Unlock()
	_, ok := m.buckets[bucketNum]
	if !ok {
		return
	}
	delete(m.buckets[bucketNum], key)
}

// cleanup removes all the items in the bucket that was just completed. It deletes
// those items from the store, and calls the onEvict function on those items.
// This function is meant to be called periodically.
func (m *expirationMap) cleanup(store store, policy policy, onEvict itemCallback) {
	if m == nil {
		return
	}

	m.Lock()
	now := time.Now()
	bucketNum := cleanupBucket(now)
	keys := m.buckets[bucketNum]
	delete(m.buckets, bucketNum)
	m.Unlock()

	for key, conflict := range keys {
		// Sanity check. Verify that the store agrees that this key is expired.
		if store.Expiration(key).After(now) {
			continue
		}

		cost := policy.Cost(key)
		policy.Del(key)
		_, value := store.Del(key, conflict)

		if onEvict != nil {
			onEvict(&Item{Key: key,
				Conflict: conflict,
				Value:    value,
				Cost:     cost,
			})
		}
	}
}
