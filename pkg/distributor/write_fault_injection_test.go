package distributor

import (
	"context"
	"flag"
	"net/http"
	"testing"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/validation"
)

func TestWriteFaultInjectionFlags(t *testing.T) {
	var cfg WriteFaultInjection
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg.RegisterFlags(fs)
	require.NoError(t, fs.Parse(nil))
	assert.False(t, cfg.InternalErrorEnabled)
	assert.Equal(t, 2, cfg.InternalErrorPeriod)
	assert.False(t, cfg.LatencyEnabled)
	assert.Equal(t, 2*time.Second, cfg.Latency)
}

func TestWriteFaultInjectorDisabled(t *testing.T) {
	inj := newWriteFaultInjector(WriteFaultInjection{})
	for range 10 {
		require.NoError(t, inj.maybe(context.Background()))
	}
}

func TestWriteFaultInjectorEveryNthError(t *testing.T) {
	inj := newWriteFaultInjector(WriteFaultInjection{
		InternalErrorEnabled: true,
		InternalErrorPeriod:  2,
	})
	for i := 1; i <= 4; i++ {
		err := inj.maybe(context.Background())
		if i%2 == 0 {
			resp, ok := httpgrpc.HTTPResponseFromError(err)
			require.True(t, ok)
			assert.Equal(t, int32(http.StatusInternalServerError), resp.Code)
			continue
		}
		require.NoError(t, err)
	}
}

func TestWriteFaultInjectorLatency(t *testing.T) {
	inj := newWriteFaultInjector(WriteFaultInjection{
		LatencyEnabled: true,
		Latency:        20 * time.Millisecond,
	})
	start := time.Now()
	require.NoError(t, inj.maybe(context.Background()))
	assert.GreaterOrEqual(t, time.Since(start), 20*time.Millisecond)
}

func TestDistributorPushInternalErrorInjection(t *testing.T) {
	limits := &validation.Limits{}
	flagext.DefaultValues(limits)
	distributors, _ := prepare(t, 1, 3, limits, nil)
	d := distributors[0]
	d.writeFault = newWriteFaultInjector(WriteFaultInjection{
		InternalErrorEnabled: true,
		InternalErrorPeriod:  2,
	})

	_, err := d.Push(ctx, makeWriteRequest(1, 10))
	require.NoError(t, err)
	_, err = d.Push(ctx, makeWriteRequest(1, 10))
	resp, ok := httpgrpc.HTTPResponseFromError(err)
	require.True(t, ok)
	assert.Equal(t, int32(http.StatusInternalServerError), resp.Code)
}
