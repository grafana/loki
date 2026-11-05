package ingester

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/grafana/dskit/httpgrpc"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/ingester/wal"
	"github.com/grafana/loki/v3/pkg/logproto"
	loki_runtime "github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/validation"
)

func newPushErrorInstance(t *testing.T) *instance {
	t.Helper()
	cfg := defaultLimitsTestConfig()
	cfg.PerStreamRateLimit = 100
	cfg.PerStreamRateLimitBurst = 100
	cfg.ShardStreams.Enabled = true
	limits, err := validation.NewOverrides(cfg, nil)
	require.NoError(t, err)
	limiter := NewLimiter(limits, NilMetrics, newIngesterRingLimiterStrategy(&ringCountMock{count: 1}, 1), &TenantBasedStrategy{limits: limits})
	inst, err := newInstance(defaultConfig(), defaultPeriodConfigs, "test", limiter, loki_runtime.DefaultTenantConfigs(), noopWAL{}, NilMetrics, &OnceSwitch{}, nil, nil, nil, NewStreamRateCalculator(), nil, nil, retention.NewTenantsRetention(limits))
	require.NoError(t, err)
	return inst
}

func TestInstancePushPreservesErrors(t *testing.T) {
	now := time.Now()
	valid := logproto.Stream{Labels: `{kind="valid"}`, Entries: []logproto.Entry{{Timestamp: now, Line: "valid"}}}
	rejected := logproto.Stream{Labels: `{kind="limited"}`, Entries: []logproto.Entry{{Timestamp: now, Line: strings.Repeat("x", 200)}}}
	malformed := logproto.Stream{Labels: `{`, Entries: valid.Entries}
	for _, tc := range []struct {
		name    string
		streams []logproto.Stream
		code    int
	}{
		{"empty", nil, 0}, {"success", []logproto.Stream{valid}, 0},
		{"rejected then success", []logproto.Stream{rejected, valid}, 429},
		{"success then rejected", []logproto.Stream{valid, rejected}, 429},
		{"all rejected", []logproto.Stream{rejected}, 429},
		{"creation error then success", []logproto.Stream{malformed, valid}, 400},
		{"success then creation error", []logproto.Stream{valid, malformed}, 400},
		{"permanent then limited", []logproto.Stream{malformed, rejected}, 429},
		{"limited then permanent", []logproto.Stream{rejected, malformed}, 429},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inst := newPushErrorInstance(t)
			err := inst.Push(context.Background(), &logproto.PushRequest{Streams: tc.streams})
			if tc.code == 0 {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			response, ok := httpgrpc.HTTPResponseFromError(err)
			require.True(t, ok)
			require.Equal(t, int32(tc.code), response.Code)
		})
	}
}

func TestErrorForFailedEntriesPreservesRetryability(t *testing.T) {
	internal := errors.New("storage failed")
	for _, tc := range []struct {
		name string
		errs []error
		code int
	}{
		{"rate then old", []error{&validation.ErrStreamRateLimit{}, chunkenc.ErrOutOfOrder}, 429},
		{"old then rate", []error{chunkenc.ErrOutOfOrder, &validation.ErrStreamRateLimit{}}, 429},
		{"internal then rate", []error{internal, &validation.ErrStreamRateLimit{}}, 500},
		{"rate then internal", []error{&validation.ErrStreamRateLimit{}, internal}, 500},
		{"only old", []error{chunkenc.ErrOutOfOrder}, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &stream{cfg: &Config{MaxReturnedErrors: 1}, tenant: "test", labelsString: `{app="test"}`}
			failed := make([]entryWithError, len(tc.errs))
			for i, err := range tc.errs {
				failed[i] = entryWithError{entry: &logproto.Entry{Timestamp: time.Unix(int64(i), 0)}, e: err}
			}
			err := errorForFailedEntries(s, failed, len(failed))
			require.Error(t, err)
			if tc.code == 500 {
				require.ErrorIs(t, err, internal)
				return
			}
			response, ok := httpgrpc.HTTPResponseFromError(err)
			require.True(t, ok)
			require.Equal(t, int32(tc.code), response.Code)
			require.Equal(t, 1, strings.Count(string(response.Body), "entry with timestamp"))
		})
	}
}

type pushErrorWAL struct {
	noopWAL
	err error
}

func (w pushErrorWAL) Log(*wal.Record) error { return w.err }

func TestInstancePushWALErrorPrecedence(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(map[bool]string{false: "failure", true: "disk full"}[full], func(t *testing.T) {
			inst := newPushErrorInstance(t)
			walErr := errors.New("WAL write failed")
			if full {
				walErr = &os.PathError{Op: "write", Path: "wal", Err: syscall.ENOSPC}
			}
			inst.wal = pushErrorWAL{err: walErr}
			now := time.Now()
			err := inst.Push(context.Background(), &logproto.PushRequest{Streams: []logproto.Stream{
				{Labels: `{kind="limited"}`, Entries: []logproto.Entry{{Timestamp: now, Line: strings.Repeat("x", 200)}}},
				{Labels: `{kind="valid"}`, Entries: []logproto.Entry{{Timestamp: now, Line: "ok"}}},
			}})
			if !full {
				require.ErrorIs(t, err, walErr)
				return
			}
			response, ok := httpgrpc.HTTPResponseFromError(err)
			require.True(t, ok)
			require.Equal(t, int32(http.StatusTooManyRequests), response.Code)
		})
	}
}

func TestSelectPushError(t *testing.T) {
	permanent := httpgrpc.Errorf(400, "permanent")
	limited := httpgrpc.Errorf(429, "limited")
	internal := errors.New("internal")
	server := httpgrpc.Errorf(503, "unavailable")
	for _, stronger := range []error{internal, server, httpgrpc.Errorf(200, "unexpected status")} {
		for _, weaker := range []error{nil, permanent, limited} {
			require.Same(t, stronger, selectPushError(stronger, weaker))
			require.Same(t, stronger, selectPushError(weaker, stronger))
		}
	}
	require.Same(t, limited, selectPushError(permanent, limited))
	require.Same(t, limited, selectPushError(limited, permanent))
	require.Same(t, permanent, selectPushError(permanent, nil))
	require.Nil(t, selectPushError(nil, nil))
	last := httpgrpc.Errorf(400, "last diagnostic")
	require.Same(t, last, selectPushError(permanent, last))
}

func TestInstancePushRetryPreservesPartialWrites(t *testing.T) {
	inst := newPushErrorInstance(t)
	now := time.Now()
	req := &logproto.PushRequest{Streams: []logproto.Stream{
		{Labels: `{kind="limited"}`, Entries: []logproto.Entry{{Timestamp: now, Line: strings.Repeat("x", 200)}}},
		{Labels: `{kind="valid"}`, Entries: []logproto.Entry{{Timestamp: now, Line: "ok"}}},
	}}
	err := inst.Push(context.Background(), req)
	response, ok := httpgrpc.HTTPResponseFromError(err)
	require.True(t, ok)
	require.Equal(t, int32(http.StatusTooManyRequests), response.Code)
	limited, ok := inst.streams.Load(req.Streams[0].Labels)
	require.True(t, ok)
	valid, ok := inst.streams.Load(req.Streams[1].Labels)
	require.True(t, ok)
	require.EqualValues(t, 0, limited.entryCt)
	require.EqualValues(t, 1, valid.entryCt)

	// Once the limiting condition clears, retry the identical batch. The
	// previously accepted entry must not be appended a second time.
	inst.limiter.rateLimitStrategy.SetDisabled(true)
	limited.limiter.recheckAt = time.Time{}
	valid.limiter.recheckAt = time.Time{}
	require.NoError(t, inst.Push(context.Background(), req))
	require.EqualValues(t, 1, limited.entryCt)
	require.EqualValues(t, 1, valid.entryCt)
}
