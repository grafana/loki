package chunk

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
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

type dummyStoreAndClient struct {
	client        StorageClient
	schemaVersion int
}

func (dummyStoreAndClient) Put(ctx context.Context, chunks []Chunk) error { return nil }
func (dummyStoreAndClient) PutOne(ctx context.Context, from, through model.Time, chunk Chunk) error {
	return nil
}
func (dummyStoreAndClient) Get(tx context.Context, from, through model.Time, matchers ...*labels.Matcher) ([]Chunk, error) {
	return nil, nil
}
func (dummyStoreAndClient) Stop() {}

func (dummyStoreAndClient) NewWriteBatch() WriteBatch                    { return nil }
func (dummyStoreAndClient) BatchWrite(context.Context, WriteBatch) error { return nil }
func (dummyStoreAndClient) QueryPages(ctx context.Context, query IndexQuery, callback func(result ReadBatch) (shouldContinue bool)) error {
	return nil
}
func (dummyStoreAndClient) PutChunks(ctx context.Context, chunks []Chunk) error { return nil }
func (dummyStoreAndClient) GetChunks(ctx context.Context, chunks []Chunk) ([]Chunk, error) {
	return nil, nil
}

func TestNewStoreTimeConvergence(t *testing.T) {
	oldClient, newClient := dummyStoreAndClient{schemaVersion: -1}, dummyStoreAndClient{schemaVersion: -2} // Just to make sure they are not equal.

	type expectation struct {
		time          int64
		schemaVersion int
		client        dummyStoreAndClient
	}

	testcases := []struct {
		schemaTimes  []int64
		storageTimes []int64

		exp []expectation
	}{
		{
			schemaTimes:  []int64{0, 10, 20},
			storageTimes: []int64{0},

			exp: []expectation{
				{
					time:          0,
					schemaVersion: 0,
					client:        oldClient,
				},
				{
					time:          10,
					schemaVersion: 1,
					client:        oldClient,
				},
				{
					time:          20,
					schemaVersion: 2,
					client:        oldClient,
				},
			},
		},
		{
			schemaTimes:  []int64{0, 10, 20, 40},
			storageTimes: []int64{0, 30},

			exp: []expectation{
				{
					time:          0,
					schemaVersion: 0,
					client:        oldClient,
				},
				{
					time:          10,
					schemaVersion: 1,
					client:        oldClient,
				},
				{
					time:          20,
					schemaVersion: 2,
					client:        oldClient,
				},
				{
					time:          30,
					schemaVersion: 2,
					client:        newClient,
				},
				{
					time:          40,
					schemaVersion: 3,
					client:        newClient,
				},
			},
		},
		{
			schemaTimes:  []int64{0, 10, 20, 40},
			storageTimes: []int64{0, 20},

			exp: []expectation{
				{
					time:          0,
					schemaVersion: 0,
					client:        oldClient,
				},
				{
					time:          10,
					schemaVersion: 1,
					client:        oldClient,
				},
				{
					time:          20,
					schemaVersion: 2,
					client:        newClient,
				},
				{
					time:          40,
					schemaVersion: 3,
					client:        newClient,
				},
			},
		},
	}

	for _, testcase := range testcases {
		storageOpts := []StorageOpt{{model.TimeFromUnix(testcase.storageTimes[0]), oldClient}}
		if len(testcase.storageTimes) > 1 {
			storageOpts = append(storageOpts, StorageOpt{model.TimeFromUnix(testcase.storageTimes[1]), newClient})
		}

		schemaOpts := make([]SchemaOpt, 0, len(testcase.schemaTimes))
		for i, schemaTime := range testcase.schemaTimes {
			store := dummyStoreAndClient{schemaVersion: i}

			schemaOpts = append(schemaOpts, SchemaOpt{
				model.TimeFromUnix(schemaTime),
				func(storage StorageClient) (Store, error) {
					store.client = storage
					return store, nil
				},
			})
		}

		store, err := NewStore(StoreConfig{}, SchemaConfig{}, schemaOpts, storageOpts)
		require.NoError(t, err)
		cs := store.(compositeStore)
		require.Equal(t, len(testcase.exp), len(cs.stores))

		for i, store := range cs.stores {
			require.Equal(t, model.TimeFromUnix(testcase.exp[i].time), store.start)
			require.Equal(t, testcase.exp[i].schemaVersion, store.Store.(dummyStoreAndClient).schemaVersion)
			require.Equal(t, testcase.exp[i].client, store.Store.(dummyStoreAndClient).client)
		}
	}

}
