package util

import (
	"testing"
	"time"

	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/mock"
)

func TestTokenFor(t *testing.T) {
	if TokenFor("userID", "labels") != 2908432762 {
		t.Errorf("TokenFor(userID, labels) = %v, want 2908432762", TokenFor("userID", "labels"))
	}
}

func TestIsAssignedKey(t *testing.T) {
	for _, tc := range []struct {
		desc   string
		ring   ring.ReadRing
		userID string
		exp    bool
		addr   string
	}{
		{
			desc:   "basic ring and tenant are assigned key",
			ring:   newReadRingMock([]ring.InstanceDesc{{Addr: "127.0.0.1", Timestamp: time.Now().UnixNano(), State: ring.ACTIVE, Tokens: []uint32{1, 2, 3}}}),
			userID: "1",
			exp:    true,
			addr:   "127.0.0.1",
		},
		{
			desc:   "basic ring and tenant are not assigned key",
			ring:   newReadRingMock([]ring.InstanceDesc{{Addr: "127.0.0.2", Timestamp: time.Now().UnixNano(), State: ring.ACTIVE, Tokens: []uint32{1, 2, 3}}}),
			userID: "1",
			exp:    false,
			addr:   "127.0.0.1",
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			if res := IsAssignedKey(tc.ring, newReadLifecyclerMock(tc.addr).addr, tc.userID); res != tc.exp {
				t.Errorf("IsAssignedKey(%v, %v) = %v, want %v", tc.ring, tc.userID, res, tc.exp)
			}
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

func (r *readRingMock) Describe(_ chan<- *prometheus.Desc) {
}

func (r *readRingMock) Collect(_ chan<- prometheus.Metric) {
}

func (r *readRingMock) Get(_ uint32, _ ring.Operation, _ []ring.InstanceDesc, _ []string, _ []string) (ring.ReplicationSet, error) {
	return r.replicationSet, nil
}

func (r *readRingMock) ShuffleShard(_ string, size int) ring.ReadRing {
	// pass by value to copy
	return func(r readRingMock) *readRingMock {
		r.replicationSet.Instances = r.replicationSet.Instances[:size]
		return &r
	}(*r)
}

func (r *readRingMock) BatchGet(_ []uint32, _ ring.Operation) ([]ring.ReplicationSet, error) {
	return []ring.ReplicationSet{r.replicationSet}, nil
}

func (r *readRingMock) GetAllHealthy(_ ring.Operation) (ring.ReplicationSet, error) {
	return r.replicationSet, nil
}

func (r *readRingMock) GetReplicationSetForOperation(_ ring.Operation) (ring.ReplicationSet, error) {
	return r.replicationSet, nil
}

func (r *readRingMock) ReplicationFactor() int {
	return 1
}

func (r *readRingMock) InstancesCount() int {
	return len(r.replicationSet.Instances)
}

func (r *readRingMock) Subring(_ uint32, _ int) ring.ReadRing {
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

func (r *readRingMock) ShuffleShardWithLookback(_ string, _ int, _ time.Duration, _ time.Time) ring.ReadRing {
	return r
}

func (r *readRingMock) CleanupShuffleShardCache(_ string) {}

func (r *readRingMock) GetInstanceState(_ string) (ring.InstanceState, error) {
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
