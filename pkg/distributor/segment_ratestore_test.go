package distributor

import (
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/stretchr/testify/require"
)

func TestSegmentRateStore_UpdateRate(t *testing.T) {
	t.Run("buckets of all zeros", func(t *testing.T) {
		clock := quartz.NewMock(t)
		s := newSegmentationKeyRateStore(5*time.Minute, time.Minute)
		for range 5 {
			rate := s.Update("tenant", SegmentationKey("test"), 0, clock.Now())
			require.Equal(t, uint64(0), rate)
			// Update all buckets by advancing the clock one bucket size
			// duration at a time.
			clock.Advance(time.Minute)
		}
		// Ensure all buckets have been updated and each bucket contains zero.
		s.withBuckets("tenant", SegmentationKey("test"), func(buckets []segmentationKeyRateBucket) {
			clock := quartz.NewMock(t)
			require.Len(t, buckets, 5)
			for _, bucket := range buckets {
				require.False(t, bucket.start.IsZero())
				require.Equal(t, clock.Now(), bucket.start)
				require.Equal(t, uint64(0), bucket.size)
				clock.Advance(time.Minute)
			}
		})
	})

	t.Run("update each bucket with different rate", func(t *testing.T) {
		clock := quartz.NewMock(t)
		s := newSegmentationKeyRateStore(5*time.Minute, time.Minute)
		for i := range 5 {
			_ = s.Update("tenant", SegmentationKey("test"), uint64(i), clock.Now())
			// Update all buckets by advancing the clock one bucket size
			// duration at a time.
			clock.Advance(time.Minute)
		}
		// Ensure all buckets have been updated and each bucket contains its
		// expected value.
		s.withBuckets("tenant", SegmentationKey("test"), func(buckets []segmentationKeyRateBucket) {
			clock := quartz.NewMock(t)
			require.Len(t, buckets, 5)
			// Each bucket should contain the value equivalent to its index
			// in the slice.
			for i, bucket := range buckets {
				require.False(t, bucket.start.IsZero())
				require.Equal(t, clock.Now(), bucket.start)
				require.Equal(t, uint64(i), bucket.size)
				clock.Advance(time.Minute)
			}
		})
	})

	t.Run("buckets wrap around at the end of the window", func(t *testing.T) {
		clock := quartz.NewMock(t)
		s := newSegmentationKeyRateStore(5*time.Minute, time.Minute)
		s.Update("tenant", SegmentationKey("test"), 1, clock.Now())
		// Check the buckets matches expectations.
		s.withBuckets("tenant", SegmentationKey("test"), func(buckets []segmentationKeyRateBucket) {
			require.Equal(t, uint64(1), buckets[0].size)
			// All other buckets should contain zeros.
			require.Equal(t, uint64(0), buckets[1].size)
			require.Equal(t, uint64(0), buckets[2].size)
			require.Equal(t, uint64(0), buckets[3].size)
			require.Equal(t, uint64(0), buckets[4].size)
		})
		// Advance the clock 5 minutes.
		clock.Advance(5 * time.Minute)
		s.Update("tenant", SegmentationKey("test"), 2, clock.Now())
		// Check the buckets matches expectations.
		s.withBuckets("tenant", SegmentationKey("test"), func(buckets []segmentationKeyRateBucket) {
			require.Equal(t, uint64(2), buckets[0].size)
			// All other buckets should contain zeros.
			require.Equal(t, uint64(0), buckets[1].size)
			require.Equal(t, uint64(0), buckets[2].size)
			require.Equal(t, uint64(0), buckets[3].size)
			require.Equal(t, uint64(0), buckets[4].size)
		})
	})

	t.Run("buckets do not reset on wrap around", func(t *testing.T) {
		clock := quartz.NewMock(t)
		s := newSegmentationKeyRateStore(5*time.Minute, time.Minute)
		for i := range 5 {
			s.Update("tenant", SegmentationKey("test"), uint64(i), clock.Now())
			clock.Advance(time.Minute)
		}
		// Check the buckets matches expectations.
		s.withBuckets("tenant", SegmentationKey("test"), func(buckets []segmentationKeyRateBucket) {
			require.Equal(t, uint64(0), buckets[0].size)
			require.Equal(t, uint64(1), buckets[1].size)
			require.Equal(t, uint64(2), buckets[2].size)
			require.Equal(t, uint64(3), buckets[3].size)
			require.Equal(t, uint64(4), buckets[4].size)
		})
		// Advance the clock 5 minutes.
		clock.Advance(5 * time.Minute)
		s.Update("tenant", SegmentationKey("test"), 5, clock.Now())
		// Check the buckets matches expectations.
		s.withBuckets("tenant", SegmentationKey("test"), func(buckets []segmentationKeyRateBucket) {
			require.Equal(t, uint64(5), buckets[0].size)
			require.Equal(t, uint64(1), buckets[1].size)
			require.Equal(t, uint64(2), buckets[2].size)
			require.Equal(t, uint64(3), buckets[3].size)
			require.Equal(t, uint64(4), buckets[4].size)
		})
		// Advance the clock another minute.
		clock.Advance(time.Minute)
		s.Update("tenant", SegmentationKey("test"), 6, clock.Now())
		// Check the buckets matches expectations.
		s.withBuckets("tenant", SegmentationKey("test"), func(buckets []segmentationKeyRateBucket) {
			require.Equal(t, uint64(5), buckets[0].size)
			require.Equal(t, uint64(6), buckets[1].size)
			require.Equal(t, uint64(2), buckets[2].size)
			require.Equal(t, uint64(3), buckets[3].size)
			require.Equal(t, uint64(4), buckets[4].size)
		})
	})

	t.Run("buckets outside window are excluded from the average rate", func(t *testing.T) {
		clock := quartz.NewMock(t)
		s := newSegmentationKeyRateStore(5*time.Minute, time.Minute)
		actual := s.Update("tenant", SegmentationKey("test"), 100, clock.Now())
		// We don't have a per second rate as a second hasn't passed.
		require.Equal(t, uint64(0), actual)
		clock.Advance(59 * time.Second)
		// Advance the clock 59 seconds. We expect the average per second rate
		// to be 1 because integer division of 100 by 59 is 1 where the bucket
		// contains 100, and we have 59 seconds within the window.
		actual = s.Update("tenant", SegmentationKey("test"), 0, clock.Now())
		require.Equal(t, uint64(1), actual)
		// Advance the clock so we update the next bucket. This time we expect
		// the per second rate to be 0 because integer division of 101 by 119
		// is 0. The 119 because we have 60 seconds of the first bucket and
		// 59 seconds of the second bucket within the window.
		clock.Advance(time.Minute)
		actual = s.Update("tenant", SegmentationKey("test"), 1, clock.Now())
		require.Equal(t, uint64(0), actual)
		// Add 199 so we have an average per second rate of 2.
		actual = s.Update("tenant", SegmentationKey("test"), 199, clock.Now())
		require.Equal(t, uint64(2), actual)
		// Wrap around to the start of the window to make sure the first bucket
		// is ignored as it is older than 5 minutes.
		clock.Advance(4 * time.Minute)
		actual = s.Update("tenant", SegmentationKey("test"), 0, clock.Now())
		require.Equal(t, uint64(1), actual)
	})
}

func TestSegmentRateStore_EvictExpired(t *testing.T) {
	t.Run("segmentation key is evicted when all buckets outside window", func(t *testing.T) {
		clock := quartz.NewMock(t)
		s := newSegmentationKeyRateStore(5*time.Minute, time.Minute)
		// No segmentation keys should be evicted.
		require.Equal(t, 0, s.EvictExpired(clock.Now()))
		// Update the per second rate for the segmentation key. The segmentation
		// key should not be evicted as the bucket is within the window.
		s.Update("tenant", SegmentationKey("test"), 100, clock.Now())
		require.Equal(t, 0, s.EvictExpired(clock.Now()))
		// Advance the clock 4 minutes and update the per second rate for the
		// same segmentation key. It should not be evicted as we now have
		// two buckets within the window.
		clock.Advance(4 * time.Minute)
		s.Update("tenant", SegmentationKey("test"), 100, clock.Now())
		require.Equal(t, 0, s.EvictExpired(clock.Now()))
		// Advance the clock 1 minute. The segmentation key should not be
		// evicted as both buckets are still within the window.
		clock.Advance(time.Minute)
		require.Equal(t, 0, s.EvictExpired(clock.Now()))
		// Advance the clock another minute. The segmentation key should not
		// be evicted because while one bucket is now outside the window,
		// the other bucket is still within the window.
		clock.Advance(time.Minute)
		require.Equal(t, 0, s.EvictExpired(clock.Now()))
		// Advance the clock another 4 minutes. All buckets should be outside
		// the window and the segmentation key should be evicted.
		clock.Advance(4 * time.Minute)
		require.Equal(t, 1, s.EvictExpired(clock.Now()))
	})

	t.Run("tenant is not evicted unless all of their segmentation keys are evicted", func(t *testing.T) {
		clock := quartz.NewMock(t)
		s := newSegmentationKeyRateStore(5*time.Minute, time.Minute)
		// Add rates for two segmentation keys 4 minutes apart of each other.
		s.Update("tenant", SegmentationKey("test1"), 100, clock.Now())
		clock.Advance(4 * time.Minute)
		s.Update("tenant", SegmentationKey("test2"), 200, clock.Now())
		// Advance the clock past the end of the bucket containing test1.
		// It should be evicted, but not test2.
		clock.Advance(2 * time.Minute)
		require.Equal(t, 1, s.EvictExpired(clock.Now()))
		// Check that test2 is still present.
		s.withBuckets("tenant", SegmentationKey("test2"), func(buckets []segmentationKeyRateBucket) {
			var totalSize uint64
			for _, bucket := range buckets {
				totalSize += bucket.size
			}
			require.Equal(t, uint64(200), totalSize)
		})
		// Advance the clock past the end of the bucket containing test2.
		// Now test2 should be evicted.
		clock.Advance(4 * time.Minute)
		require.Equal(t, 1, s.EvictExpired(clock.Now()))
	})
}
