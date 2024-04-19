// This directory was copied and adapted from https://github.com/grafana/agent/tree/main/pkg/metrics.
// We cannot vendor the agent in since the agent vendors loki in, which would cause a cyclic dependency.
// NOTE: many changes have been made to the original code for our use-case.
package instance

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"net/url"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	common_config "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/exemplar"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/metadata"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/wlog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/util/test"
)

func TestConfig_Unmarshal_Defaults(t *testing.T) {
	cfgText := `name: test`

	cfg, err := UnmarshalConfig(strings.NewReader(cfgText))
	require.NoError(t, err)

	err = cfg.ApplyDefaults()
	require.NoError(t, err)

	require.Equal(t, DefaultConfig.TruncateFrequency, cfg.TruncateFrequency)
	require.Equal(t, DefaultConfig.RemoteFlushDeadline, cfg.RemoteFlushDeadline)
}

func TestConfig_ApplyDefaults_Validations(t *testing.T) {
	cfg := DefaultConfig
	cfg.Name = "instance"
	cfg.RemoteWrite = []*config.RemoteWriteConfig{{Name: "write"}}

	tt := []struct {
		name     string
		mutation func(c *Config)
		err      error
	}{
		{
			"valid config",
			nil,
			nil,
		},
		{
			"requires name",
			func(c *Config) { c.Name = "" },
			fmt.Errorf("missing instance name"),
		},
		{
			"missing wal truncate frequency",
			func(c *Config) { c.TruncateFrequency = 0 },
			fmt.Errorf("wal_truncate_frequency must be greater than 0s"),
		},
		{
			"missing remote flush deadline",
			func(c *Config) { c.RemoteFlushDeadline = 0 },
			fmt.Errorf("remote_flush_deadline must be greater than 0s"),
		},
		{
			"empty remote write",
			func(c *Config) { c.RemoteWrite = append(c.RemoteWrite, nil) },
			fmt.Errorf("empty or null remote write config section"),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {

			// Copy the input and all of its slices
			input := cfg

			var remoteWrites []*config.RemoteWriteConfig
			for _, rw := range input.RemoteWrite {
				rwCopy := *rw
				remoteWrites = append(remoteWrites, &rwCopy)
			}
			input.RemoteWrite = remoteWrites

			if tc.mutation != nil {
				tc.mutation(&input)
			}

			err := input.ApplyDefaults()
			if tc.err == nil {
				require.NoError(t, err)
			} else {
				require.EqualError(t, err, tc.err.Error())
			}
		})
	}
}

func TestMetricValueCollector(t *testing.T) {
	r := prometheus.NewRegistry()
	vc := NewMetricValueCollector(r, "this_should_be_tracked")

	shouldTrack := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "this_should_be_tracked",
		ConstLabels: prometheus.Labels{
			"foo": "bar",
		},
	})

	shouldTrack.Set(12345)

	shouldNotTrack := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "this_should_not_be_tracked",
	})

	r.MustRegister(shouldTrack, shouldNotTrack)

	vals, err := vc.GetValues("foo", "bar")
	require.NoError(t, err)
	require.Equal(t, []float64{12345}, vals)
}

func TestRemoteWriteMetricInterceptor_AllValues(t *testing.T) {
	r := prometheus.NewRegistry()
	vc := NewMetricValueCollector(r, "track")

	valueA := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "this_should_be_tracked",
		ConstLabels: prometheus.Labels{
			"foo": "bar",
		},
	})
	valueA.Set(12345)

	valueB := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "track_this_too",
		ConstLabels: prometheus.Labels{
			"foo": "bar",
		},
	})
	valueB.Set(67890)

	shouldNotReturn := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "track_this_but_label_does_not_match",
		ConstLabels: prometheus.Labels{
			"foo": "nope",
		},
	})

	r.MustRegister(valueA, valueB, shouldNotReturn)

	vals, err := vc.GetValues("foo", "bar")
	require.NoError(t, err)
	require.Equal(t, []float64{12345, 67890}, vals)
}

// TestInstance tests that discovery and scraping are working by using a mock
// instance of the WAL storage and testing that samples get written to it.
// This test touches most of Instance and is enough for a basic integration test.
func TestInstance(t *testing.T) {
	walDir := t.TempDir()

	mockStorage := mockWalStorage{
		series:    make(map[storage.SeriesRef]int),
		directory: walDir,
	}
	newWal := func(_ prometheus.Registerer) (walStorage, error) { return &mockStorage, nil }

	logger := level.NewFilter(log.NewLogfmtLogger(log.NewSyncWriter(os.Stderr)), level.AllowInfo())
	cfg := DefaultConfig
	cfg.Dir = walDir
	inst, err := newInstance(cfg, nil, logger, newWal, "12345")
	require.NoError(t, err)
	runInstance(t, inst)

	// Wait until mockWalStorage is initialized.
	test.Poll(t, 10*time.Second, true, func() interface{} {
		mockStorage.mut.Lock()
		defer mockStorage.mut.Unlock()
		return inst.Ready()
	})

	app := inst.Appender(context.TODO())
	refTime := time.Now().UnixNano()

	count := 3
	for i := 0; i < count; i++ {
		_, err := app.Append(0, labels.Labels{
			labels.Label{Name: "__name__", Value: "test"},
			labels.Label{Name: "iter", Value: fmt.Sprintf("%v", i)},
		}, refTime-int64(i), float64(i))

		require.NoError(t, err)
	}
	assert.Len(t, mockStorage.series, count)

	require.NotPanics(t, func() {
		sendInterval := 100 * time.Millisecond
		cfg.RemoteWrite = []*config.RemoteWriteConfig{
			{
				Name: "write-test",
				URL: &common_config.URL{
					URL: &url.URL{
						Scheme: "http",
						Host:   "localhost:8080",
						Path:   "/write",
					},
				},
				RemoteTimeout: config.DefaultRemoteWriteConfig.RemoteTimeout,
				QueueConfig:   config.DefaultRemoteWriteConfig.QueueConfig,
				MetadataConfig: config.MetadataConfig{
					Send:              true,
					MaxSamplesPerSend: 1,
					SendInterval:      model.Duration(sendInterval),
				},
			},
		}
		err = inst.Update(cfg)
		assert.NoError(t, err)
		// Wait enough time for the metrics send interval to kick in.
		time.Sleep(2 * sendInterval)
	})
}

type mockWalStorage struct {
	storage.Queryable
	storage.ChunkQueryable

	directory string
	mut       sync.Mutex
	series    map[storage.SeriesRef]int
}

func (s *mockWalStorage) Directory() string                          { return s.directory }
func (s *mockWalStorage) StartTime() (int64, error)                  { return 0, nil }
func (s *mockWalStorage) WriteStalenessMarkers(_ func() int64) error { return nil }
func (s *mockWalStorage) SetWriteNotified(_ wlog.WriteNotified)      {}
func (s *mockWalStorage) Close() error                               { return nil }
func (s *mockWalStorage) Truncate(_ int64) error                     { return nil }

func (s *mockWalStorage) Appender(context.Context) storage.Appender {
	return &mockAppender{s: s}
}

type mockAppender struct {
	s *mockWalStorage
}

func (a *mockAppender) Append(ref storage.SeriesRef, l labels.Labels, t int64, v float64) (storage.SeriesRef, error) {
	if ref == 0 {
		return a.Add(l, t, v)
	}
	return ref, a.AddFast(ref, t, v)
}

// Add adds a new series and sets its written count to 1.
func (a *mockAppender) Add(l labels.Labels, _ int64, _ float64) (storage.SeriesRef, error) {
	a.s.mut.Lock()
	defer a.s.mut.Unlock()

	hash := l.Hash()
	a.s.series[storage.SeriesRef(hash)] = 1
	return storage.SeriesRef(hash), nil
}

// AddFast increments the number of writes to an existing series.
func (a *mockAppender) AddFast(ref storage.SeriesRef, _ int64, _ float64) error {
	a.s.mut.Lock()
	defer a.s.mut.Unlock()
	_, ok := a.s.series[ref]
	if !ok {
		return storage.ErrNotFound
	}

	a.s.series[ref]++
	return nil
}

func (a *mockAppender) AppendExemplar(_ storage.SeriesRef, _ labels.Labels, _ exemplar.Exemplar) (storage.SeriesRef, error) {
	return 0, nil
}

func (a *mockAppender) UpdateMetadata(_ storage.SeriesRef, _ labels.Labels, _ metadata.Metadata) (storage.SeriesRef, error) {
	return 0, nil
}

func (a *mockAppender) AppendHistogram(_ storage.SeriesRef, _ labels.Labels, _ int64, _ *histogram.Histogram, _ *histogram.FloatHistogram) (storage.SeriesRef, error) {
	return 0, nil
}

func (a *mockAppender) AppendCTZeroSample(_ storage.SeriesRef, _ labels.Labels, _ int64, _ int64) (storage.SeriesRef, error) {
	return 0, nil
}

func (a *mockAppender) Commit() error {
	return nil
}

func (a *mockAppender) Rollback() error {
	return nil
}

func runInstance(t *testing.T, i *Instance) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel() })
	go require.NotPanics(t, func() {
		_ = i.Run(ctx)
	})
}
