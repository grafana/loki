package pattern

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestPatternPersistenceConfiguration tests that pattern persistence writers
// are correctly configured based on the limits configuration.
func TestPatternPersistenceConfiguration(t *testing.T) {
	tests := []struct {
		name                      string
		patternPersistenceEnabled bool
		metricAggregationEnabled  bool
		expectPatternWriter       bool
		expectMetricWriter        bool
	}{
		{
			name:                      "both disabled",
			patternPersistenceEnabled: false,
			metricAggregationEnabled:  false,
			expectPatternWriter:       false,
			expectMetricWriter:        false,
		},
		{
			name:                      "only pattern persistence enabled",
			patternPersistenceEnabled: true,
			metricAggregationEnabled:  false,
			expectPatternWriter:       true,
			expectMetricWriter:        false,
		},
		{
			name:                      "only metric aggregation enabled",
			patternPersistenceEnabled: false,
			metricAggregationEnabled:  true,
			expectPatternWriter:       false,
			expectMetricWriter:        true,
		},
		{
			name:                      "both enabled",
			patternPersistenceEnabled: true,
			metricAggregationEnabled:  true,
			expectPatternWriter:       true,
			expectMetricWriter:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mock limits
			limits := &configurableLimits{
				patternPersistenceEnabled: tt.patternPersistenceEnabled,
				metricAggregationEnabled:  tt.metricAggregationEnabled,
			}

			// Setup ring
			replicationSet := ring.ReplicationSet{
				Instances: []ring.InstanceDesc{
					{Id: "localhost", Addr: "ingester0"},
				},
			}

			fakeRing := &fakeRing{}
			fakeRing.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
				Return(replicationSet, nil)

			ringClient := &fakeRingClient{
				ring: fakeRing,
			}

			// Create ingester with the specified configuration
			cfg := testIngesterConfig(t)
			ing, err := New(cfg, limits, ringClient, "test", nil, log.NewNopLogger())
			require.NoError(t, err)
			defer services.StopAndAwaitTerminated(context.Background(), ing) //nolint:errcheck

			err = services.StartAndAwaitRunning(context.Background(), ing)
			require.NoError(t, err)

			// Create test instance
			_ = user.InjectOrgID(context.Background(), "test-tenant")
			instance, err := ing.GetOrCreateInstance("test-tenant")
			require.NoError(t, err)

			// Verify writer configuration based on test expectations
			if tt.expectPatternWriter {
				require.NotNil(t, instance.patternWriter, "pattern writer should be configured when pattern persistence is enabled")
			} else {
				require.Nil(t, instance.patternWriter, "pattern writer should be nil when pattern persistence is disabled")
			}

			if tt.expectMetricWriter {
				require.NotNil(t, instance.metricWriter, "metric writer should be configured when metric aggregation is enabled")
			} else {
				require.Nil(t, instance.metricWriter, "metric writer should be nil when metric aggregation is disabled")
			}

			// Verify they are different instances when both are enabled
			if tt.expectPatternWriter && tt.expectMetricWriter {
				require.NotEqual(t, instance.patternWriter, instance.metricWriter,
					"pattern writer and metric writer should be different instances")
			}
		})
	}
}

// TestPatternPersistenceStopWriters tests that both pattern and metric writers
// are properly stopped when the ingester shuts down
func TestPatternPersistenceStopWriters(t *testing.T) {
	mockPatternWriter := &mockEntryWriter{}
	mockMetricWriter := &mockEntryWriter{}

	mockPatternWriter.On("Stop").Return()
	mockMetricWriter.On("Stop").Return()

	replicationSet := ring.ReplicationSet{
		Instances: []ring.InstanceDesc{
			{Id: "localhost", Addr: "ingester0"},
		},
	}

	fakeRing := &fakeRing{}
	fakeRing.On("Get", mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).
		Return(replicationSet, nil)

	ringClient := &fakeRingClient{ring: fakeRing}

	cfg := testIngesterConfig(t)
	ing, err := New(cfg, &configurableLimits{
		patternPersistenceEnabled: true,
		metricAggregationEnabled:  true,
	}, ringClient, "test", nil, log.NewNopLogger())
	require.NoError(t, err)

	err = services.StartAndAwaitRunning(context.Background(), ing)
	require.NoError(t, err)

	// Create instance and replace writers with mocks
	instance, err := ing.GetOrCreateInstance("test-tenant")
	require.NoError(t, err)

	instance.patternWriter = mockPatternWriter
	instance.metricWriter = mockMetricWriter

	// Stop the ingester - this should call stopWriters
	err = services.StopAndAwaitTerminated(context.Background(), ing)
	require.NoError(t, err)

	// Verify both writers were stopped
	mockPatternWriter.AssertCalled(t, "Stop")
	mockMetricWriter.AssertCalled(t, "Stop")
}

// configurableLimits implements the Limits interface with configurable pattern persistence
type configurableLimits struct {
	patternPersistenceEnabled bool
	metricAggregationEnabled  bool
}

var _ Limits = &configurableLimits{}

func (c *configurableLimits) PatternIngesterTokenizableJSONFields(_ string) []string {
	return []string{"log", "message", "msg", "msg_", "_msg", "content"}
}

func (c *configurableLimits) PatternPersistenceEnabled(_ string) bool {
	return c.patternPersistenceEnabled
}

func (c *configurableLimits) MetricAggregationEnabled(_ string) bool {
	return c.metricAggregationEnabled
}

func testIngesterConfig(t testing.TB) Config {
	kvClient, err := kv.NewClient(kv.Config{Store: "inmemory"}, ring.GetCodec(), nil, log.NewNopLogger())
	require.NoError(t, err)

	cfg := Config{}
	flagext.DefaultValues(&cfg)
	cfg.FlushCheckPeriod = 99999 * time.Hour
	cfg.ConcurrentFlushes = 1
	cfg.LifecyclerConfig.RingConfig.KVStore.Mock = kvClient
	cfg.LifecyclerConfig.NumTokens = 1
	cfg.LifecyclerConfig.ListenPort = 0
	cfg.LifecyclerConfig.Addr = "localhost"
	cfg.LifecyclerConfig.ID = "localhost"
	cfg.LifecyclerConfig.FinalSleep = 0
	cfg.LifecyclerConfig.MinReadyDuration = 0

	// Configure pattern persistence
	cfg.PatternPersistence.LokiAddr = "http://localhost:3100"
	cfg.PatternPersistence.WriteTimeout = 30 * time.Second
	cfg.PatternPersistence.PushPeriod = 10 * time.Second
	cfg.PatternPersistence.BatchSize = 1000

	// Configure metric aggregation
	cfg.MetricAggregation.LokiAddr = "http://localhost:3100"
	cfg.MetricAggregation.WriteTimeout = 30 * time.Second
	cfg.MetricAggregation.SamplePeriod = 10 * time.Second

	return cfg
}
