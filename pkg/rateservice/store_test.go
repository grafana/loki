package rateservice

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/grafana/loki/v3/pkg/rateservice/proto"
	"github.com/stretchr/testify/require"
)

func TestRateStore_GetRealm(t *testing.T) {
	t.Run("realm does not exist", func(t *testing.T) {
		s := newRateStore(300, 15)
		results, ok := s.GetRealm("realm1")
		require.Nil(t, results)
		require.False(t, ok)
	})

	t.Run("realm exists but it has no rates", func(t *testing.T) {
		s := newRateStore(300, 15)
		s.realms["realm1"] = make(map[string][]rateBucket)
		results, ok := s.GetRealm("realm1")
		require.True(t, ok)
		require.Len(t, results, 0)
	})

	t.Run("realm contains rates inside window", func(t *testing.T) {
		s := newRateStore(300, 15)
		buckets := make([]rateBucket, 20)
		buckets[0].value = 100
		// Truncate ts to a multiple of the bucket size.
		buckets[0].ts = uint64(time.Now().
			Truncate(15 * time.Second).
			Unix())
		s.realms["realm1"] = make(map[string][]rateBucket)
		s.realms["realm1"]["foo"] = buckets
		results, ok := s.GetRealm("realm1")
		require.True(t, ok)
		require.Len(t, results, 1)
		require.Equal(t, "foo", results[0].Name)
		require.Equal(t, uint64(0x64), results[0].Value)
	})

	t.Run("realm contains rates outside window", func(t *testing.T) {
		s := newRateStore(300, 15)
		buckets := make([]rateBucket, 1)
		buckets[0].value = 100
		// Truncate ts to a multiple of the bucket size.
		buckets[0].ts = uint64(time.Now().
			Add(-6 * time.Minute).
			Truncate(15 * time.Second).
			Unix())
		s.realms["realm1"] = make(map[string][]rateBucket)
		s.realms["realm1"]["foo"] = buckets
		results, ok := s.GetRealm("realm1")
		require.True(t, ok)
		require.Len(t, results, 1)
		// The value should be zero as all buckets outside the window.
		require.Equal(t, "foo", results[0].Name)
		require.Equal(t, uint64(0), results[0].Value)
	})
}

func TestRateStore_UpdateRealm(t *testing.T) {
	t.Run("add new realm", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			s := newRateStore(300, 15)
			nowSecs1 := unixSecs()
			s.UpdateRealm("realm1", []*proto.UpdateRealmParam{{
				Name: "foo",
				Values: []*proto.UpdateRateParam{{
					Value: 100,
					Ts:    nowSecs1,
				}},
			}})
			// Check that the expected bucket was incremented.
			realm, ok := s.realms["realm1"]
			require.True(t, ok)
			buckets, ok := realm["foo"]
			require.True(t, ok)
			require.Len(t, buckets, 20)
			expected := make([]rateBucket, 20)
			expected[0].ts = nowSecs1
			expected[0].value = 100
			require.Equal(t, expected, buckets)

			// Advance time, but keep it within the same bucket.
			time.Sleep(14 * time.Second)
			nowSecs2 := unixSecs()
			require.NotEqual(t, nowSecs1, nowSecs2)
			s.UpdateRealm("realm1", []*proto.UpdateRealmParam{{
				Name: "foo",
				Values: []*proto.UpdateRateParam{{
					Value: 100,
					Ts:    nowSecs2,
				}},
			}})
			expected[0].value = 200
			require.Equal(t, expected, buckets)

			// Advance time, but move to the third bucket.
			time.Sleep(16 * time.Second)
			nowSecs3 := unixSecs()
			require.NotEqual(t, nowSecs3, nowSecs2)
			s.UpdateRealm("realm1", []*proto.UpdateRealmParam{{
				Name: "foo",
				Values: []*proto.UpdateRateParam{{
					Value: 300,
					Ts:    nowSecs3,
				}},
			}})
			expected[2].ts = nowSecs3
			expected[2].value = 300
			require.Equal(t, expected, buckets)

			// Wrap around to the first bucket.
			time.Sleep(270 * time.Second)
			nowSecs4 := unixSecs()
			require.Equal(t, nowSecs1+300, nowSecs4)
			s.UpdateRealm("realm1", []*proto.UpdateRealmParam{{
				Name: "foo",
				Values: []*proto.UpdateRateParam{{
					Value: 400,
					Ts:    nowSecs4,
				}},
			}})
			expected[0].ts = nowSecs4
			expected[0].value = 400
			require.Equal(t, expected, buckets)
		})
	})
}
