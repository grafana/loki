package distributor

import (
	"maps"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/v3/pkg/rateservice/client"
	"github.com/grafana/loki/v3/pkg/rateservice/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestRateStoreV2_GetRate(t *testing.T) {
	t.Run("realm does not exist", func(t *testing.T) {
		s := newRateStoreV2()
		actual, ok := s.GetRate("realm1", "name1")
		require.Equal(t, uint64(0), actual)
		require.False(t, ok)
	})

	t.Run("name does not exist", func(t *testing.T) {
		s := newRateStoreV2()
		s.realms["realm1"] = make(map[string]uint64)
		actual, ok := s.GetRate("realm1", "name1")
		require.Equal(t, uint64(0), actual)
		require.False(t, ok)
	})

	t.Run("returns rate for realm and name", func(t *testing.T) {
		s := newRateStoreV2()
		realmRates := make(map[string]uint64)
		realmRates["name1"] = 100
		s.realms["realm1"] = realmRates
		actual, ok := s.GetRate("realm1", "name1")
		require.Equal(t, uint64(100), actual)
		require.True(t, ok)
	})
}

func TestRateStoreV2_GetRates(t *testing.T) {
	t.Run("realm does not exist", func(t *testing.T) {
		s := newRateStoreV2()
		actual, ok := s.GetRates("realm1")
		require.Nil(t, actual)
		require.False(t, ok)
	})

	t.Run("rates for realm", func(t *testing.T) {
		s := newRateStoreV2()
		realmRates := make(map[string]uint64)
		realmRates["name1"] = 100
		s.realms["realm1"] = realmRates
		actual, ok := s.GetRates("realm1")
		expected := maps.Clone(realmRates)
		require.Equal(t, expected, actual)
		require.True(t, ok)
	})

	t.Run("returns a clone", func(t *testing.T) {
		s := newRateStoreV2()
		realmRates := make(map[string]uint64)
		s.realms["realm1"] = realmRates
		actual, ok := s.GetRates("realm1")
		require.NotNil(t, actual)
		// Check that GetRates returns a clone.
		require.NotEqual(
			t,
			reflect.ValueOf(realmRates).Pointer(),
			reflect.ValueOf(actual).Pointer(),
		)
		require.True(t, ok)
	})
}

func TestRateStoreV2_UpdateRates(t *testing.T) {
	t.Run("add a new realm", func(t *testing.T) {
		s := newRateStoreV2()
		realmRates := map[string]uint64{"name1": 100, "name2": 200}
		s.UpdateRates("realm1", realmRates)
		actual, ok := s.realms["realm1"]
		require.True(t, ok)
		require.Equal(t, realmRates, actual)
	})
}

func TestRateBatcherV2_Add(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		b := newRateBatcherV2(nil, time.Minute, log.NewNopLogger(), prometheus.NewRegistry())
		require.Len(t, b.realms, 0)
		// Add a rate to realm1, assert that it is batched.
		nowSecs1 := uint64(time.Now().Unix())
		b.Add("realm1", "name1", 100, nowSecs1)
		require.Len(t, b.realms, 1)
		realmRates, ok := b.realms["realm1"]
		require.NotNil(t, realmRates)
		require.True(t, ok)
		rates, ok := realmRates["name1"]
		require.True(t, ok)
		require.Equal(t, rateBatchV2{nowSecs1: 100}, rates)
		// Add a second rate to realm1 for the same second.
		b.Add("realm1", "name1", 100, nowSecs1)
		require.Equal(t, rateBatchV2{nowSecs1: 200}, rates)
		//  Add a third rate to realm1 but for the next second.
		time.Sleep(time.Second)
		nowSecs2 := uint64(time.Now().Unix())
		require.NotEqual(t, nowSecs2, nowSecs1)
		b.Add("realm1", "name1", 300, nowSecs2)
		require.Equal(t, rateBatchV2{nowSecs1: 200, nowSecs2: 300}, rates)
		// Add a fourth rate but to a different realm.
		b.Add("realm2", "name1", 400, nowSecs2)
		require.Len(t, b.realms, 2)
		// Each realm should have 1 rate each.
		realm1Rates, ok := b.realms["realm1"]
		require.True(t, ok)
		require.Len(t, realm1Rates, 1)
		require.Equal(t, rateBatchV2{nowSecs1: 200, nowSecs2: 300}, realm1Rates["name1"])
		realm2Rates, ok := b.realms["realm2"]
		require.True(t, ok)
		require.Len(t, realm2Rates, 1)
		require.Equal(t, rateBatchV2{nowSecs2: 400}, realm2Rates["name1"])
	})
}

func TestRateBatcherV2_Flush(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reg := prometheus.NewRegistry()
		mock := client.NewMockClient()
		require.Len(t, mock.UpdateRealmRequests(), 0)
		b := newRateBatcherV2(mock, time.Minute, log.NewNopLogger(), reg)
		require.Len(t, b.realms, 0)
		// Add some data to be batched, and then flush it.
		nowSecs1 := uint64(time.Now().Unix())
		b.Add("realm1", "name1", 100, nowSecs1)
		require.NoError(t, b.Flush(t.Context()))
		actual := mock.UpdateRealmRequests()
		expected := []*proto.UpdateRealmRequest{{
			Realm: "realm1",
			Params: []*proto.UpdateRealmParam{{
				Name: "name1",
				Values: []*proto.UpdateRateParam{{
					Ts:    nowSecs1,
					Value: 100,
				}},
			}},
		}}
		require.Equal(t, expected, actual)
		// Add two realms to be batched, and then flush that.
		time.Sleep(time.Second)
		nowSecs2 := uint64(time.Now().Unix())
		b.Add("realm2", "name2", 200, nowSecs2)
		b.Add("realm3", "name3", 300, nowSecs2)
		require.NoError(t, b.Flush(t.Context()))
		actual = mock.UpdateRealmRequests()
		expected = append(expected, &proto.UpdateRealmRequest{
			Realm: "realm2",
			Params: []*proto.UpdateRealmParam{{
				Name: "name2",
				Values: []*proto.UpdateRateParam{{
					Ts:    nowSecs2,
					Value: 200,
				}},
			}},
		})
		expected = append(expected, &proto.UpdateRealmRequest{
			Realm: "realm3",
			Params: []*proto.UpdateRealmParam{{
				Name: "name3",
				Values: []*proto.UpdateRateParam{{
					Ts:    nowSecs2,
					Value: 300,
				}},
			}},
		})
		require.Equal(t, expected, actual)
		// Check the metrics.
		require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(`
	# HELP loki_rate_service_batcher_flushes_total The total number of flushes.
	# TYPE loki_rate_service_batcher_flushes_total counter
	loki_rate_service_batcher_flushes_total 2
	# HELP loki_rate_service_batcher_requests_total The total number of requests to the rate service.
	# TYPE loki_rate_service_batcher_requests_total counter
	loki_rate_service_batcher_requests_total 3
	# HELP loki_rate_service_batcher_requests_failed_total The total number of failed requests to the rate service.
	# TYPE loki_rate_service_batcher_requests_failed_total counter
	loki_rate_service_batcher_requests_failed_total 0
`)))
	})
}
