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
