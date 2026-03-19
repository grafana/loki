package limits

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRateStore_Record(t *testing.T) {
	t.Run("fills the rate buckets", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			store := newRateStore(5*time.Minute, time.Minute)
			store.Record("scope1", 0x1, 100, time.Now())
			expectedBuckets := make([]rateBucket, 5)
			expectedBuckets[0].value = 100
			expectedBuckets[0].ts = time.Now().UnixNano()
			requireBuckets(t, store, "scope1", 0x1, expectedBuckets)
			// Advance the clock 1 minute, the next bucket should be used.
			time.Sleep(time.Minute)
			store.Record("scope1", 0x1, 50, time.Now())
			expectedBuckets[1].value = 50
			expectedBuckets[1].ts = time.Now().UnixNano()
			requireBuckets(t, store, "scope1", 0x1, expectedBuckets)
			// Advance the clock 59 seconds, the same bucket should be used.
			time.Sleep(59 * time.Second)
			store.Record("scope1", 0x1, 25, time.Now())
			expectedBuckets[1].value = 75
			requireBuckets(t, store, "scope1", 0x1, expectedBuckets)
			// Advance time until we wrap around to the original bucket.
			time.Sleep(time.Second + 3*time.Minute)
			store.Record("scope1", 0x1, 10, time.Now())
			expectedBuckets[0].value = 10
			expectedBuckets[0].ts = time.Now().UnixNano()
			requireBuckets(t, store, "scope1", 0x1, expectedBuckets)
		})
	})

	t.Run("scopes are isolated", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			store := newRateStore(5*time.Minute, time.Minute)
			store.Record("scope1", 0x1, 100, time.Now())
			store.Record("scope2", 0x1, 50, time.Now())
			require.Len(t, store.scopes, 2)
			entries1, ok := store.scopes["scope1"]
			require.True(t, ok)
			require.Len(t, entries1, 1)
			buckets1 := make([]rateBucket, 5)
			buckets1[0].value = 100
			buckets1[0].ts = time.Now().UnixNano()
			requireBuckets(t, store, "scope1", 0x1, buckets1)
			entries2, ok := store.scopes["scope2"]
			require.True(t, ok)
			require.Len(t, entries2, 1)
			buckets2 := make([]rateBucket, 5)
			buckets2[0].value = 50
			buckets2[0].ts = time.Now().UnixNano()
			requireBuckets(t, store, "scope2", 0x1, buckets2)
		})
	})
}

func TestRateStore_RecordBatch(t *testing.T) {
	t.Run("fills the rate buckets", func(t *testing.T) {

	})

	t.Run("scopes are isolated", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			store := newRateStore(5*time.Minute, time.Minute)
			store.RecordBatch(rateRecordBatchParams{{
				scope: "scope1",
				key:   0x1,
				value: 100,
				ts:    time.Now(),
			}, {
				scope: "scope2",
				key:   0x2,
				value: 50,
				ts:    time.Now(),
			}})
			require.Len(t, store.scopes, 2)
			entries1, ok := store.scopes["scope1"]
			require.True(t, ok)
			buckets1 := make([]rateBucket, 5)
			buckets1[0].ts = time.Now().UnixNano()
			buckets1[0].value = 100
			requireBuckets(t, store, "scope1", 0x1, buckets1)
			require.Len(t, entries1, 1)
			entries2, ok := store.scopes["scope2"]
			require.True(t, ok)
			require.Len(t, entries2, 1)
			buckets2 := make([]rateBucket, 5)
			buckets2[0].ts = time.Now().UnixNano()
			buckets2[0].value = 50
			requireBuckets(t, store, "scope2", 0x2, buckets2)
		})
	})
}

func TestRateStore_Rate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := newRateStore(5*time.Minute, time.Minute)
		require.Equal(t, float64(0), store.Rate("scope1", 0x1))
		store.Record("scope1", 0x1, 100, time.Now())
		// 100 averaged over 5 minutes is 100 / 300 = 0.3333.
		actual := store.Rate("scope1", 0x1)
		require.GreaterOrEqual(t, actual, float64(0.3))
		require.LessOrEqual(t, actual, float64(0.34))
		// Add another 200, and over 5 minutes it is 300/300 = 1.
		store.Record("scope1", 0x1, 200, time.Now())
		require.Equal(t, 1.0, store.Rate("scope1", 0x1))
		// Move forward in time so the first bucket is now outside the window.
		time.Sleep(6 * time.Minute)
		require.Equal(t, float64(0), store.Rate("scope1", 0x1))
	})
}

func TestRateStore_RatesForScope(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		store := newRateStore(5*time.Minute, time.Minute)
		store.Record("scope1", 0x1, 300, time.Now())
		store.Record("scope1", 0x2, 600, time.Now())
		rates := store.RatesForScope("scope1")
		require.Len(t, rates, 2)
		require.Equal(t, float64(1.0), rates[0x1])
		require.Equal(t, float64(2.0), rates[0x2])
		rates = store.RatesForScope("scope2")
		require.Nil(t, rates)
	})
}

func requireBuckets(t *testing.T, store *rateStore, scope string, key uint64, expected []rateBucket) {
	entries, ok := store.scopes[scope]
	require.True(t, ok)
	entry, ok := entries[key]
	require.True(t, ok)
	require.Equal(t, expected, entry.buckets)
}
