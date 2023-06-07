// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/thanos-io/thanos/blob/main/pkg/store/cache/cache_test.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Thanos Authors.

package indexcache

// func TestMain(m *testing.M) {
// 	test.VerifyNoLeakTestMain(m)
// }

// func TestCanonicalLabelMatchersKey(t *testing.T) {
// 	foo := labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")
// 	bar := labels.MustNewMatcher(labels.MatchEqual, "bar", "foo")

// 	assert.Equal(t, CanonicalLabelMatchersKey([]*labels.Matcher{foo, bar}), CanonicalLabelMatchersKey([]*labels.Matcher{bar, foo}))
// }

// func BenchmarkCanonicalLabelMatchersKey(b *testing.B) {
// 	ms := make([]*labels.Matcher, 20)
// 	for i := range ms {
// 		ms[i] = labels.MustNewMatcher(labels.MatchType(i%4), fmt.Sprintf("%04x", i%3), fmt.Sprintf("%04x", i%2))
// 	}
// 	for _, l := range []int{1, 5, 10, 20} {
// 		b.Run(fmt.Sprintf("%d matchers", l), func(b *testing.B) {
// 			b.ResetTimer()
// 			for i := 0; i < b.N; i++ {
// 				_ = CanonicalLabelMatchersKey(ms[:l])
// 			}
// 		})
// 	}
// }

// func BenchmarkCanonicalPostingsKey(b *testing.B) {
// 	ms := make([]storage.SeriesRef, 1_000_000)
// 	for i := range ms {
// 		ms[i] = storage.SeriesRef(i)
// 	}
// 	for numPostings := 10; numPostings <= len(ms); numPostings *= 10 {
// 		b.Run(fmt.Sprintf("%d postings", numPostings), func(b *testing.B) {
// 			b.ReportAllocs()
// 			for i := 0; i < b.N; i++ {
// 				_ = CanonicalPostingsKey(ms[:numPostings])
// 			}
// 		})
// 	}
// }

// func TestUnsafeCastPostingsToBytes(t *testing.T) {
// 	slowPostingsToBytes := func(postings []storage.SeriesRef) []byte {
// 		byteSlice := make([]byte, len(postings)*8)
// 		for i, posting := range postings {
// 			for octet := 0; octet < 8; octet++ {
// 				byteSlice[i*8+octet] = byte(posting >> (octet * 8))
// 			}
// 		}
// 		return byteSlice
// 	}
// 	t.Run("base case", func(t *testing.T) {
// 		postings := []storage.SeriesRef{1, 2}
// 		assert.Equal(t, slowPostingsToBytes(postings), unsafeCastPostingsToBytes(postings))
// 	})
// 	t.Run("zero-length postings", func(t *testing.T) {
// 		postings := make([]storage.SeriesRef, 0)
// 		assert.Equal(t, slowPostingsToBytes(postings), unsafeCastPostingsToBytes(postings))
// 	})
// 	t.Run("nil postings", func(t *testing.T) {
// 		assert.Equal(t, []byte(nil), unsafeCastPostingsToBytes(nil))
// 	})
// 	t.Run("more than 256 postings", func(t *testing.T) {
// 		// Only casting a slice pointer truncates all postings to only their last byte.
// 		postings := make([]storage.SeriesRef, 300)
// 		for i := range postings {
// 			postings[i] = storage.SeriesRef(i + 1)
// 		}
// 		assert.Equal(t, slowPostingsToBytes(postings), unsafeCastPostingsToBytes(postings))
// 	})
// }

// func TestCanonicalPostingsKey(t *testing.T) {
// 	t.Run("same length postings have different hashes", func(t *testing.T) {
// 		postings1 := []storage.SeriesRef{1, 2, 3, 4}
// 		postings2 := []storage.SeriesRef{5, 6, 7, 8}

// 		assert.NotEqual(t, CanonicalPostingsKey(postings1), CanonicalPostingsKey(postings2))
// 	})

// 	t.Run("when postings are a subset of each other, they still have different hashes", func(t *testing.T) {
// 		postings1 := []storage.SeriesRef{1, 2, 3, 4}
// 		postings2 := []storage.SeriesRef{1, 2, 3, 4, 5}

// 		assert.NotEqual(t, CanonicalPostingsKey(postings1), CanonicalPostingsKey(postings2))
// 	})

// 	t.Run("same postings with different slice capacities have same hashes", func(t *testing.T) {
// 		postings1 := []storage.SeriesRef{1, 2, 3, 4}
// 		postings2 := make([]storage.SeriesRef, 4, 8)
// 		copy(postings2, postings1)

// 		assert.Equal(t, CanonicalPostingsKey(postings1), CanonicalPostingsKey(postings2))
// 	})

// 	t.Run("postings key is a base64-encoded string (i.e. is printable)", func(t *testing.T) {
// 		key := CanonicalPostingsKey([]storage.SeriesRef{1, 2, 3, 4})
// 		_, err := base64.RawURLEncoding.DecodeString(string(key))
// 		assert.NoError(t, err)
// 	})
// }
