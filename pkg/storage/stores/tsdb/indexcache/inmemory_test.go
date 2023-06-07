// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/thanos-io/thanos/blob/main/pkg/store/cache/inmemory_test.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Thanos Authors.

// Tests out the index cache implementation.
package indexcache

// func TestInMemoryIndexCache_AvoidsDeadlock(t *testing.T) {
// 	user := "tenant"
// 	metrics := prometheus.NewRegistry()
// 	cache, err := NewInMemoryIndexCacheWithConfig(log.NewNopLogger(), metrics, InMemoryIndexCacheConfig{
// 		MaxItemSize: sliceHeaderSize + 5,
// 		MaxSize:     sliceHeaderSize + 5,
// 	})
// 	assert.NoError(t, err)

// 	l, err := simplelru.NewLRU(math.MaxInt64, func(key, val interface{}) {
// 		// Hack LRU to simulate broken accounting: evictions do not reduce current size.
// 		size := cache.curSize
// 		cache.onEvict(key, val)
// 		cache.curSize = size
// 	})
// 	assert.NoError(t, err)
// 	cache.lru = l

// 	cache.StorePostings(user, ulid.MustNew(0, nil), labels.Label{Name: "test2", Value: "1"}, []byte{42, 33, 14, 67, 11})

// 	assert.Equal(t, uint64(sliceHeaderSize+5), cache.curSize)
// 	assert.Equal(t, float64(cache.curSize), promtest.ToFloat64(cache.currentSize.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(1), promtest.ToFloat64(cache.current.WithLabelValues(cacheTypePostings)))

// 	// This triggers deadlock logic.
// 	cache.StorePostings(user, ulid.MustNew(0, nil), labels.Label{Name: "test1", Value: "1"}, []byte{42})

// 	assert.Equal(t, uint64(sliceHeaderSize+1), cache.curSize)
// 	assert.Equal(t, float64(cache.curSize), promtest.ToFloat64(cache.currentSize.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(1), promtest.ToFloat64(cache.current.WithLabelValues(cacheTypePostings)))
// }

// func TestInMemoryIndexCache_UpdateItem(t *testing.T) {
// 	const maxSize = 2 * (sliceHeaderSize + 1)

// 	var errorLogs []string
// 	errorLogger := log.LoggerFunc(func(kvs ...interface{}) error {
// 		var lvl string
// 		for i := 0; i < len(kvs); i += 2 {
// 			if kvs[i] == "level" {
// 				lvl = fmt.Sprint(kvs[i+1])
// 				break
// 			}
// 		}
// 		if lvl != "error" {
// 			return nil
// 		}
// 		var buf bytes.Buffer
// 		defer func() { errorLogs = append(errorLogs, buf.String()) }()
// 		return log.NewLogfmtLogger(&buf).Log(kvs...)
// 	})

// 	metrics := prometheus.NewRegistry()
// 	cache, err := NewInMemoryIndexCacheWithConfig(log.NewSyncLogger(errorLogger), metrics, InMemoryIndexCacheConfig{
// 		MaxItemSize: maxSize,
// 		MaxSize:     maxSize,
// 	})
// 	assert.NoError(t, err)

// 	user := "tenant"
// 	uid := func(id uint64) ulid.ULID { return ulid.MustNew(uint64(id), nil) }
// 	lbl := labels.Label{Name: "foo", Value: "bar"}
// 	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"), labels.MustNewMatcher(labels.MatchNotRegexp, "baz", ".*")}
// 	shard := &sharding.ShardSelector{ShardIndex: 1, ShardCount: 16}
// 	ctx := context.Background()

// 	for _, tt := range []struct {
// 		typ string
// 		set func(uint64, []byte)
// 		get func(uint64) ([]byte, bool)
// 	}{
// 		{
// 			typ: cacheTypePostings,
// 			set: func(id uint64, b []byte) { cache.StorePostings(user, uid(id), lbl, b) },
// 			get: func(id uint64) ([]byte, bool) {
// 				hits := cache.FetchMultiPostings(ctx, user, uid(id), []labels.Label{lbl})
// 				b, _ := hits.Next()
// 				return b, b != nil
// 			},
// 		},
// 		{
// 			typ: cacheTypeSeriesForRef,
// 			set: func(id uint64, b []byte) {
// 				cache.StoreSeriesForRef(user, uid(id), storage.SeriesRef(id), b)
// 			},
// 			get: func(id uint64) ([]byte, bool) {
// 				seriesRef := storage.SeriesRef(id)
// 				hits, _ := cache.FetchMultiSeriesForRefs(ctx, user, uid(id), []storage.SeriesRef{seriesRef})
// 				b, ok := hits[seriesRef]

// 				return b, ok
// 			},
// 		},
// 		{
// 			typ: cacheTypeExpandedPostings,
// 			set: func(id uint64, b []byte) {
// 				cache.StoreExpandedPostings(user, uid(id), CanonicalLabelMatchersKey(matchers), "strategy", b)
// 			},
// 			get: func(id uint64) ([]byte, bool) {
// 				return cache.FetchExpandedPostings(ctx, user, uid(id), CanonicalLabelMatchersKey(matchers), "strategy")
// 			},
// 		},
// 		{
// 			typ: cacheTypeSeriesForPostings,
// 			set: func(id uint64, b []byte) {
// 				cache.StoreSeriesForPostings(user, uid(id), shard, CanonicalPostingsKey([]storage.SeriesRef{1}), b)
// 			},
// 			get: func(id uint64) ([]byte, bool) {
// 				return cache.FetchSeriesForPostings(ctx, user, uid(id), shard, CanonicalPostingsKey([]storage.SeriesRef{1}))
// 			},
// 		},
// 		{
// 			typ: cacheTypeLabelNames,
// 			set: func(id uint64, b []byte) {
// 				cache.StoreLabelNames(user, uid(id), CanonicalLabelMatchersKey(matchers), b)
// 			},
// 			get: func(id uint64) ([]byte, bool) {
// 				return cache.FetchLabelNames(ctx, user, uid(id), CanonicalLabelMatchersKey(matchers))
// 			},
// 		},
// 		{
// 			typ: cacheTypeLabelValues,
// 			set: func(id uint64, b []byte) {
// 				cache.StoreLabelValues(user, uid(id), fmt.Sprintf("lbl_%d", id), CanonicalLabelMatchersKey(matchers), b)
// 			},
// 			get: func(id uint64) ([]byte, bool) {
// 				return cache.FetchLabelValues(ctx, user, uid(id), fmt.Sprintf("lbl_%d", id), CanonicalLabelMatchersKey(matchers))
// 			},
// 		},
// 	} {
// 		t.Run(tt.typ, func(t *testing.T) {
// 			defer func() { errorLogs = nil }()

// 			// Set value.
// 			tt.set(0, []byte{0})
// 			buf, ok := tt.get(0)
// 			assert.Equal(t, true, ok)
// 			assert.Equal(t, []byte{0}, buf)
// 			assert.Equal(t, float64(sliceHeaderSize+1), promtest.ToFloat64(cache.currentSize.WithLabelValues(tt.typ)))
// 			assert.Equal(t, float64(1), promtest.ToFloat64(cache.current.WithLabelValues(tt.typ)))
// 			assert.Equal(t, []string(nil), errorLogs)

// 			// Set the same value again.
// 			// NB: This used to over-count the value.
// 			tt.set(0, []byte{0})
// 			buf, ok = tt.get(0)
// 			assert.Equal(t, true, ok)
// 			assert.Equal(t, []byte{0}, buf)
// 			assert.Equal(t, float64(sliceHeaderSize+1), promtest.ToFloat64(cache.currentSize.WithLabelValues(tt.typ)))
// 			assert.Equal(t, float64(1), promtest.ToFloat64(cache.current.WithLabelValues(tt.typ)))
// 			assert.Equal(t, []string(nil), errorLogs)

// 			// Set a larger value.
// 			// NB: This used to deadlock when enough values were over-counted and it
// 			// couldn't clear enough space -- repeatedly removing oldest after empty.
// 			tt.set(1, []byte{0, 1})
// 			buf, ok = tt.get(1)
// 			assert.Equal(t, true, ok)
// 			assert.Equal(t, []byte{0, 1}, buf)
// 			assert.Equal(t, float64(sliceHeaderSize+2), promtest.ToFloat64(cache.currentSize.WithLabelValues(tt.typ)))
// 			assert.Equal(t, float64(1), promtest.ToFloat64(cache.current.WithLabelValues(tt.typ)))
// 			assert.Equal(t, []string(nil), errorLogs)

// 			// Mutations to existing values will be ignored.
// 			tt.set(1, []byte{1, 2})
// 			buf, ok = tt.get(1)
// 			assert.Equal(t, true, ok)
// 			assert.Equal(t, []byte{0, 1}, buf)
// 			assert.Equal(t, float64(sliceHeaderSize+2), promtest.ToFloat64(cache.currentSize.WithLabelValues(tt.typ)))
// 			assert.Equal(t, float64(1), promtest.ToFloat64(cache.current.WithLabelValues(tt.typ)))
// 			assert.Equal(t, []string(nil), errorLogs)
// 		})
// 	}
// }

// // This should not happen as we hardcode math.MaxInt, but we still add test to check this out.
// func TestInMemoryIndexCache_MaxNumberOfItemsHit(t *testing.T) {
// 	user := "tenant"
// 	metrics := prometheus.NewRegistry()
// 	cache, err := NewInMemoryIndexCacheWithConfig(log.NewNopLogger(), metrics, InMemoryIndexCacheConfig{
// 		MaxItemSize: 2*sliceHeaderSize + 10,
// 		MaxSize:     2*sliceHeaderSize + 10,
// 	})
// 	assert.NoError(t, err)

// 	l, err := simplelru.NewLRU(2, cache.onEvict)
// 	assert.NoError(t, err)
// 	cache.lru = l

// 	id := ulid.MustNew(0, nil)

// 	cache.StorePostings(user, id, labels.Label{Name: "test", Value: "123"}, []byte{42, 33})
// 	cache.StorePostings(user, id, labels.Label{Name: "test", Value: "124"}, []byte{42, 33})
// 	cache.StorePostings(user, id, labels.Label{Name: "test", Value: "125"}, []byte{42, 33})

// 	assert.Equal(t, uint64(2*sliceHeaderSize+4), cache.curSize)
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.overflow.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(1), promtest.ToFloat64(cache.evicted.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(3), promtest.ToFloat64(cache.added.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.requests.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.hits.WithLabelValues(cacheTypePostings)))
// 	for _, typ := range remove(allCacheTypes, cacheTypePostings) {
// 		assert.Equal(t, float64(0), promtest.ToFloat64(cache.overflow.WithLabelValues(typ)))
// 		assert.Equal(t, float64(0), promtest.ToFloat64(cache.evicted.WithLabelValues(typ)))
// 		assert.Equal(t, float64(0), promtest.ToFloat64(cache.added.WithLabelValues(typ)))
// 		assert.Equal(t, float64(0), promtest.ToFloat64(cache.requests.WithLabelValues(typ)))
// 		assert.Equal(t, float64(0), promtest.ToFloat64(cache.hits.WithLabelValues(typ)))
// 	}
// }

// func TestInMemoryIndexCache_Eviction_WithMetrics(t *testing.T) {
// 	user := "tenant"
// 	metrics := prometheus.NewRegistry()
// 	cache, err := NewInMemoryIndexCacheWithConfig(log.NewNopLogger(), metrics, InMemoryIndexCacheConfig{
// 		MaxItemSize: 2*sliceHeaderSize + 5,
// 		MaxSize:     2*sliceHeaderSize + 5,
// 	})
// 	assert.NoError(t, err)

// 	id := ulid.MustNew(0, nil)
// 	lbls := labels.Label{Name: "test", Value: "123"}
// 	ctx := context.Background()
// 	emptySeriesHits := map[storage.SeriesRef][]byte{}
// 	emptySeriesMisses := []storage.SeriesRef(nil)

// 	testFetchMultiPostings(ctx, t, cache, user, id, []labels.Label{lbls}, nil)

// 	// Add sliceHeaderSize + 2 bytes.
// 	cache.StorePostings(user, id, lbls, []byte{42, 33})
// 	assert.Equal(t, uint64(sliceHeaderSize+2), cache.curSize)
// 	assert.Equal(t, float64(1), promtest.ToFloat64(cache.current.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(sliceHeaderSize+2), promtest.ToFloat64(cache.currentSize.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(sliceHeaderSize+2+cacheKeyPostings{user, id, lbls}.size()), promtest.ToFloat64(cache.totalCurrentSize.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.current.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.currentSize.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.totalCurrentSize.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.overflow.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.overflow.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.evicted.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.evicted.WithLabelValues(cacheTypeSeriesForRef)))

// 	testFetchMultiPostings(ctx, t, cache, user, id, []labels.Label{lbls}, map[labels.Label][]byte{lbls: {42, 33}})

// 	testFetchMultiPostings(ctx, t, cache, user, ulid.MustNew(1, nil), []labels.Label{lbls}, nil)

// 	testFetchMultiPostings(ctx, t, cache, user, id, []labels.Label{{Name: "test", Value: "124"}}, nil)

// 	// Add sliceHeaderSize + 3 more bytes.
// 	cache.StoreSeriesForRef(user, id, 1234, []byte{222, 223, 224})
// 	assert.Equal(t, uint64(2*sliceHeaderSize+5), cache.curSize)
// 	assert.Equal(t, float64(1), promtest.ToFloat64(cache.current.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(sliceHeaderSize+2), promtest.ToFloat64(cache.currentSize.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(sliceHeaderSize+2+cacheKeyPostings{user, id, lbls}.size()), promtest.ToFloat64(cache.totalCurrentSize.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(1), promtest.ToFloat64(cache.current.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(sliceHeaderSize+3), promtest.ToFloat64(cache.currentSize.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(sliceHeaderSize+3+cacheKeySeriesForRef{user, id, 1234}.size()), promtest.ToFloat64(cache.totalCurrentSize.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.overflow.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.overflow.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.evicted.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.evicted.WithLabelValues(cacheTypeSeriesForRef)))

// 	sHits, sMisses := cache.FetchMultiSeriesForRefs(ctx, user, id, []storage.SeriesRef{1234})
// 	assert.Equal(t, map[storage.SeriesRef][]byte{1234: {222, 223, 224}}, sHits, "key exists")
// 	assert.Equal(t, emptySeriesMisses, sMisses)

// 	lbls2 := labels.Label{Name: "test", Value: "124"}

// 	// Add sliceHeaderSize + 5 + 16 bytes, should fully evict 2 last items.
// 	v := []byte{42, 33, 14, 67, 11}
// 	for i := 0; i < sliceHeaderSize; i++ {
// 		v = append(v, 3)
// 	}
// 	cache.StorePostings(user, id, lbls2, v)

// 	assert.Equal(t, uint64(2*sliceHeaderSize+5), cache.curSize)
// 	assert.Equal(t, float64(1), promtest.ToFloat64(cache.current.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(2*sliceHeaderSize+5), promtest.ToFloat64(cache.currentSize.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(2*sliceHeaderSize+5+cacheKeyPostings{user, id, lbls}.size()), promtest.ToFloat64(cache.totalCurrentSize.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.current.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.currentSize.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.totalCurrentSize.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.overflow.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.overflow.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(1), promtest.ToFloat64(cache.evicted.WithLabelValues(cacheTypePostings)))     // Eviction.
// 	assert.Equal(t, float64(1), promtest.ToFloat64(cache.evicted.WithLabelValues(cacheTypeSeriesForRef))) // Eviction.

// 	// Evicted.
// 	testFetchMultiPostings(ctx, t, cache, user, id, []labels.Label{lbls}, nil)

// 	sHits, sMisses = cache.FetchMultiSeriesForRefs(ctx, user, id, []storage.SeriesRef{1234})
// 	assert.Equal(t, emptySeriesHits, sHits, "no such key")
// 	assert.Equal(t, []storage.SeriesRef{1234}, sMisses)

// 	testFetchMultiPostings(ctx, t, cache, user, id, []labels.Label{lbls2}, map[labels.Label][]byte{lbls2: v})

// 	// Add same item again.
// 	cache.StorePostings(user, id, lbls2, v)

// 	assert.Equal(t, uint64(2*sliceHeaderSize+5), cache.curSize)
// 	assert.Equal(t, float64(1), promtest.ToFloat64(cache.current.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(2*sliceHeaderSize+5), promtest.ToFloat64(cache.currentSize.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(2*sliceHeaderSize+5+cacheKeyPostings{user, id, lbls}.size()), promtest.ToFloat64(cache.totalCurrentSize.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.current.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.currentSize.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.totalCurrentSize.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.overflow.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.overflow.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(1), promtest.ToFloat64(cache.evicted.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(1), promtest.ToFloat64(cache.evicted.WithLabelValues(cacheTypeSeriesForRef)))

// 	testFetchMultiPostings(ctx, t, cache, user, id, []labels.Label{lbls2}, map[labels.Label][]byte{lbls2: v})

// 	// Add too big item.
// 	cache.StorePostings(user, id, labels.Label{Name: "test", Value: "toobig"}, append(v, 5))
// 	assert.Equal(t, uint64(2*sliceHeaderSize+5), cache.curSize)
// 	assert.Equal(t, float64(1), promtest.ToFloat64(cache.current.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(2*sliceHeaderSize+5), promtest.ToFloat64(cache.currentSize.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(2*sliceHeaderSize+5+cacheKeyPostings{user, id, lbls}.size()), promtest.ToFloat64(cache.totalCurrentSize.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.current.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.currentSize.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.totalCurrentSize.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(1), promtest.ToFloat64(cache.overflow.WithLabelValues(cacheTypePostings))) // Overflow.
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.overflow.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(1), promtest.ToFloat64(cache.evicted.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(1), promtest.ToFloat64(cache.evicted.WithLabelValues(cacheTypeSeriesForRef)))

// 	_, _, ok := cache.lru.RemoveOldest()
// 	assert.True(t, ok, "something to remove")

// 	assert.Equal(t, uint64(0), cache.curSize)
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.current.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.currentSize.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.totalCurrentSize.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.current.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.currentSize.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.totalCurrentSize.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(1), promtest.ToFloat64(cache.overflow.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.overflow.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(2), promtest.ToFloat64(cache.evicted.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(1), promtest.ToFloat64(cache.evicted.WithLabelValues(cacheTypeSeriesForRef)))

// 	_, _, ok = cache.lru.RemoveOldest()
// 	assert.True(t, !ok, "nothing to remove")

// 	lbls3 := labels.Label{Name: "test", Value: "124"}

// 	cache.StorePostings(user, id, lbls3, []byte{})

// 	assert.Equal(t, uint64(sliceHeaderSize), cache.curSize)
// 	assert.Equal(t, float64(1), promtest.ToFloat64(cache.current.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(sliceHeaderSize), promtest.ToFloat64(cache.currentSize.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(sliceHeaderSize+cacheKeyPostings{user, id, lbls3}.size()), promtest.ToFloat64(cache.totalCurrentSize.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.current.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.currentSize.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.totalCurrentSize.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(1), promtest.ToFloat64(cache.overflow.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.overflow.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(2), promtest.ToFloat64(cache.evicted.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(1), promtest.ToFloat64(cache.evicted.WithLabelValues(cacheTypeSeriesForRef)))

// 	testFetchMultiPostings(ctx, t, cache, user, id, []labels.Label{lbls3}, map[labels.Label][]byte{lbls3: {}})

// 	// nil works and still allocates empty slice.
// 	lbls4 := labels.Label{Name: "test", Value: "125"}
// 	cache.StorePostings(user, id, lbls4, []byte(nil))

// 	assert.Equal(t, 2*uint64(sliceHeaderSize), cache.curSize)
// 	assert.Equal(t, float64(2), promtest.ToFloat64(cache.current.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, 2*float64(sliceHeaderSize), promtest.ToFloat64(cache.currentSize.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, 2*float64(sliceHeaderSize+cacheKeyPostings{user, id, lbls4}.size()), promtest.ToFloat64(cache.totalCurrentSize.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.current.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.currentSize.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.totalCurrentSize.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(1), promtest.ToFloat64(cache.overflow.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(0), promtest.ToFloat64(cache.overflow.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(2), promtest.ToFloat64(cache.evicted.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(1), promtest.ToFloat64(cache.evicted.WithLabelValues(cacheTypeSeriesForRef)))

// 	testFetchMultiPostings(ctx, t, cache, user, id, []labels.Label{lbls4}, map[labels.Label][]byte{lbls4: {}})

// 	// Other metrics.
// 	assert.Equal(t, float64(4), promtest.ToFloat64(cache.added.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(1), promtest.ToFloat64(cache.added.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(9), promtest.ToFloat64(cache.requests.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(2), promtest.ToFloat64(cache.requests.WithLabelValues(cacheTypeSeriesForRef)))
// 	assert.Equal(t, float64(5), promtest.ToFloat64(cache.hits.WithLabelValues(cacheTypePostings)))
// 	assert.Equal(t, float64(1), promtest.ToFloat64(cache.hits.WithLabelValues(cacheTypeSeriesForRef)))
// }

// func testFetchMultiPostings(ctx context.Context, t *testing.T, cache IndexCache, user string, id ulid.ULID, keys []labels.Label, expectedHits map[labels.Label][]byte) {
// 	t.Helper()
// 	pHits := cache.FetchMultiPostings(ctx, user, id, keys)
// 	expectedResult := &MapIterator[labels.Label]{M: expectedHits, Keys: keys}

// 	assert.Equal(t, expectedResult.Remaining(), pHits.Remaining())
// 	for exp, hasNext := expectedResult.Next(); hasNext; exp, hasNext = expectedResult.Next() {
// 		actual, ok := pHits.Next()
// 		assert.True(t, ok)
// 		assert.Equal(t, exp, actual)
// 	}
// 	_, ok := pHits.Next()
// 	assert.False(t, ok)
// }
