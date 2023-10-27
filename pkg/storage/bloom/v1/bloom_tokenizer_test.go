package v1

import (
	"fmt"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func BenchmarkMapClear(b *testing.B) {
	bt, _ := NewBloomTokenizer(prometheus.DefaultRegisterer)
	for i := 0; i < b.N; i++ {
		for k := 0; k < CacheSize; k++ {
			bt.cache[fmt.Sprint(k)] = k
		}

		clearCache(bt.cache)
	}
}

func BenchmarkNewMap(b *testing.B) {
	bt, _ := NewBloomTokenizer(prometheus.DefaultRegisterer)
	for i := 0; i < b.N; i++ {
		for k := 0; k < CacheSize; k++ {
			bt.cache[fmt.Sprint(k)] = k
		}

		bt.cache = make(map[string]interface{}, CacheSize)
	}
}
