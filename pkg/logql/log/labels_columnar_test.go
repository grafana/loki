package log

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStringColumn_Add(t *testing.T) {
	sc := newStringColumn(10)
	sc.add([]byte("foo"))
	sc.add([]byte("bar"))
	sc.add([]byte("baz"))
	sc.add([]byte("qux"))

	require.Equal(t, 4, sc.len())

	require.Equal(t, []byte("foo"), sc.get(0))
	require.Equal(t, []byte("bar"), sc.get(1))
	require.Equal(t, []byte("baz"), sc.get(2))
	require.Equal(t, []byte("qux"), sc.get(3))
}

func TestStringColumn_Del(t *testing.T) {
	sc := newStringColumn(10)
	sc.add([]byte("foo"))
	sc.add([]byte("bar"))
	sc.add([]byte("baz"))
	sc.add([]byte("qux"))

	all := ""
	for i := 0; i < sc.len(); i++ {
		all += string(sc.get(i))
	}
	require.Equal(t, "foobarbazqux", all)

	require.Equal(t, 4, sc.len())
	sc.del(2)
	require.Equal(t, 3, sc.len())

	all = ""
	for i := 0; i < sc.len(); i++ {
		all += string(sc.get(i))
	}
	require.Equal(t, "foobarqux", all)
}

func TestColumnarLabels_Get(t *testing.T) {
	cl := newColumnarLabels(10)
	cl.add([]byte("foo"), []byte("bar"))
	cl.add([]byte("baz"), []byte("qux"))

	require.Equal(t, 2, cl.len())
	v, ok := cl.get([]byte("foo"))
	require.True(t, ok)
	require.Equal(t, []byte("bar"), v)

	v, ok = cl.get([]byte("baz"))
	require.True(t, ok)
	require.Equal(t, []byte("qux"), v)
}

func TestStringColumn_Index(t *testing.T) {
	sc := newStringColumn(10)
	sc.add([]byte("foo"))
	sc.add([]byte("bar"))
	sc.add([]byte("baz"))
	sc.add([]byte("qux"))

	require.Equal(t, 0, sc.index([]byte("foo")))
	require.Equal(t, 1, sc.index([]byte("bar")))
	require.Equal(t, 2, sc.index([]byte("baz")))
	require.Equal(t, 3, sc.index([]byte("qux")))

	require.Equal(t, -1, sc.index([]byte("ba")))
	require.Equal(t, -1, sc.index([]byte("bazq")))
	require.Equal(t, -1, sc.index([]byte("qu")))
	require.Equal(t, -1, sc.index([]byte("nonexistent")))

	sc.del(2)
	require.Equal(t, 2, sc.index([]byte("qux")))
	require.Equal(t, -1, sc.index([]byte("baz")))
}

func TestColumnarLabels_GetAt(t *testing.T) {
	cl := newColumnarLabels(10)
	cl.add([]byte("foo"), []byte("bar"))
	cl.add([]byte("baz"), []byte("qux"))

	require.Equal(t, 2, cl.len())
	n, v := cl.getAt(0)
	require.Equal(t, []byte("foo"), n)
	require.Equal(t, []byte("bar"), v)

	n, v = cl.getAt(1)
	require.Equal(t, []byte("baz"), n)
	require.Equal(t, []byte("qux"), v)
}

func TestColumnarLabels_Del(t *testing.T) {
	cl := newColumnarLabels(10)
	cl.add([]byte("foo"), []byte("bar"))
	cl.add([]byte("baz"), []byte("qux"))

	require.Equal(t, 2, cl.len())
	cl.del([]byte("foo"))
	require.Equal(t, 1, cl.len())

	_, ok := cl.get([]byte("foo"))
	require.False(t, ok)

	n, v := cl.getAt(0)
	require.Equal(t, []byte("baz"), n)
	require.Equal(t, []byte("qux"), v)
}

func TestColumnarLabels_Override(t *testing.T) {
	cl := newColumnarLabels(10)
	cl.add([]byte("foo"), []byte("bar"))
	cl.add([]byte("baz"), []byte("qux"))

	require.Equal(t, 2, cl.len())
	ok := cl.override([]byte("foo"), []byte("new"))
	require.True(t, ok)
	require.Equal(t, 2, cl.len())

	v, ok := cl.get([]byte("foo"))
	require.True(t, ok)
	require.Equal(t, []byte("new"), v)

	v, ok = cl.get([]byte("baz"))
	require.True(t, ok)
	require.Equal(t, []byte("qux"), v)

	ok = cl.override([]byte("nonexistent"), []byte("new"))
	require.False(t, ok)
	require.Equal(t, 2, cl.len())

	_, ok = cl.get([]byte("nonexistent"))
	require.False(t, ok)
}

func BenchmarkColumnarLabels_Get(b *testing.B) {
	for _, l := range []int{10, 30, 100, 300, 1000} {
		cl := newColumnarLabels(l)
		for i := 0; i < l; i++ {
			cl.add([]byte(fmt.Sprintf("foo%d", i)), []byte(fmt.Sprintf("bar%d", i)))
		}

		b.ResetTimer()
		b.Run(fmt.Sprintf("get-middle-%d", l), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				cl.get([]byte(fmt.Sprintf("foo%d", l/2)))
			}
		})
		b.Run(fmt.Sprintf("get-first-%d", l), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				cl.get([]byte("foo0"))
			}
		})
		b.Run(fmt.Sprintf("get-last-%d", l), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				cl.get([]byte(fmt.Sprintf("foo%d", l-1)))
			}
		})
		b.Run(fmt.Sprintf("get-nonexistent-%d", l), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				cl.get([]byte("nonexistent"))
			}
		})
	}
}
