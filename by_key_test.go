package chunk

import (
	"math/rand"
	"reflect"
	"sort"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
)

func c(id string) Chunk {
	return Chunk{UserID: id}
}

func TestUnique(t *testing.T) {
	for _, tc := range []struct {
		in   ByKey
		want ByKey
	}{
		{nil, ByKey{}},
		{ByKey{c("a"), c("a")}, ByKey{c("a")}},
		{ByKey{c("a"), c("a"), c("b"), c("b"), c("c")}, ByKey{c("a"), c("b"), c("c")}},
		{ByKey{c("a"), c("b"), c("c")}, ByKey{c("a"), c("b"), c("c")}},
	} {
		have := unique(tc.in)
		if !reflect.DeepEqual(tc.want, have) {
			assert.Equal(t, tc.want, have)
		}
	}
}

func TestMerge(t *testing.T) {
	type args struct {
		a ByKey
		b ByKey
	}
	for _, tc := range []struct {
		args args
		want ByKey
	}{
		{args{ByKey{}, ByKey{}}, ByKey{}},
		{args{ByKey{c("a")}, ByKey{}}, ByKey{c("a")}},
		{args{ByKey{}, ByKey{c("b")}}, ByKey{c("b")}},
		{args{ByKey{c("a")}, ByKey{c("b")}}, ByKey{c("a"), c("b")}},
		{
			args{ByKey{c("a"), c("c")}, ByKey{c("a"), c("b"), c("d")}},
			ByKey{c("a"), c("b"), c("c"), c("d")},
		},
	} {
		have := merge(tc.args.a, tc.args.b)
		if !reflect.DeepEqual(tc.want, have) {
			assert.Equal(t, tc.want, have)
		}
	}
}

func TestNWayUnion(t *testing.T) {
	for _, tc := range []struct {
		in   []ByKey
		want ByKey
	}{
		{nil, ByKey{}},
		{[]ByKey{{c("a")}}, ByKey{c("a")}},
		{[]ByKey{{c("a")}, {c("a")}}, ByKey{c("a")}},
		{[]ByKey{{c("a")}, {}}, ByKey{c("a")}},
		{[]ByKey{{}, {c("b")}}, ByKey{c("b")}},
		{[]ByKey{{c("a")}, {c("b")}}, ByKey{c("a"), c("b")}},
		{
			[]ByKey{{c("a"), c("c"), c("e")}, {c("c"), c("d")}, {c("b")}},
			ByKey{c("a"), c("b"), c("c"), c("d"), c("e")},
		},
		{
			[]ByKey{{c("c"), c("d")}, {c("b")}, {c("a"), c("c"), c("e")}},
			ByKey{c("a"), c("b"), c("c"), c("d"), c("e")},
		},
	} {
		have := nWayUnion(tc.in)
		if !reflect.DeepEqual(tc.want, have) {
			assert.Equal(t, tc.want, have)
		}
	}
}

func TestNWayIntersect(t *testing.T) {
	for _, tc := range []struct {
		in   []ByKey
		want ByKey
	}{
		{nil, ByKey{}},
		{[]ByKey{{c("a"), c("b"), c("c")}}, []Chunk{c("a"), c("b"), c("c")}},
		{[]ByKey{{c("a"), c("b"), c("c")}, {c("a"), c("c")}}, ByKey{c("a"), c("c")}},
		{[]ByKey{{c("a"), c("b"), c("c")}, {c("a"), c("c")}, {c("b")}}, ByKey{}},
		{[]ByKey{{c("a"), c("b"), c("c")}, {c("a"), c("c")}, {c("a")}}, ByKey{c("a")}},
	} {
		have := nWayIntersect(tc.in)
		if !reflect.DeepEqual(tc.want, have) {
			assert.Equal(t, tc.want, have)
		}
	}
}

func BenchmarkByKeyLess(b *testing.B) {
	a := ByKey{dummyChunk(), dummyChunk()}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		a.Less(0, 1)
	}
}

func BenchmarkByKeySort100(b *testing.B)   { benchmarkByKeySort(b, 100) }
func BenchmarkByKeySort1000(b *testing.B)  { benchmarkByKeySort(b, 1000) }
func BenchmarkByKeySort10000(b *testing.B) { benchmarkByKeySort(b, 10000) }

func benchmarkByKeySort(b *testing.B, batchSize int) {
	chunks := []Chunk{}
	for i := 0; i < batchSize; i++ {
		chunk := dummyChunk()
		// Tweak the dummy data slightly so the chunks are more likely to be different
		// this makes the checksum wrong but we don't look at it
		chunk.From += model.Time(rand.Intn(batchSize))
		chunks = append(chunks, chunk)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		sort.Sort(ByKey(chunks))
	}
}
