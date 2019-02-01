package ratelimiter

import (
	"testing"
	"time"
)

func TestPour(t *testing.T) {
	bucket := NewLeakyBucket(60, time.Second)
	bucket.Lastupdate = time.Unix(0, 0)

	bucket.Now = func() time.Time { return time.Unix(1, 0) }

	if bucket.Pour(61) {
		t.Error("Expected false")
	}

	if !bucket.Pour(10) {
		t.Error("Expected true")
	}

	if !bucket.Pour(49) {
		t.Error("Expected true")
	}

	if bucket.Pour(2) {
		t.Error("Expected false")
	}

	bucket.Now = func() time.Time { return time.Unix(61, 0) }
	if !bucket.Pour(60) {
		t.Error("Expected true")
	}

	if bucket.Pour(1) {
		t.Error("Expected false")
	}

	bucket.Now = func() time.Time { return time.Unix(70, 0) }

	if !bucket.Pour(1) {
		t.Error("Expected true")
	}

}

func TestTimeSinceLastUpdate(t *testing.T) {
	bucket := NewLeakyBucket(60, time.Second)
	bucket.Now = func() time.Time { return time.Unix(1, 0) }
	bucket.Pour(1)
	bucket.Now = func() time.Time { return time.Unix(2, 0) }

	sinceLast := bucket.TimeSinceLastUpdate()
	if sinceLast != time.Second*1 {
		t.Errorf("Expected time since last update to be less than 1 second, got %d", sinceLast)
	}
}

func TestTimeToDrain(t *testing.T) {
	bucket := NewLeakyBucket(60, time.Second)
	bucket.Now = func() time.Time { return time.Unix(1, 0) }
	bucket.Pour(10)

	if bucket.TimeToDrain() != time.Second*10 {
		t.Error("Time to drain should be 10 seconds")
	}

	bucket.Now = func() time.Time { return time.Unix(2, 0) }

	if bucket.TimeToDrain() != time.Second*9 {
		t.Error("Time to drain should be 9 seconds")
	}
}
