package index

import (
	"fmt"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/types"
)

type periodIndex struct {
	time.Time
	idx int // address of the index to use
}

type Multi struct {
	periods []periodIndex
	indices []Interface
}

func NewMultiInvertedIndex(periods []config.PeriodConfig, indexShards uint32) (*Multi, error) {
	var (
		err error

		ii          Interface // always stored in 0th index
		bitPrefixed Interface // always stored in 1st index

		periodIndices []periodIndex
	)

	for _, pd := range periods {
		switch pd.IndexType {
		case types.TSDBType:
			if bitPrefixed == nil {
				bitPrefixed, err = NewBitPrefixWithShards(indexShards)
				if err != nil {
					return nil, fmt.Errorf("creating tsdb inverted index for period starting %v: %w", pd.From, err)
				}
			}
			periodIndices = append(periodIndices, periodIndex{
				Time: pd.From.Time.Time(),
				idx:  1, // tsdb inverted index is always stored in position one
			})
		default:
			if ii == nil {
				ii = NewWithShards(indexShards)
			}
			periodIndices = append(periodIndices, periodIndex{
				Time: pd.From.Time.Time(),
				idx:  0, // regular inverted index is always stored in position zero
			})
		}
	}

	return &Multi{
		periods: periodIndices,
		indices: []Interface{ii, bitPrefixed},
	}, nil
}

func (m *Multi) Add(labels []logproto.LabelAdapter, fp model.Fingerprint) (result labels.Labels) {
	for _, i := range m.indices {
		if i != nil {
			result = i.Add(labels, fp)
		}
	}
	return
}

func (m *Multi) Delete(labels labels.Labels, fp model.Fingerprint) {
	for _, i := range m.indices {
		if i != nil {
			i.Delete(labels, fp)
		}
	}

}

func (m *Multi) Lookup(t time.Time, matchers []*labels.Matcher, shard *logql.Shard) ([]model.Fingerprint, error) {
	return m.indexFor(t).Lookup(matchers, shard)
}

func (m *Multi) LabelNames(t time.Time, shard *logql.Shard) ([]string, error) {
	return m.indexFor(t).LabelNames(shard)
}

func (m *Multi) LabelValues(t time.Time, name string, shard *logql.Shard) ([]string, error) {
	return m.indexFor(t).LabelValues(name, shard)
}

// Query planning is responsible for ensuring no query spans more than one inverted index.
// Therefore we don't need to account for both `from` and `through`.
func (m *Multi) indexFor(t time.Time) Interface {
	for i := range m.periods {
		if !m.periods[i].Time.After(t) && (i+1 == len(m.periods) || t.Before(m.periods[i+1].Time)) {
			return m.indices[m.periods[i].idx]
		}
	}
	return noopInvertedIndex{}
}

type noopInvertedIndex struct{}

func (noopInvertedIndex) Add(_ []logproto.LabelAdapter, _ model.Fingerprint) labels.Labels {
	return nil
}

func (noopInvertedIndex) Delete(_ labels.Labels, _ model.Fingerprint) {}

func (noopInvertedIndex) Lookup(_ []*labels.Matcher, _ *logql.Shard) ([]model.Fingerprint, error) {
	return nil, nil
}

func (noopInvertedIndex) LabelNames(_ *logql.Shard) ([]string, error) {
	return nil, nil
}

func (noopInvertedIndex) LabelValues(_ string, _ *logql.Shard) ([]string, error) {
	return nil, nil
}
