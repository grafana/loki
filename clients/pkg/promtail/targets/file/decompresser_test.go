package file

import (
	"sync"
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/atomic"
)

type noopClient struct {
	noopChan chan api.Entry
	wg       sync.WaitGroup //nolint:copylocks
	once     sync.Once
}

func (n noopClient) Chan() chan<- api.Entry { //nolint:copylocks
	return n.noopChan
}

func (n noopClient) Stop() { //nolint:copylocks
	n.once.Do(func() { close(n.noopChan) })
}

func newNoopClient() *noopClient {
	c := &noopClient{noopChan: make(chan api.Entry)}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for range c.noopChan {
			// noop
		}
	}()
	return c
}

func BenchmarkReadlines(b *testing.B) {
	entryHandler := newNoopClient()

	scenarios := []struct {
		name string
		file string
	}{
		{
			name: "2000 lines of log .tar.gz compressed",
			file: "test_fixtures/short-access.tar.gz",
		},
		{
			name: "100000 lines of log .tar.gz compressed",
			file: "test_fixtures/long-access.tar.gz",
		},
		{
			name: "100000 lines of log .zip compressed",
			file: "test_fixtures/long-access.zip",
		},
	}

	for _, tc := range scenarios {
		b.Run(tc.name, func(b *testing.B) {
			decBase := &decompressor{
				logger:  log.NewNopLogger(),
				running: atomic.NewBool(false),
				handler: entryHandler,
				path:    tc.file,
			}

			for i := 0; i < b.N; i++ {
				newDec := decBase
				newDec.metrics = NewMetrics(prometheus.NewRegistry())
				newDec.done = make(chan struct{})
				newDec.readLines()
				<-newDec.done
			}
		})
	}
}
