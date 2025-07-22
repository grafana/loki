package log

import (
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
