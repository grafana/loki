package chunk

import (
	"context"
	"sort"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
)

// Store for chunks.
type Store interface {
	Put(ctx context.Context, chunks []Chunk) error
	PutOne(ctx context.Context, from, through model.Time, chunk Chunk) error
	Get(tx context.Context, from, through model.Time, matchers ...*labels.Matcher) ([]Chunk, error)
	Stop()
}

// compositeStore is a Store which delegates to various stores depending
// on when they were activated.
type compositeStore struct {
	stores []compositeStoreEntry
}

type compositeStoreEntry struct {
	start model.Time
	Store
}

type byStart []compositeStoreEntry

func (a byStart) Len() int           { return len(a) }
func (a byStart) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byStart) Less(i, j int) bool { return a[i].start < a[j].start }

// SchemaOpt stores when a schema starts.
type SchemaOpt struct {
	From     model.Time
	NewStore func(StorageClient) (Store, error)
}

// SchemaOpts returns the schemas and the times when they activate.
func SchemaOpts(cfg StoreConfig, schemaCfg SchemaConfig) []SchemaOpt {
	opts := []SchemaOpt{{
		From: 0,
		NewStore: func(storage StorageClient) (Store, error) {
			return newStore(cfg, v1Schema(schemaCfg), storage)
		},
	}}

	if schemaCfg.DailyBucketsFrom.IsSet() {
		opts = append(opts, SchemaOpt{
			From: schemaCfg.DailyBucketsFrom.Time,
			NewStore: func(storage StorageClient) (Store, error) {
				return newStore(cfg, v2Schema(schemaCfg), storage)
			},
		})
	}

	if schemaCfg.Base64ValuesFrom.IsSet() {
		opts = append(opts, SchemaOpt{
			From: schemaCfg.Base64ValuesFrom.Time,
			NewStore: func(storage StorageClient) (Store, error) {
				return newStore(cfg, v3Schema(schemaCfg), storage)
			},
		})
	}

	if schemaCfg.V4SchemaFrom.IsSet() {
		opts = append(opts, SchemaOpt{
			From: schemaCfg.V4SchemaFrom.Time,
			NewStore: func(storage StorageClient) (Store, error) {
				return newStore(cfg, v4Schema(schemaCfg), storage)
			},
		})
	}

	if schemaCfg.V5SchemaFrom.IsSet() {
		opts = append(opts, SchemaOpt{
			From: schemaCfg.V5SchemaFrom.Time,
			NewStore: func(storage StorageClient) (Store, error) {
				return newStore(cfg, v5Schema(schemaCfg), storage)
			},
		})
	}

	if schemaCfg.V6SchemaFrom.IsSet() {
		opts = append(opts, SchemaOpt{
			From: schemaCfg.V6SchemaFrom.Time,
			NewStore: func(storage StorageClient) (Store, error) {
				return newStore(cfg, v6Schema(schemaCfg), storage)
			},
		})
	}

	if schemaCfg.V7SchemaFrom.IsSet() {
		opts = append(opts, SchemaOpt{
			From: schemaCfg.V7SchemaFrom.Time,
			NewStore: func(storage StorageClient) (Store, error) {
				return newStore(cfg, v7Schema(schemaCfg), storage)
			},
		})
	}

	if schemaCfg.V8SchemaFrom.IsSet() {
		opts = append(opts, SchemaOpt{
			From: schemaCfg.V8SchemaFrom.Time,
			NewStore: func(storage StorageClient) (Store, error) {
				return newStore(cfg, v8Schema(schemaCfg), storage)
			},
		})
	}

	if schemaCfg.V9SchemaFrom.IsSet() {
		opts = append(opts, SchemaOpt{
			From: schemaCfg.V9SchemaFrom.Time,
			NewStore: func(storage StorageClient) (Store, error) {
				return newSeriesStore(cfg, v9Schema(schemaCfg), storage)
			},
		})
	}

	return opts
}

// StorageOpt stores when a StorageClient is to be used.
type StorageOpt struct {
	From   model.Time
	Client StorageClient
}

// NewStore creates a new Store which delegates to different stores depending
// on time.
func NewStore(cfg StoreConfig, schemaCfg SchemaConfig, schemaOpts []SchemaOpt, storageOpts []StorageOpt) (Store, error) {
	stores := []compositeStoreEntry{}
	from := schemaOpts[0].From

	i, j := 0, 0
	for i+1 < len(schemaOpts) && j+1 < len(storageOpts) {
		currSchema := schemaOpts[i]
		currStorage := storageOpts[j]

		nextSchema := schemaOpts[i+1]
		nextStorage := storageOpts[j+1]

		store, err := currSchema.NewStore(currStorage.Client)
		if err != nil {
			return nil, err
		}
		stores = append(stores, compositeStoreEntry{from, store})

		if nextSchema.From.Before(nextStorage.From) {
			from = nextSchema.From
			i++
		} else if nextSchema.From.After(nextStorage.From) {
			from = nextStorage.From
			j++
		} else {
			from = nextSchema.From
			i++
			j++
		}
	}

	// Now cover the remaining schemas and storages.
	for i < len(schemaOpts) && j < len(storageOpts) {
		store, err := schemaOpts[i].NewStore(storageOpts[j].Client)
		if err != nil {
			return nil, err
		}
		stores = append(stores, compositeStoreEntry{from, store})

		// No more storageOpts.
		if j+1 == len(storageOpts) {
			i++
			if i < len(schemaOpts) {
				from = schemaOpts[i].From
			}
			continue
		}

		// No more schemaOpts. No comparison as we'll enter this after the exit of previous loop.
		j++
		if j < len(storageOpts) {
			from = storageOpts[j].From
		}
	}

	return compositeStore{stores}, nil
}

func (c compositeStore) Put(ctx context.Context, chunks []Chunk) error {
	for _, chunk := range chunks {
		err := c.forStores(chunk.From, chunk.Through, func(from, through model.Time, store Store) error {
			return store.PutOne(ctx, from, through, chunk)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (c compositeStore) PutOne(ctx context.Context, from, through model.Time, chunk Chunk) error {
	return c.forStores(from, through, func(from, through model.Time, store Store) error {
		return store.PutOne(ctx, from, through, chunk)
	})
}

func (c compositeStore) Get(ctx context.Context, from, through model.Time, matchers ...*labels.Matcher) ([]Chunk, error) {
	var results []Chunk
	err := c.forStores(from, through, func(from, through model.Time, store Store) error {
		chunks, err := store.Get(ctx, from, through, matchers...)
		if err != nil {
			return err
		}
		results = append(results, chunks...)
		return nil
	})
	return results, err
}

func (c compositeStore) Stop() {
	for _, store := range c.stores {
		store.Stop()
	}
}

func (c compositeStore) forStores(from, through model.Time, callback func(from, through model.Time, store Store) error) error {
	if len(c.stores) == 0 {
		return nil
	}

	// first, find the schema with the highest start _before or at_ from
	i := sort.Search(len(c.stores), func(i int) bool {
		return c.stores[i].start > from
	})
	if i > 0 {
		i--
	} else {
		// This could happen if we get passed a sample from before 1970.
		i = 0
		from = c.stores[0].start
	}

	// next, find the schema with the lowest start _after_ through
	j := sort.Search(len(c.stores), func(j int) bool {
		return c.stores[j].start > through
	})

	min := func(a, b model.Time) model.Time {
		if a < b {
			return a
		}
		return b
	}

	start := from
	for ; i < j; i++ {
		nextSchemaStarts := model.Latest
		if i+1 < len(c.stores) {
			nextSchemaStarts = c.stores[i+1].start
		}

		// If the next schema starts at the same time as this one,
		// skip this one.
		if nextSchemaStarts == c.stores[i].start {
			continue
		}

		end := min(through, nextSchemaStarts-1)
		err := callback(start, end, c.stores[i].Store)
		if err != nil {
			return err
		}

		start = nextSchemaStarts
	}

	return nil
}
