package index

import (
	"sort"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/querier/astmapper"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
)

func MustParseDayTime(s string) config.DayTime {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return config.DayTime{Time: model.TimeFromUnix(t.Unix())}
}

var testPeriodConfigs = []config.PeriodConfig{
	{
		From:      MustParseDayTime("2020-01-01"),
		IndexType: config.BoltDBShipperType,
	},
	{
		From:      MustParseDayTime("2021-01-01"),
		IndexType: config.TSDBType,
	},
}

// Only run the specific shard factor validation logic if a period config using
// tsdb exists
func TestIgnoresInvalidShardFactorWhenTSDBNotPresent(t *testing.T) {
	factor := uint32(6)
	_, err := NewMultiInvertedIndex(
		[]config.PeriodConfig{
			{
				From:      MustParseDayTime("2020-01-01"),
				IndexType: config.BoltDBShipperType,
			},
		},
		factor,
	)
	require.Nil(t, err)

	_, err = NewMultiInvertedIndex(
		[]config.PeriodConfig{
			{
				From:      MustParseDayTime("2020-01-01"),
				IndexType: config.BoltDBShipperType,
			},
			{
				From:      MustParseDayTime("2021-01-01"),
				IndexType: config.TSDBType,
			},
		},
		factor,
	)
	require.Error(t, err)
}

func TestMultiIndexCreation(t *testing.T) {
	multi, err := NewMultiInvertedIndex(testPeriodConfigs, uint32(2))
	require.Nil(t, err)

	x, _ := NewBitPrefixWithShards(2)
	expected := &Multi{
		periods: []periodIndex{
			{
				Time: testPeriodConfigs[0].From.Time.Time(),
				idx:  0,
			},
			{
				Time: testPeriodConfigs[1].From.Time.Time(),
				idx:  1,
			},
		},
		indices: []Interface{
			NewWithShards(2),
			x,
		},
	}
	require.Equal(t, expected, multi)
}

func TestMultiIndex(t *testing.T) {
	factor := uint32(32)
	multi, err := NewMultiInvertedIndex(testPeriodConfigs, factor)
	require.Nil(t, err)

	lbs := []logproto.LabelAdapter{
		{Name: "foo", Value: "foo"},
		{Name: "bar", Value: "bar"},
		{Name: "buzz", Value: "buzz"},
	}
	sort.Sort(logproto.FromLabelAdaptersToLabels(lbs))
	fp := model.Fingerprint((logproto.FromLabelAdaptersToLabels(lbs).Hash()))

	ls := multi.Add(lbs, fp)

	// Lookup at a time corresponding to a non-tsdb periodconfig
	// and ensure we use modulo hashing
	expShard := labelsSeriesIDHash(logproto.FromLabelAdaptersToLabels(lbs)) % factor
	ids, err := multi.Lookup(
		testPeriodConfigs[0].From.Time.Time(),
		[]*labels.Matcher{
			labels.MustNewMatcher(labels.MatchEqual, "foo", "foo"),
		},
		&astmapper.ShardAnnotation{Shard: int(expShard), Of: int(factor)},
	)

	require.Nil(t, err)
	require.Equal(t, []model.Fingerprint{fp}, ids)

	// Lookup at a time corresponding to a tsdb periodconfig
	// and ensure we use bit prefix hashing
	requiredBits := index.NewShard(0, factor).RequiredBits()
	expShard = uint32(fp >> (64 - requiredBits))
	ids, err = multi.Lookup(
		testPeriodConfigs[1].From.Time.Time(),
		[]*labels.Matcher{
			labels.MustNewMatcher(labels.MatchEqual, "foo", "foo"),
		},
		&astmapper.ShardAnnotation{Shard: int(expShard), Of: int(factor)},
	)

	require.Nil(t, err)
	require.Equal(t, []model.Fingerprint{fp}, ids)

	// Delete the entry
	multi.Delete(ls, fp)

	// Ensure deleted entry is not in modulo variant
	ids, err = multi.Lookup(
		testPeriodConfigs[0].From.Time.Time(),
		[]*labels.Matcher{
			labels.MustNewMatcher(labels.MatchEqual, "foo", "foo"),
		},
		nil,
	)

	require.Nil(t, err)
	require.Equal(t, 0, len(ids))

	// Ensure deleted entry is not in bit prefix variant
	ids, err = multi.Lookup(
		testPeriodConfigs[1].From.Time.Time(),
		[]*labels.Matcher{
			labels.MustNewMatcher(labels.MatchEqual, "foo", "foo"),
		},
		nil,
	)

	require.Nil(t, err)
	require.Equal(t, 0, len(ids))
}
