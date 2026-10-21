package builder

import (
	"bytes"
	"context"
	"testing"
	"testing/synctest"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
)

func TestBufferRecords(t *testing.T) {
	newRecord := func(offset int64, size int) *kgo.Record {
		return &kgo.Record{Offset: offset, Value: bytes.Repeat([]byte{'x'}, size)}
	}
	start := func(ctx context.Context, maxBytes int) (chan *kgo.Record, chan *kgo.Record, prometheus.Gauge, chan struct{}) {
		in, out := make(chan *kgo.Record), make(chan *kgo.Record)
		buffered := prometheus.NewGauge(prometheus.GaugeOpts{Name: "test"})
		done := make(chan struct{})
		go func() {
			defer close(done)
			bufferRecords(ctx, in, out, maxBytes, buffered)
		}()
		return in, out, buffered, done
	}

	t.Run("delivers records in the order it reads them", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			in, out, _, done := start(t.Context(), 100)
			for i := range 5 {
				in <- newRecord(int64(i), 4)
			}
			for i := range 5 {
				require.Equal(t, int64(i), (<-out).Offset)
			}
			close(in)
			<-done
		})
	})

	t.Run("stops reading at the byte limit and reads again when the reader takes a record", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			in, out, buffered, done := start(t.Context(), 10)
			in <- newRecord(0, 4)
			in <- newRecord(1, 4)
			in <- newRecord(2, 4)
			synctest.Wait()
			require.Equal(t, float64(12), testutil.ToFloat64(buffered))

			sent := make(chan struct{})
			go func() {
				in <- newRecord(3, 4)
				close(sent)
			}()
			synctest.Wait()
			select {
			case <-sent:
				t.Fatal("bufferRecords read a record while it held more than the byte limit")
			default:
			}

			require.Equal(t, int64(0), (<-out).Offset)
			<-sent
			synctest.Wait()
			require.Equal(t, float64(12), testutil.ToFloat64(buffered))

			// At the byte limit, bufferRecords sees in closed only after the
			// reader takes a record.
			close(in)
			require.Equal(t, int64(1), (<-out).Offset)
			for range out {
			}
			<-done
		})
	})

	t.Run("passes a record larger than the byte limit when it holds nothing else", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			in, out, _, done := start(t.Context(), 10)
			in <- newRecord(0, 50)
			require.Len(t, (<-out).Value, 50)
			close(in)
			<-done
		})
	})

	t.Run("closes out and drops the held records when in is closed", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			in, out, buffered, done := start(t.Context(), 100)
			in <- newRecord(0, 4)
			in <- newRecord(1, 4)
			close(in)
			<-done

			_, ok := <-out
			require.False(t, ok)
			require.Zero(t, testutil.ToFloat64(buffered))
		})
	})

	t.Run("closes out when the context is canceled", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			in, out, _, done := start(ctx, 100)
			in <- newRecord(0, 4)
			cancel()
			<-done

			_, ok := <-out
			require.False(t, ok)
		})
	})
}
