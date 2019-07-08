package chunk

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/weaveworks/common/test"
)

type mockStore int

func (m mockStore) Put(ctx context.Context, chunks []Chunk) error {
	return nil
}

func (m mockStore) PutOne(ctx context.Context, from, through model.Time, chunk Chunk) error {
	return nil
}

func (m mockStore) Get(tx context.Context, from, through model.Time, matchers ...*labels.Matcher) ([]Chunk, error) {
	return nil, nil
}
func (m mockStore) LabelValuesForMetricName(ctx context.Context, from, through model.Time, metricName string, labelName string) ([]string, error) {
	return nil, nil
}

func (m mockStore) GetChunkRefs(tx context.Context, from, through model.Time, matchers ...*labels.Matcher) ([][]Chunk, []*Fetcher, error) {
	return nil, nil, nil
}

func (m mockStore) LabelNamesForMetricName(ctx context.Context, from, through model.Time, metricName string) ([]string, error) {
	return nil, nil
}

func (m mockStore) Stop() {}

func TestCompositeStore(t *testing.T) {
	type result struct {
		from, through model.Time
		store         Store
	}
	collect := func(results *[]result) func(from, through model.Time, store Store) error {
		return func(from, through model.Time, store Store) error {
			*results = append(*results, result{from, through, store})
			return nil
		}
	}
	cs := compositeStore{
		stores: []compositeStoreEntry{
			{model.TimeFromUnix(0), mockStore(1)},
			{model.TimeFromUnix(100), mockStore(2)},
			{model.TimeFromUnix(200), mockStore(3)},
		},
	}

	for i, tc := range []struct {
		cs            compositeStore
		from, through int64
		want          []result
	}{
		// Test we have sensible results when there are no schema's defined
		{compositeStore{}, 0, 1, []result{}},

		// Test we have sensible results when there is a single schema
		{
			compositeStore{
				stores: []compositeStoreEntry{
					{model.TimeFromUnix(0), mockStore(1)},
				},
			},
			0, 10,
			[]result{
				{model.TimeFromUnix(0), model.TimeFromUnix(10), mockStore(1)},
			},
		},

		// Test we have sensible results for negative (ie pre 1970) times
		{
			compositeStore{
				stores: []compositeStoreEntry{
					{model.TimeFromUnix(0), mockStore(1)},
				},
			},
			-10, -9,
			[]result{},
		},
		{
			compositeStore{
				stores: []compositeStoreEntry{
					{model.TimeFromUnix(0), mockStore(1)},
				},
			},
			-10, 10,
			[]result{
				{model.TimeFromUnix(0), model.TimeFromUnix(10), mockStore(1)},
			},
		},

		// Test we have sensible results when there is two schemas
		{
			compositeStore{
				stores: []compositeStoreEntry{
					{model.TimeFromUnix(0), mockStore(1)},
					{model.TimeFromUnix(100), mockStore(2)},
				},
			},
			34, 165,
			[]result{
				{model.TimeFromUnix(34), model.TimeFromUnix(100) - 1, mockStore(1)},
				{model.TimeFromUnix(100), model.TimeFromUnix(165), mockStore(2)},
			},
		},

		// Test we get only one result when two schema start at same time
		{
			compositeStore{
				stores: []compositeStoreEntry{
					{model.TimeFromUnix(0), mockStore(1)},
					{model.TimeFromUnix(10), mockStore(2)},
					{model.TimeFromUnix(10), mockStore(3)},
				},
			},
			0, 165,
			[]result{
				{model.TimeFromUnix(0), model.TimeFromUnix(10) - 1, mockStore(1)},
				{model.TimeFromUnix(10), model.TimeFromUnix(165), mockStore(3)},
			},
		},

		// Test all the various combination we can get when there are three schemas
		{
			cs, 34, 65,
			[]result{
				{model.TimeFromUnix(34), model.TimeFromUnix(65), mockStore(1)},
			},
		},

		{
			cs, 244, 6785,
			[]result{
				{model.TimeFromUnix(244), model.TimeFromUnix(6785), mockStore(3)},
			},
		},

		{
			cs, 34, 165,
			[]result{
				{model.TimeFromUnix(34), model.TimeFromUnix(100) - 1, mockStore(1)},
				{model.TimeFromUnix(100), model.TimeFromUnix(165), mockStore(2)},
			},
		},

		{
			cs, 151, 264,
			[]result{
				{model.TimeFromUnix(151), model.TimeFromUnix(200) - 1, mockStore(2)},
				{model.TimeFromUnix(200), model.TimeFromUnix(264), mockStore(3)},
			},
		},

		{
			cs, 32, 264,
			[]result{
				{model.TimeFromUnix(32), model.TimeFromUnix(100) - 1, mockStore(1)},
				{model.TimeFromUnix(100), model.TimeFromUnix(200) - 1, mockStore(2)},
				{model.TimeFromUnix(200), model.TimeFromUnix(264), mockStore(3)},
			},
		},
	} {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			have := []result{}
			tc.cs.forStores(model.TimeFromUnix(tc.from), model.TimeFromUnix(tc.through), collect(&have))
			if !reflect.DeepEqual(tc.want, have) {
				t.Fatalf("wrong stores - %s", test.Diff(tc.want, have))
			}
		})
	}
}
