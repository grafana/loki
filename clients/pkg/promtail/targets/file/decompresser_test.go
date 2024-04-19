package file

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
)

type noopClient struct {
	noopChan chan api.Entry
	wg       sync.WaitGroup
	once     sync.Once
}

func (n *noopClient) Chan() chan<- api.Entry {
	return n.noopChan
}

func (n *noopClient) Stop() {
	n.once.Do(func() { close(n.noopChan) })
}

func newNoopClient() *noopClient {
	c := &noopClient{noopChan: make(chan api.Entry)}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for range c.noopChan { //nolint:revive
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
			name: "100000 lines of log .gz compressed",
			file: "test_fixtures/long-access.gz",
		},
	}

	for _, tc := range scenarios {
		b.Run(tc.name, func(b *testing.B) {
			decBase := &decompressor{
				logger:  log.NewNopLogger(),
				running: atomic.NewBool(false),
				handler: entryHandler,
				path:    tc.file,
				cfg:     &scrapeconfig.DecompressionConfig{InitialDelay: 0, Format: "gz"},
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

func TestGigantiqueGunzipFile(t *testing.T) {
	file := "test_fixtures/long-access.gz"
	handler := fake.New(func() {})

	d := &decompressor{
		logger:  log.NewNopLogger(),
		running: atomic.NewBool(false),
		handler: handler,
		path:    file,
		done:    make(chan struct{}),
		metrics: NewMetrics(prometheus.NewRegistry()),
		cfg:     &scrapeconfig.DecompressionConfig{InitialDelay: 0, Format: "gz"},
	}

	d.readLines()

	<-d.done
	time.Sleep(time.Millisecond * 200)

	entries := handler.Received()
	require.Equal(t, 100000, len(entries))
}

// TestOnelineFiles test the supported formats for log lines that only contain 1 line.
//
// Based on our experience, this is the scenario with the most edge cases.
func TestOnelineFiles(t *testing.T) {
	fileContent, err := os.ReadFile("test_fixtures/onelinelog.log")
	require.NoError(t, err)
	t.Run("gunzip file", func(t *testing.T) {
		file := "test_fixtures/onelinelog.log.gz"
		handler := fake.New(func() {})

		d := &decompressor{
			logger:  log.NewNopLogger(),
			running: atomic.NewBool(false),
			handler: handler,
			path:    file,
			done:    make(chan struct{}),
			metrics: NewMetrics(prometheus.NewRegistry()),
			cfg:     &scrapeconfig.DecompressionConfig{InitialDelay: 0, Format: "gz"},
		}

		d.readLines()

		<-d.done
		time.Sleep(time.Millisecond * 200)

		entries := handler.Received()
		require.Equal(t, 1, len(entries))
		require.Equal(t, string(fileContent), entries[0].Line)
	})

	t.Run("bzip2 file", func(t *testing.T) {
		file := "test_fixtures/onelinelog.log.bz2"
		handler := fake.New(func() {})

		d := &decompressor{
			logger:  log.NewNopLogger(),
			running: atomic.NewBool(false),
			handler: handler,
			path:    file,
			done:    make(chan struct{}),
			metrics: NewMetrics(prometheus.NewRegistry()),
			cfg:     &scrapeconfig.DecompressionConfig{InitialDelay: 0, Format: "bz2"},
		}

		d.readLines()

		<-d.done
		time.Sleep(time.Millisecond * 200)

		entries := handler.Received()
		require.Equal(t, 1, len(entries))
		require.Equal(t, string(fileContent), entries[0].Line)
	})

	t.Run("tar.gz file", func(t *testing.T) {
		file := "test_fixtures/onelinelog.tar.gz"
		handler := fake.New(func() {})

		d := &decompressor{
			logger:  log.NewNopLogger(),
			running: atomic.NewBool(false),
			handler: handler,
			path:    file,
			done:    make(chan struct{}),
			metrics: NewMetrics(prometheus.NewRegistry()),
			cfg:     &scrapeconfig.DecompressionConfig{InitialDelay: 0, Format: "gz"},
		}

		d.readLines()

		<-d.done
		time.Sleep(time.Millisecond * 200)

		entries := handler.Received()
		require.Equal(t, 1, len(entries))
		firstEntry := entries[0]
		require.Contains(t, firstEntry.Line, "onelinelog.log") // contains .tar.gz headers
		require.Contains(t, firstEntry.Line, `5.202.214.160 - - [26/Jan/2019:19:45:25 +0330] "GET / HTTP/1.1" 200 30975 "https://www.zanbil.ir/" "Mozilla/5.0 (Windows NT 6.2; WOW64; rv:21.0) Gecko/20100101 Firefox/21.0" "-"`)
	})
}
