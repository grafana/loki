package index

import (
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/querier/astmapper"
)

type periodIndex struct {
	time.Time
	idx int // address of the index to use
}

type Multi struct {
	periods []periodIndex
	indices []Interface
}

func (m *Multi) Add(labels []logproto.LabelAdapter, fp model.Fingerprint) (result labels.Labels) {
	for _, i := range m.indices {
		result = i.Add(labels, fp)
	}
	return
}

func (m *Multi) Delete(labels labels.Labels, fp model.Fingerprint) {
	for _, i := range m.indices {
		i.Delete(labels, fp)
	}
	return
}

func (m *Multi) Lookup(t time.Time, matchers []*labels.Matcher, shard *astmapper.ShardAnnotation) ([]model.Fingerprint, error) {
	return m.indexFor(t).Lookup(matchers, shard)
}

func (m *Multi) LabelNames(t time.Time, shard *astmapper.ShardAnnotation) ([]string, error) {
	return m.indexFor(t).LabelNames(shard)
}

func (m *Multi) LabelValues(t time.Time, name string, shard *astmapper.ShardAnnotation) ([]string, error) {
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

func (noopInvertedIndex) Add(labels []logproto.LabelAdapter, fp model.Fingerprint) labels.Labels {
	return nil
}

func (noopInvertedIndex) Delete(labels labels.Labels, fp model.Fingerprint) {
	return nil
}

func (noopInvertedIndex) Lookup(matchers []*labels.Matcher, shard *astmapper.ShardAnnotation) ([]model.Fingerprint, error) {
	return nil, nil
}

func (noopInvertedIndex) LabelNames(shard *astmapper.ShardAnnotation) ([]string, error) {
	return nil, nil
}

func (noopInvertedIndex) LabelValues(name string, shard *astmapper.ShardAnnotation) ([]string, error) {
	return nil, nil
}
