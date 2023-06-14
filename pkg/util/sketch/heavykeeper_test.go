package sketch

import (
	"fmt"
	"github.com/DmitriyVTitov/size"
	"github.com/migotom/heavykeeper"
	"math/rand"
	"strconv"
	"testing"
)

func BenchmarkOSHK(b *testing.B) {
	//runtime.SetBlockProfileRate(0)
	names := make([]string, 1000)
	max := 1000
	k := 10
	//for i := 0; i < len(names); i++ {
	//	names[i] = RandStringRunes(100)
	//}
	b.ResetTimer()
	//runtime.SetBlockProfileRate(1)
	for i := 0; i < b.N; i++ {
		//total := (len(names)-3)*10 + (3 * 100)
		topk := heavykeeper.New(1, uint32(k), 128, 6, 0.9, 1234)

		//assert.NoError(b, err)
		m := 0
		for i := 0; i < len(names)-k; i++ {
			n := rand.Intn(max) + 1
			if n > m {
				m = n
			}
			for j := 0; j <= n; j++ {
				topk.Add(strconv.Itoa(i))
			}
		}
		for i := len(names) - k; i < len(names); i++ {
			n := rand.Intn(max) + 1 + m

			for j := 0; j < n; j++ {
				topk.Add(strconv.Itoa(i))
			}
		}

		top := topk.List()
		fmt.Println("top: ", top)
		missing := 0
		//fmt.Println("top: ", top)
		var eventName string
	outer:
		for i := len(names) - k; i < len(names); i++ {
			eventName = strconv.Itoa(i)
			for j := 0; j < len(top); j++ {
				if string(top[j].Item) == eventName {
					continue outer
				}
			}
			missing++
		}

		fmt.Println("missing: ", missing)
		b.ReportMetric(float64(size.Of(topk)), "struct_size")
	}
}

//func BenchmarkHK(b *testing.B) {
//	//runtime.SetBlockProfileRate(0)
//	names := make([]string, 10000)
//	max := 1000
//	k := 10
//	//for i := 0; i < len(names); i++ {
//	//	names[i] = RandStringRunes(100)
//	//}
//	b.ResetTimer()
//	//runtime.SetBlockProfileRate(1)
//	for i := 0; i < b.N; i++ {
//		//total := (len(names)-3)*10 + (3 * 100)
//		topk := NewHK(uint32(k), 2048, 6, 0.9, 1234)
//
//		//assert.NoError(b, err)
//		m := 0
//		for i := 0; i < len(names)-k; i++ {
//			n := rand.Intn(max) + 1
//			if n > m {
//				m = n
//			}
//			for j := 0; j <= n; j++ {
//				topk.Add(strconv.Itoa(i))
//			}
//		}
//		for i := len(names) - k; i < len(names); i++ {
//			n := rand.Intn(max) + 1 + m
//
//			for j := 0; j < n; j++ {
//				topk.Add(strconv.Itoa(i))
//			}
//		}
//
//		top := topk.TopK()
//		fmt.Println("top: ", top)
//		missing := 0
//		//fmt.Println("top: ", top)
//		var eventName string
//	outer:
//		for i := len(names) - k; i < len(names); i++ {
//			eventName = strconv.Itoa(i)
//			for j := 0; j < len(top); j++ {
//				if top[j].Event == eventName {
//					continue outer
//				}
//			}
//			missing++
//		}
//
//		fmt.Println("missing: ", missing)
//		b.ReportMetric(float64(size.Of(topk)), "struct_size")
//	}
//}

func BenchmarkMapSize(b *testing.B) {
	for i := 0; i < b.N; i++ {
		items := 100000000
		a := make(map[uint64]uint32, items)
		for j := uint64(0); j < uint64(items); j++ {
			a[j] = 1
		}
		//mapSize := (len(a) * 8 * 8) + (len(a) * 8 * 4)
		//fmt.Println("map size: ", mapSize)
		fmt.Println("size: ", size.Of(a))
	}
}
