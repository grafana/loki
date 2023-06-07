package sketch

import (
	"github.com/stretchr/testify/assert"
	"math/rand"
	"strconv"
	"testing"
	"time"
)

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func RandStringRunes(n int) string {
	gen := rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[gen.Intn(len(letterRunes))]
	}
	return string(b)
}

type event struct {
	name  string
	count int
}

func TestTopK(t *testing.T) {
	events := make([]event, 0)
	total := 0
	// lets insert 0-9 some amount of times between 1-5 times
	for i := 0; i < 10; i++ {
		n := rand.Intn(5) + 1
		events = append(events, event{name: strconv.Itoa(i), count: n})
		total = total + n
	}
	// then another set of things more than 5 times
	for i := 10; i < 13; i++ {
		n := rand.Intn(6) + 10
		events = append(events, event{name: strconv.Itoa(i), count: n})
		total = total + n

	}

	topk, err := NewTopk(3, total)
	assert.NoError(t, err, "error creating topk")
	for _, e := range events {
		for i := 0; i < e.count; i++ {
			topk.Observe(e.name)
		}
	}
	assert.True(t, topk.InTopk("10"), "10 was not in topk")
	assert.True(t, topk.InTopk("11"), "11 was not in topk")
	assert.True(t, topk.InTopk("12"), "12 was not in topk")
}

func BenchmarkTopK(b *testing.B) {
	names := make([]string, 100000)
	for i := 0; i < len(names); i++ {
		names[i] = RandStringRunes(100)
	}
	for i := 0; i < b.N; i++ {
		total := (len(names)-3)*10 + (3 * 100)
		topk, err := NewTopk(3, total)

		assert.NoError(b, err)
		for i := 0; i < len(names)-3; i++ {
			for j := 0; j <= 10; j++ {
				topk.Observe(strconv.Itoa(i))
			}
		}
		for i := len(names) - 3; i < len(names); i++ {
			for j := 0; j < 100; j++ {
				topk.Observe(strconv.Itoa(i))
			}
		}
		//b.ReportMetric(float64(size.Of(topk)), "struct_size")
	}
}
