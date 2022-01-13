package storegateway

import (
	"testing"
	"time"

	"github.com/grafana/dskit/ring"
	"github.com/stretchr/testify/assert"
)

func TestIsHealthyForStoreGatewayOperations(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		instance          *ring.InstanceDesc
		timeout           time.Duration
		ownerSyncExpected bool
		ownerReadExpected bool
		readExpected      bool
	}{
		"ACTIVE instance with last keepalive newer than timeout": {
			instance:          &ring.InstanceDesc{State: ring.ACTIVE, Timestamp: time.Now().Add(-30 * time.Second).Unix()},
			timeout:           time.Minute,
			ownerSyncExpected: true,
			ownerReadExpected: true,
			readExpected:      true,
		},
		"ACTIVE instance with last keepalive older than timeout": {
			instance:          &ring.InstanceDesc{State: ring.ACTIVE, Timestamp: time.Now().Add(-90 * time.Second).Unix()},
			timeout:           time.Minute,
			ownerSyncExpected: false,
			ownerReadExpected: false,
			readExpected:      false,
		},
		"JOINING instance with last keepalive newer than timeout": {
			instance:          &ring.InstanceDesc{State: ring.JOINING, Timestamp: time.Now().Add(-30 * time.Second).Unix()},
			timeout:           time.Minute,
			ownerSyncExpected: true,
			ownerReadExpected: false,
			readExpected:      false,
		},
		"LEAVING instance with last keepalive newer than timeout": {
			instance:          &ring.InstanceDesc{State: ring.LEAVING, Timestamp: time.Now().Add(-30 * time.Second).Unix()},
			timeout:           time.Minute,
			ownerSyncExpected: true,
			ownerReadExpected: false,
			readExpected:      false,
		},
		"PENDING instance with last keepalive newer than timeout": {
			instance:          &ring.InstanceDesc{State: ring.PENDING, Timestamp: time.Now().Add(-30 * time.Second).Unix()},
			timeout:           time.Minute,
			ownerSyncExpected: false,
			ownerReadExpected: false,
			readExpected:      false,
		},
	}

	for testName, testData := range tests {
		testData := testData

		t.Run(testName, func(t *testing.T) {
			actual := testData.instance.IsHealthy(BlocksOwnerSync, testData.timeout, time.Now())
			assert.Equal(t, testData.ownerSyncExpected, actual)

			actual = testData.instance.IsHealthy(BlocksOwnerRead, testData.timeout, time.Now())
			assert.Equal(t, testData.ownerReadExpected, actual)

			actual = testData.instance.IsHealthy(BlocksRead, testData.timeout, time.Now())
			assert.Equal(t, testData.readExpected, actual)
		})
	}
}
