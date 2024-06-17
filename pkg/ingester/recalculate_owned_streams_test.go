package ingester

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/runtime"
	"github.com/grafana/loki/v3/pkg/validation"
)

func Test_recalculateOwnedStreams_newRecalculateOwnedStreams(t *testing.T) {
	mockInstancesSupplier := &mockTenantsSuplier{tenants: []*instance{}}
	mockRing := newReadRingMock([]ring.InstanceDesc{
		{Addr: "test", Timestamp: time.Now().UnixNano(), State: ring.ACTIVE, Tokens: []uint32{1, 2, 3}},
	}, 0)
	service := newRecalculateOwnedStreams(mockInstancesSupplier.get, "test", mockRing, 50*time.Millisecond, log.NewNopLogger())
	require.Equal(t, 0, mockRing.getAllHealthyCallsCount, "ring must be called only after service's start up")
	ctx := context.Background()
	require.NoError(t, service.StartAsync(ctx))
	require.NoError(t, service.AwaitRunning(ctx))
	require.Eventually(t, func() bool {
		return mockRing.getAllHealthyCallsCount >= 2
	}, 1*time.Second, 50*time.Millisecond, "expected at least two runs of the iteration")
}

func Test_recalculateOwnedStreams_recalculate(t *testing.T) {
	tests := map[string]struct {
		featureEnabled              bool
		expectedOwnedStreamCount    int
		expectedNotOwnedStreamCount int
	}{
		"expected streams ownership to be recalculated": {
			featureEnabled:              true,
			expectedOwnedStreamCount:    4,
			expectedNotOwnedStreamCount: 3,
		},
		"expected streams ownership recalculation to be skipped": {
			featureEnabled:           false,
			expectedOwnedStreamCount: 7,
		},
	}
	for testName, testData := range tests {
		t.Run(testName, func(t *testing.T) {
			mockRing := &readRingMock{
				replicationSet: ring.ReplicationSet{
					Instances: []ring.InstanceDesc{{Addr: "ingester-0", Timestamp: time.Now().UnixNano(), State: ring.ACTIVE, Tokens: []uint32{100, 200, 300}}},
				},
				tokenRangesByIngester: map[string]ring.TokenRanges{
					// this ingester owns token ranges [50, 100] and [200, 300]
					"ingester-0": {50, 100, 200, 300},
				},
			}

			limits, err := validation.NewOverrides(validation.Limits{
				MaxGlobalStreamsPerUser: 100,
				UseOwnedStreamCount:     testData.featureEnabled,
			}, nil)
			require.NoError(t, err)
			limiter := NewLimiter(limits, NilMetrics, mockRing, 1)

			tenant, err := newInstance(
				defaultConfig(),
				defaultPeriodConfigs,
				"tenant-a",
				limiter,
				runtime.DefaultTenantConfigs(),
				noopWAL{},
				NilMetrics,
				nil,
				nil,
				nil,
				nil,
				NewStreamRateCalculator(),
				nil,
				nil,
			)
			require.NoError(t, err)
			// not owned streams
			createStream(t, tenant, 49)
			createStream(t, tenant, 101)
			createStream(t, tenant, 301)

			// owned streams
			createStream(t, tenant, 50)
			createStream(t, tenant, 60)
			createStream(t, tenant, 100)
			createStream(t, tenant, 250)

			require.Equal(t, 7, tenant.ownedStreamsSvc.ownedStreamCount)
			require.Equal(t, 0, tenant.ownedStreamsSvc.notOwnedStreamCount)

			mockTenantsSupplier := &mockTenantsSuplier{tenants: []*instance{tenant}}

			service := newRecalculateOwnedStreams(mockTenantsSupplier.get, "ingester-0", mockRing, 50*time.Millisecond, log.NewNopLogger())

			service.recalculate()

			require.Equal(t, testData.expectedOwnedStreamCount, tenant.ownedStreamsSvc.ownedStreamCount)
			require.Equal(t, testData.expectedNotOwnedStreamCount, tenant.ownedStreamsSvc.notOwnedStreamCount)
		})
	}

}

func Test_recalculateOwnedStreams_checkRingForChanges(t *testing.T) {
	mockRing := &readRingMock{
		replicationSet: ring.ReplicationSet{
			Instances: []ring.InstanceDesc{{Addr: "ingester-0", Timestamp: time.Now().UnixNano(), State: ring.ACTIVE, Tokens: []uint32{100, 200, 300}}},
		},
	}
	mockTenantsSupplier := &mockTenantsSuplier{tenants: []*instance{{}}}
	service := newRecalculateOwnedStreams(mockTenantsSupplier.get, "ingester-0", mockRing, 50*time.Millisecond, log.NewNopLogger())

	ringChanged, err := service.checkRingForChanges()
	require.NoError(t, err)
	require.True(t, ringChanged, "expected ring to be changed because it was not initialized yet")

	ringChanged, err = service.checkRingForChanges()
	require.NoError(t, err)
	require.False(t, ringChanged, "expected ring not to be changed because token ranges is not changed")

	anotherIngester := ring.InstanceDesc{Addr: "ingester-1", Timestamp: time.Now().UnixNano(), State: ring.ACTIVE, Tokens: []uint32{150, 250, 350}}
	mockRing.replicationSet.Instances = append(mockRing.replicationSet.Instances, anotherIngester)

	ringChanged, err = service.checkRingForChanges()
	require.NoError(t, err)
	require.True(t, ringChanged)
}

func createStream(t *testing.T, inst *instance, fingerprint int) {
	lbls := labels.Labels{
		labels.Label{Name: "mock", Value: strconv.Itoa(fingerprint)}}

	_, _, err := inst.streams.LoadOrStoreNew(lbls.String(), func() (*stream, error) {
		return inst.createStreamByFP(lbls, model.Fingerprint(fingerprint))
	}, nil)
	require.NoError(t, err)
}

type mockTenantsSuplier struct {
	tenants []*instance
}

func (m *mockTenantsSuplier) get() []*instance {
	return m.tenants
}
