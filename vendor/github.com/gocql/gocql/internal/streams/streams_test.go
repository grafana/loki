package streams

import (
	"math"
	"strconv"
	"sync/atomic"
	"testing"
)

func TestUsesAllStreams(t *testing.T) {
	streams := New(1)

	got := make(map[int]struct{})

	for i := 1; i < streams.NumStreams; i++ {
		stream, ok := streams.GetStream()
		if !ok {
			t.Fatalf("unable to get stream %d", i)
		}

		if _, ok = got[stream]; ok {
			t.Fatalf("got an already allocated stream: %d", stream)
		}
		got[stream] = struct{}{}

		if !streams.isSet(stream) {
			bucket := atomic.LoadUint64(&streams.streams[bucketOffset(stream)])
			t.Logf("bucket=%d: %s\n", bucket, strconv.FormatUint(bucket, 2))
			t.Fatalf("stream not set: %d", stream)
		}
	}

	for i := 1; i < streams.NumStreams; i++ {
		if _, ok := got[i]; !ok {
			t.Errorf("did not use stream %d", i)
		}
	}
	if _, ok := got[0]; ok {
		t.Fatal("expected to not use stream 0")
	}

	for i, bucket := range streams.streams {
		if bucket != math.MaxUint64 {
			t.Errorf("did not use all streams in offset=%d bucket=%s", i, bitfmt(bucket))
		}
	}
}

func TestFullStreams(t *testing.T) {
	streams := New(1)
	for i := range streams.streams {
		streams.streams[i] = math.MaxUint64
	}

	stream, ok := streams.GetStream()
	if ok {
		t.Fatalf("should not get stream when all in use: stream=%d", stream)
	}
}

func TestClearStreams(t *testing.T) {
	streams := New(1)
	for i := range streams.streams {
		streams.streams[i] = math.MaxUint64
	}
	streams.inuseStreams = int32(streams.NumStreams)

	for i := 0; i < streams.NumStreams; i++ {
		streams.Clear(i)
	}

	for i, bucket := range streams.streams {
		if bucket != 0 {
			t.Errorf("did not clear streams in offset=%d bucket=%s", i, bitfmt(bucket))
		}
	}
}

func TestDoubleClear(t *testing.T) {
	streams := New(1)
	stream, ok := streams.GetStream()
	if !ok {
		t.Fatal("did not get stream")
	}

	if !streams.Clear(stream) {
		t.Fatalf("stream not indicated as in use: %d", stream)
	}
	if streams.Clear(stream) {
		t.Fatalf("stream not as in use after clear: %d", stream)
	}
}

func BenchmarkConcurrentUse(b *testing.B) {
	streams := New(2)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			stream, ok := streams.GetStream()
			if !ok {
				b.Error("unable to get stream")
				return
			}

			if !streams.Clear(stream) {
				b.Errorf("stream was already cleared: %d", stream)
				return
			}
		}
	})
}

func TestStreamOffset(t *testing.T) {
	tests := [...]struct {
		n   int
		off uint64
	}{
		{0, 63},
		{1, 62},
		{2, 61},
		{3, 60},
		{63, 0},
		{64, 63},

		{128, 63},
	}

	for _, test := range tests {
		if off := streamOffset(test.n); off != test.off {
			t.Errorf("n=%d expected %d got %d", test.n, off, test.off)
		}
	}
}

func TestIsSet(t *testing.T) {
	tests := [...]struct {
		stream int
		bucket uint64
		set    bool
	}{
		{0, 0, false},
		{0, 1 << 63, true},
		{1, 0, false},
		{1, 1 << 62, true},
		{63, 1, true},
		{64, 1 << 63, true},
		{0, 0x8000000000000000, true},
	}

	for i, test := range tests {
		if set := isSet(test.bucket, test.stream); set != test.set {
			t.Errorf("[%d] stream=%d expected %v got %v", i, test.stream, test.set, set)
		}
	}

	for i := 0; i < bucketBits; i++ {
		if !isSet(math.MaxUint64, i) {
			var shift uint64 = math.MaxUint64 >> streamOffset(i)
			t.Errorf("expected isSet for all i=%d got=%d", i, shift)
		}
	}
}

func TestBucketOfset(t *testing.T) {
	tests := [...]struct {
		n      int
		bucket int
	}{
		{0, 0},
		{1, 0},
		{63, 0},
		{64, 1},
	}

	for _, test := range tests {
		if bucket := bucketOffset(test.n); bucket != test.bucket {
			t.Errorf("n=%d expected %v got %v", test.n, test.bucket, bucket)
		}
	}
}

func TestStreamFromBucket(t *testing.T) {
	tests := [...]struct {
		bucket int
		pos    int
		stream int
	}{
		{0, 0, 0},
		{0, 1, 1},
		{0, 2, 2},
		{0, 63, 63},
		{1, 0, 64},
		{1, 1, 65},
	}

	for _, test := range tests {
		if stream := streamFromBucket(test.bucket, test.pos); stream != test.stream {
			t.Errorf("bucket=%d pos=%d expected %v got %v", test.bucket, test.pos, test.stream, stream)
		}
	}
}
