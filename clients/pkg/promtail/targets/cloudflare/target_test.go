package cloudflare

import (
	"context"
	"errors"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/v3/clients/pkg/promtail/positions"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
)

func Test_CloudflareTarget(t *testing.T) {
	var (
		w      = log.NewSyncWriter(os.Stderr)
		logger = log.NewLogfmtLogger(w)
		cfg    = &scrapeconfig.CloudflareConfig{
			APIToken:  "foo",
			ZoneID:    "bar",
			Labels:    model.LabelSet{"job": "cloudflare"},
			PullRange: model.Duration(time.Minute),
		}
		end      = time.Unix(0, time.Hour.Nanoseconds())
		start    = time.Unix(0, time.Hour.Nanoseconds()-int64(cfg.PullRange))
		client   = fake.New(func() {})
		cfClient = newFakeCloudflareClient()
	)
	ps, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Second,
		PositionsFile: t.TempDir() + "/positions.yml",
	})
	// set our end time to be the last time we have a position
	ps.Put(positions.CursorKey(cfg.ZoneID), end.UnixNano())
	require.NoError(t, err)

	// setup response for the first pull batch of 1 minutes.
	cfClient.On("LogpullReceived", mock.Anything, start, start.Add(time.Duration(cfg.PullRange/3))).Return(&fakeLogIterator{
		logs: []string{
			`{"EdgeStartTimestamp":1, "EdgeRequestHost":"foo.com"}`,
		},
	}, nil)
	cfClient.On("LogpullReceived", mock.Anything, start.Add(time.Duration(cfg.PullRange/3)), start.Add(time.Duration(2*cfg.PullRange/3))).Return(&fakeLogIterator{
		logs: []string{
			`{"EdgeStartTimestamp":2, "EdgeRequestHost":"bar.com"}`,
		},
	}, nil)
	cfClient.On("LogpullReceived", mock.Anything, start.Add(time.Duration(2*cfg.PullRange/3)), end).Return(&fakeLogIterator{
		logs: []string{
			`{"EdgeStartTimestamp":3, "EdgeRequestHost":"buzz.com"}`,
			`{"EdgeRequestHost":"fuzz.com"}`,
		},
	}, nil)
	// setup empty response for the rest.
	cfClient.On("LogpullReceived", mock.Anything, mock.Anything, mock.Anything).Return(&fakeLogIterator{
		logs: []string{},
	}, nil)
	// replace the client.
	getClient = func(_, _ string, _ []string) (Client, error) {
		return cfClient, nil
	}

	ta, err := NewTarget(NewMetrics(prometheus.NewRegistry()), logger, client, ps, cfg)
	require.NoError(t, err)
	require.True(t, ta.Ready())

	require.Eventually(t, func() bool {
		return len(client.Received()) == 4
	}, 5*time.Second, 100*time.Millisecond)

	received := client.Received()
	sort.Slice(received, func(i, j int) bool {
		return received[i].Timestamp.After(received[j].Timestamp)
	})
	for _, e := range received {
		require.Equal(t, model.LabelValue("cloudflare"), e.Labels["job"])
	}
	require.WithinDuration(t, time.Now(), received[0].Timestamp, time.Minute) // no timestamp default to now.
	require.Equal(t, `{"EdgeRequestHost":"fuzz.com"}`, received[0].Line)

	require.Equal(t, `{"EdgeStartTimestamp":3, "EdgeRequestHost":"buzz.com"}`, received[1].Line)
	require.Equal(t, time.Unix(0, 3), received[1].Timestamp)
	require.Equal(t, `{"EdgeStartTimestamp":2, "EdgeRequestHost":"bar.com"}`, received[2].Line)
	require.Equal(t, time.Unix(0, 2), received[2].Timestamp)
	require.Equal(t, `{"EdgeStartTimestamp":1, "EdgeRequestHost":"foo.com"}`, received[3].Line)
	require.Equal(t, time.Unix(0, 1), received[3].Timestamp)
	cfClient.AssertExpectations(t)
	ta.Stop()
	ps.Stop()
	// Make sure we save the last position.
	newPos, _ := ps.Get(positions.CursorKey(cfg.ZoneID))
	require.Greater(t, newPos, end.UnixNano())
}

func Test_RetryErrorLogpullReceived(t *testing.T) {
	var (
		w        = log.NewSyncWriter(os.Stderr)
		logger   = log.NewLogfmtLogger(w)
		end      = time.Unix(0, time.Hour.Nanoseconds())
		start    = time.Unix(0, end.Add(-30*time.Minute).UnixNano())
		client   = fake.New(func() {})
		cfClient = newFakeCloudflareClient()
	)
	cfClient.On("LogpullReceived", mock.Anything, start, end).Return(&fakeLogIterator{
		err: ErrorLogpullReceived,
	}, nil).Times(2) // just retry once
	// replace the client
	getClient = func(_, _ string, _ []string) (Client, error) {
		return cfClient, nil
	}
	defaultBackoff.MinBackoff = 0
	defaultBackoff.MaxBackoff = 5
	ta := &Target{
		logger:  logger,
		handler: client,
		client:  cfClient,
		config: &scrapeconfig.CloudflareConfig{
			Labels: make(model.LabelSet),
		},
		metrics: NewMetrics(nil),
	}

	require.NoError(t, ta.pull(context.Background(), start, end))
}

func Test_RetryErrorIterating(t *testing.T) {
	var (
		w        = log.NewSyncWriter(os.Stderr)
		logger   = log.NewLogfmtLogger(w)
		end      = time.Unix(0, time.Hour.Nanoseconds())
		start    = time.Unix(0, end.Add(-30*time.Minute).UnixNano())
		client   = fake.New(func() {})
		cfClient = newFakeCloudflareClient()
	)
	cfClient.On("LogpullReceived", mock.Anything, start, end).Return(&fakeLogIterator{
		logs: []string{
			`{"EdgeStartTimestamp":1, "EdgeRequestHost":"foo.com"}`,
			`error`,
		},
	}, nil).Once()
	// setup response for the first pull batch of 1 minutes.
	cfClient.On("LogpullReceived", mock.Anything, start, end).Return(&fakeLogIterator{
		logs: []string{
			`{"EdgeStartTimestamp":1, "EdgeRequestHost":"foo.com"}`,
			`{"EdgeStartTimestamp":2, "EdgeRequestHost":"foo.com"}`,
			`{"EdgeStartTimestamp":3, "EdgeRequestHost":"foo.com"}`,
		},
	}, nil).Once()
	cfClient.On("LogpullReceived", mock.Anything, start, end).Return(&fakeLogIterator{
		err: ErrorLogpullReceived,
	}, nil).Once()
	// replace the client.
	getClient = func(_, _ string, _ []string) (Client, error) {
		return cfClient, nil
	}
	// retries as fast as possible.
	defaultBackoff.MinBackoff = 0
	defaultBackoff.MaxBackoff = 0
	ta := &Target{
		logger:  logger,
		handler: client,
		client:  cfClient,
		config: &scrapeconfig.CloudflareConfig{
			Labels: make(model.LabelSet),
		},
		metrics: NewMetrics(prometheus.DefaultRegisterer),
	}

	require.NoError(t, ta.pull(context.Background(), start, end))
	require.Eventually(t, func() bool {
		return len(client.Received()) == 4
	}, 5*time.Second, 100*time.Millisecond)
}

func Test_CloudflareTargetError(t *testing.T) {
	var (
		w      = log.NewSyncWriter(os.Stderr)
		logger = log.NewLogfmtLogger(w)
		cfg    = &scrapeconfig.CloudflareConfig{
			APIToken:  "foo",
			ZoneID:    "bar",
			Labels:    model.LabelSet{"job": "cloudflare"},
			PullRange: model.Duration(time.Minute),
		}
		end      = time.Unix(0, time.Hour.Nanoseconds())
		client   = fake.New(func() {})
		cfClient = newFakeCloudflareClient()
	)
	ps, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Second,
		PositionsFile: t.TempDir() + "/positions.yml",
	})
	// retries as fast as possible.
	defaultBackoff.MinBackoff = 0
	defaultBackoff.MaxBackoff = 0

	// set our end time to be the last time we have a position
	ps.Put(positions.CursorKey(cfg.ZoneID), end.UnixNano())
	require.NoError(t, err)

	// setup errors for all retries
	cfClient.On("LogpullReceived", mock.Anything, mock.Anything, mock.Anything).Return(nil, errors.New("no logs"))
	// replace the client.
	getClient = func(_, _ string, _ []string) (Client, error) {
		return cfClient, nil
	}

	ta, err := NewTarget(NewMetrics(prometheus.NewRegistry()), logger, client, ps, cfg)
	require.NoError(t, err)
	require.True(t, ta.Ready())

	// wait for the target to be stopped.
	require.Eventually(t, func() bool {
		return !ta.Ready()
	}, 5*time.Second, 100*time.Millisecond)

	require.Len(t, client.Received(), 0)
	require.GreaterOrEqual(t, cfClient.CallCount(), 5)
	require.NotEmpty(t, ta.Details().(map[string]string)["error"])
	ta.Stop()
	ps.Stop()

	// Make sure we save the last position.
	newEnd, _ := ps.Get(positions.CursorKey(cfg.ZoneID))
	require.Equal(t, newEnd, end.UnixNano())
}

func Test_CloudflareTargetError168h(t *testing.T) {
	var (
		w      = log.NewSyncWriter(os.Stderr)
		logger = log.NewLogfmtLogger(w)
		cfg    = &scrapeconfig.CloudflareConfig{
			APIToken:  "foo",
			ZoneID:    "bar",
			Labels:    model.LabelSet{"job": "cloudflare"},
			PullRange: model.Duration(time.Minute),
		}
		end      = time.Unix(0, time.Hour.Nanoseconds())
		client   = fake.New(func() {})
		cfClient = newFakeCloudflareClient()
	)
	ps, err := positions.New(logger, positions.Config{
		SyncPeriod:    10 * time.Second,
		PositionsFile: t.TempDir() + "/positions.yml",
	})
	// retries as fast as possible.
	defaultBackoff.MinBackoff = 0
	defaultBackoff.MaxBackoff = 0

	// set our end time to be the last time we have a position
	ps.Put(positions.CursorKey(cfg.ZoneID), end.UnixNano())
	require.NoError(t, err)

	// setup errors for all retries
	cfClient.On("LogpullReceived", mock.Anything, mock.Anything, mock.Anything).Return(nil, errors.New("HTTP status 400: bad query: error parsing time: invalid time range: too early: logs older than 168h0m0s are not available"))
	// replace the client.
	getClient = func(_, _ string, _ []string) (Client, error) {
		return cfClient, nil
	}

	ta, err := NewTarget(NewMetrics(prometheus.NewRegistry()), logger, client, ps, cfg)
	require.NoError(t, err)
	require.True(t, ta.Ready())

	// wait for the target to be stopped.
	require.Eventually(t, func() bool {
		return cfClient.CallCount() >= 5
	}, 5*time.Second, 100*time.Millisecond)

	require.Len(t, client.Received(), 0)
	require.GreaterOrEqual(t, cfClient.CallCount(), 5)
	ta.Stop()
	ps.Stop()

	// Make sure we move on from the save the last position.
	newEnd, _ := ps.Get(positions.CursorKey(cfg.ZoneID))
	require.Greater(t, newEnd, end.UnixNano())
}

func Test_validateConfig(t *testing.T) {
	tests := []struct {
		in      *scrapeconfig.CloudflareConfig
		out     *scrapeconfig.CloudflareConfig
		wantErr bool
	}{
		{
			&scrapeconfig.CloudflareConfig{
				APIToken: "foo",
				ZoneID:   "bar",
			},
			&scrapeconfig.CloudflareConfig{
				APIToken:   "foo",
				ZoneID:     "bar",
				Workers:    3,
				PullRange:  model.Duration(time.Minute),
				FieldsType: string(FieldsTypeDefault),
			},
			false,
		},
		{
			&scrapeconfig.CloudflareConfig{
				APIToken: "foo",
			},
			nil,
			true,
		},
		{
			&scrapeconfig.CloudflareConfig{
				ZoneID: "foo",
			},
			nil,
			true,
		},
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			err := validateConfig(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.Equal(t, tt.out, tt.in)
		})
	}
}

func Test_splitRequests(t *testing.T) {
	tests := []struct {
		start time.Time
		end   time.Time
		want  []pullRequest
	}{
		// perfectly divisible
		{
			time.Unix(0, 0),
			time.Unix(0, int64(time.Minute)),
			[]pullRequest{
				{start: time.Unix(0, 0), end: time.Unix(0, int64(time.Minute/3))},
				{start: time.Unix(0, int64(time.Minute/3)), end: time.Unix(0, int64(time.Minute*2/3))},
				{start: time.Unix(0, int64(time.Minute*2/3)), end: time.Unix(0, int64(time.Minute))},
			},
		},
		// not divisible
		{
			time.Unix(0, 0),
			time.Unix(0, int64(time.Minute+1)),
			[]pullRequest{
				{start: time.Unix(0, 0), end: time.Unix(0, int64(time.Minute/3))},
				{start: time.Unix(0, int64(time.Minute/3)), end: time.Unix(0, int64(time.Minute*2/3))},
				{start: time.Unix(0, int64(time.Minute*2/3)), end: time.Unix(0, int64(time.Minute+1))},
			},
		},
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := splitRequests(tt.start, tt.end, 3)
			if !assert.Equal(t, tt.want, got) {
				for i := range got {
					if !assert.Equal(t, tt.want[i].start, got[i].start) {
						t.Logf("expected i:%d start: %d , got: %d", i, tt.want[i].start.UnixNano(), got[i].start.UnixNano())
					}
					if !assert.Equal(t, tt.want[i].end, got[i].end) {
						t.Logf("expected i:%d end: %d , got: %d", i, tt.want[i].end.UnixNano(), got[i].end.UnixNano())
					}
				}
			}
		})
	}
}
