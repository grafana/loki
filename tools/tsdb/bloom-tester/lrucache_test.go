package main

import (
	"encoding/binary"
	"github.com/stretchr/testify/require"
	"strconv"
	"testing"
)

func BenchmarkLRU1Put(b *testing.B) {
	cache := NewLRUCache(num)
	for i := 0; i < b.N; i++ {
		cache.Put(strconv.Itoa(i))
	}
}

func BenchmarkLRU1Get(b *testing.B) {
	cache := NewLRUCache(num)
	for i := 0; i < num; i++ {
		cache.Put(strconv.Itoa(i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(strconv.Itoa(i))
	}
}

func BenchmarkLRU2Put(b *testing.B) {
	cache := NewLRUCache2(num)
	for i := 0; i < b.N; i++ {
		cache.Put(strconv.Itoa(i))
	}
}

func BenchmarkLRU2Get(b *testing.B) {
	cache := NewLRUCache2(num)
	for i := 0; i < num; i++ {
		cache.Put(strconv.Itoa(i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(strconv.Itoa(i))
	}
}

func BenchmarkLRU4Put(b *testing.B) {
	cache := NewLRUCache4(num)
	for i := 0; i < b.N; i++ {
		cache.Put([]byte(strconv.Itoa(i)))
	}
}

func BenchmarkLRU4Get(b *testing.B) {
	cache := NewLRUCache4(num)
	for i := 0; i < num; i++ {
		cache.Put([]byte(strconv.Itoa(i)))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get([]byte(strconv.Itoa(i)))
	}
}

func TestByteSet(t *testing.T) {
	set := NewByteSet(30)
	set.Put("fooa")
	set.PutBytes([]byte("foob"))
	for _, tc := range []struct {
		desc  string
		input string
		exp   bool
	}{
		{
			desc:  "test string put",
			input: "fooa",
			exp:   true,
		},
		{
			desc:  "test byte put",
			input: "foob",
			exp:   true,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.exp, set.Get(tc.input))
		})
	}
}

func TestByteKeyLRUCache(t *testing.T) {
	set := NewByteKeyLRUCache(30)
	set.Put(NewFourByteKeyFromSlice([]byte("fooa")))
	//set.PutBytes([]byte("foob"))
	for _, tc := range []struct {
		desc  string
		input string
		exp   bool
	}{
		{
			desc:  "test valid",
			input: "fooa",
			exp:   true,
		},
		{
			desc:  "test not valid",
			input: "foob",
			exp:   false,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.exp, set.Get(NewFourByteKeyFromSlice([]byte(tc.input))))
		})
	}
}

func BenchmarkByteKeyLRUCacheSet(b *testing.B) {
	buf := make([]byte, 26)
	cache := NewByteKeyLRUCache(num)
	for i := 0; i < b.N; i++ {
		binary.LittleEndian.PutUint64(buf, uint64(i))

		cache.Put(NewTwentySixByteKeyFromSlice(buf))
	}
}

func BenchmarkByteKeyLRUCacheGet(b *testing.B) {
	buf := make([]byte, 26)

	cache := NewByteKeyLRUCache(num)
	for i := 0; i < b.N; i++ {
		binary.LittleEndian.PutUint64(buf, uint64(i))

		cache.Put(NewTwentySixByteKeyFromSlice(buf))
		//cache.Put(NewTwentySixByteKeyFromSlice([]byte(strconv.Itoa(i))))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		binary.LittleEndian.PutUint64(buf, uint64(i))

		cache.Get(NewTwentySixByteKeyFromSlice(buf))
		//cache.Get(NewTwentySixByteKeyFromSlice([]byte(strconv.Itoa(i))))
	}
}

func BenchmarkByteSetPut(b *testing.B) {
	cache := NewByteSet(num)
	for i := 0; i < b.N; i++ {
		cache.Put(strconv.Itoa(i))
	}
}

func BenchmarkByteSetGet(b *testing.B) {
	cache := NewByteSet(num)
	for i := 0; i < b.N; i++ {
		cache.Put(strconv.Itoa(i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.Get(strconv.Itoa(i))
	}
}
