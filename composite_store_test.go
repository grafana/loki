package chunk

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/assert"
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

type dummy struct {
	version int

	// Include nil-implementations of these interfaces so I don't have to stub out
	// the methods.
	StorageClient
	Store
}

func dummySchema(from model.Time, version int) SchemaOpt {
	return SchemaOpt{
		From: from,
		NewStore: func(s StorageClient) (Store, error) {
			return dummy{
				version:       version,
				StorageClient: s,
			}, nil
		},
	}
}

func TestNewStoreTimeConvergence(t *testing.T) {
	oldClient := dummy{version: -1}
	newClient := dummy{version: -2}
	newerClient := dummy{version: -3}

	type expectation struct {
		time   model.Time
		schema int
		client StorageClient
	}

	for i, testcase := range []struct {
		schemaOpts  []SchemaOpt
		storageOpts []StorageOpt

		expected []expectation
	}{
		{
			schemaOpts: []SchemaOpt{
				dummySchema(0, 1),
			},
			storageOpts: []StorageOpt{
				{0, oldClient},
			},
			expected: []expectation{
				{0, 1, oldClient},
			},
		},
		{
			schemaOpts: []SchemaOpt{
				dummySchema(0, 1),
				dummySchema(10, 2),
			},
			storageOpts: []StorageOpt{
				{0, oldClient},
			},
			expected: []expectation{
				{0, 1, oldClient},
				{10, 2, oldClient},
			},
		},
		{
			schemaOpts: []SchemaOpt{
				dummySchema(0, 1),
			},
			storageOpts: []StorageOpt{
				{0, oldClient},
				{10, newClient},
			},
			expected: []expectation{
				{0, 1, oldClient},
				{10, 1, newClient},
			},
		},
		{
			schemaOpts: []SchemaOpt{
				dummySchema(0, 1),
				dummySchema(10, 2),
			},
			storageOpts: []StorageOpt{
				{0, oldClient},
				{10, newClient},
			},
			expected: []expectation{
				{0, 1, oldClient},
				{10, 2, newClient},
			},
		},
		{
			schemaOpts: []SchemaOpt{
				dummySchema(0, 1),
				dummySchema(20, 2),
			},
			storageOpts: []StorageOpt{
				{0, oldClient},
				{10, newClient},
				{30, newerClient},
			},
			expected: []expectation{
				{0, 1, oldClient},
				{10, 1, newClient},
				{20, 2, newClient},
				{30, 2, newerClient},
			},
		},
		{
			schemaOpts: []SchemaOpt{
				dummySchema(0, 1),
				dummySchema(10, 2),
				dummySchema(30, 3),
			},
			storageOpts: []StorageOpt{
				{0, oldClient},
				{20, newClient},
			},
			expected: []expectation{
				{0, 1, oldClient},
				{10, 2, oldClient},
				{20, 2, newClient},
				{30, 3, newClient},
			},
		},
		{
			schemaOpts: []SchemaOpt{
				dummySchema(0, 1),
				dummySchema(10, 2),
				dummySchema(20, 3),
				dummySchema(40, 4),
			},
			storageOpts: []StorageOpt{
				{0, oldClient},
				{20, newClient},
			},
			expected: []expectation{
				{0, 1, oldClient},
				{10, 2, oldClient},
				{20, 3, newClient},
				{40, 4, newClient},
			},
		},
	} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			store, err := newCompositeStore(StoreConfig{}, SchemaConfig{}, testcase.schemaOpts, testcase.storageOpts)
			require.NoError(t, err)
			cs := store.(compositeStore)
			require.Equal(t, len(testcase.expected), len(cs.stores))

			for i, store := range cs.stores {
				assert.Equal(t, testcase.expected[i].time, store.start, "%d", i)
				assert.Equal(t, testcase.expected[i].schema, store.Store.(dummy).version, "%d", i)
				assert.Equal(t, testcase.expected[i].client, store.Store.(dummy).StorageClient, "%d", i)
			}
		})
	}
}
