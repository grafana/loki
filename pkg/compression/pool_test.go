package compression

import (
	"bytes"
	"io"
	"os"
	"runtime"
	"runtime/pprof"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPool(t *testing.T) {
	for _, enc := range supportedCodecs {
		t.Run(enc.String(), func(t *testing.T) {
			var wg sync.WaitGroup

			for i := 0; i < 200; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					var (
						buf   = bytes.NewBuffer(nil)
						res   = make([]byte, 1024)
						wpool = GetWriterPool(enc)
						rpool = GetReaderPool(enc)
					)

					w := wpool.GetWriter(buf)
					defer wpool.PutWriter(w)
					_, err := w.Write([]byte("test"))
					require.NoError(t, err)
					require.NoError(t, w.Close())

					require.True(t, buf.Len() != 0, enc)
					r, err := rpool.GetReader(bytes.NewBuffer(buf.Bytes()))
					require.NoError(t, err)
					defer rpool.PutReader(r)
					n, err := r.Read(res)
					if err != nil {
						require.Error(t, err, io.EOF)
					}
					require.Equal(t, 4, n, enc.String())
					require.Equal(t, []byte("test"), res[:n], enc)
				}()
			}

			wg.Wait()

			if !assert.Eventually(t, func() bool {
				runtime.GC()
				return runtime.NumGoroutine() <= 50
			}, 5*time.Second, 10*time.Millisecond) {
				_ = pprof.Lookup("goroutine").WriteTo(os.Stdout, 1)
			}

		})
	}
}
