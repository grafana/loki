package util

import (
	"testing"
	"time"

	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestTokenFor(t *testing.T) {
	if TokenFor("userID", "labels") != 2908432762 {
		t.Errorf("TokenFor(userID, labels) = %v, want 2908432762", TokenFor("userID", "labels"))
	}
}

func TestIsInReplicationSet(t *testing.T) {
	for _, tc := range []struct {
		desc   string
		ring   ring.DynamicReplicationReadRing
		userID string
		exp    bool
		addr   string
	}{
		{
			desc:   "is in replication set",
			ring:   newReadRingMock([]ring.InstanceDesc{{Addr: "127.0.0.1", Timestamp: time.Now().UnixNano(), State: ring.ACTIVE, Tokens: []uint32{1, 2, 3}}}),
			userID: "1",
			exp:    true,
			addr:   "127.0.0.1",
		},
		{
			desc:   "is not in replication set",
			ring:   newReadRingMock([]ring.InstanceDesc{{Addr: "127.0.0.2", Timestamp: time.Now().UnixNano(), State: ring.ACTIVE, Tokens: []uint32{1, 2, 3}}}),
			userID: "1",
			exp:    false,
			addr:   "127.0.0.1",
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			token := TokenFor(tc.userID, "")
			res, err := IsInReplicationSet(tc.ring, token, newReadLifecyclerMock(tc.addr).addr)
			require.NoError(t, err)
			require.Equal(t, tc.exp, res, "IsInReplicationSet(%v, %v) = %v, want %v", tc.ring, tc.userID, res, tc.exp)
		})
	}
}

type readRingMock struct {
	replicationSet ring.ReplicationSet
}

func newReadRingMock(ingesters []ring.InstanceDesc) *readRingMock {
	return &readRingMock{
		replicationSet: ring.ReplicationSet{
			Instances: ingesters,
			MaxErrors: 0,
		},
	}
}

func (r *readRingMock) Describe(ch chan<- *prometheus.Desc) {
}

func (r *readRingMock) Collect(ch chan<- prometheus.Metric) {
}

func (r *readRingMock) Get(key uint32, op ring.Operation, buf []ring.InstanceDesc, _ []string, _ []string) (ring.ReplicationSet, error) {
	return r.replicationSet, nil
}

func (r *readRingMock) GetWithRF(key uint32, op ring.Operation, buf []ring.InstanceDesc, _ []string, _ []string, _ int) (ring.ReplicationSet, error) {
	return r.replicationSet, nil
}

func (r *readRingMock) ShuffleShard(identifier string, size int) ring.ReadRing {
	// pass by value to copy
	return func(r readRingMock) *readRingMock {
		r.replicationSet.Instances = r.replicationSet.Instances[:size]
		return &r
	}(*r)
}

func (r *readRingMock) BatchGet(_ []uint32, op ring.Operation) ([]ring.ReplicationSet, error) {
	return []ring.ReplicationSet{r.replicationSet}, nil
}

func (r *readRingMock) GetAllHealthy(_ ring.Operation) (ring.ReplicationSet, error) {
	return r.replicationSet, nil
}

func (r *readRingMock) GetReplicationSetForOperation(op ring.Operation) (ring.ReplicationSet, error) {
	return r.replicationSet, nil
}

func (r *readRingMock) ReplicationFactor() int {
	return 1
}

func (r *readRingMock) InstancesCount() int {
	return len(r.replicationSet.Instances)
}

func (r *readRingMock) Subring(key uint32, n int) ring.ReadRing {
	return r
}

func (r *readRingMock) HasInstance(instanceID string) bool {
	for _, ing := range r.replicationSet.Instances {
		if ing.Addr != instanceID {
			return true
		}
	}
	return false
}

func (r *readRingMock) ShuffleShardWithLookback(identifier string, size int, lookbackPeriod time.Duration, now time.Time) ring.ReadRing {
	return r
}

func (r *readRingMock) CleanupShuffleShardCache(identifier string) {}

func (r *readRingMock) GetInstanceState(instanceID string) (ring.InstanceState, error) {
	return 0, nil
}

type readLifecyclerMock struct {
	mock.Mock
	addr string
}

func newReadLifecyclerMock(addr string) *readLifecyclerMock {
	return &readLifecyclerMock{
		addr: addr,
	}
}

func (m *readLifecyclerMock) HealthyInstancesCount() int {
	args := m.Called()
	return args.Int(0)
}

func (m *readLifecyclerMock) GetInstanceAddr() string {
	return m.addr
}

func TestDynamicReplicationFactor(t *testing.T) {
	mockRing := newReadRingMock([]ring.InstanceDesc{
		{Addr: "127.0.0.1", Timestamp: time.Now().UnixNano(), State: ring.ACTIVE, Tokens: []uint32{1, 2, 3}},
		{Addr: "127.0.0.2", Timestamp: time.Now().UnixNano(), State: ring.ACTIVE, Tokens: []uint32{4, 5, 6}},
		{Addr: "127.0.0.3", Timestamp: time.Now().UnixNano(), State: ring.ACTIVE, Tokens: []uint32{7, 8, 9}},
	})

	for _, tc := range []struct {
		desc     string
		factor   float64
		expected int
	}{
		{
			desc:     "factor 0.0",
			factor:   0,
			expected: 1, // ring.ReplicationFactor()
		},
		{
			desc:     "factor 0.5",
			factor:   0.5,
			expected: 2, // 1 + (3 - 1) * 0.5
		},
		{
			desc:     "factor 1.0",
			factor:   1,
			expected: 3, // num instances
		},
		{
			desc:     "factor > 1.0",
			factor:   2,
			expected: 3, // num instances
		},
	} {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			require.Equal(t, tc.expected, DynamicReplicationFactor(mockRing, tc.factor))
		})
	}
}
