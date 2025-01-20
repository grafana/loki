package logs

import (
	"sync"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
)

type builderPoolKey struct {
	name    string
	options dataset.BuilderOptions
}

var builderPools sync.Map

type release func()

// getColumnBuilder returns a [*dataset.ColumnBuilder] from a pool. To return
// the builder to the pool, call the returned release function.
func getColumnBuilder(name string, opts dataset.BuilderOptions) (*dataset.ColumnBuilder, release, error) {
	type result struct {
		builder *dataset.ColumnBuilder
		err     error
	}

	key := builderPoolKey{name: name, options: opts}

	val, ok := builderPools.Load(key)
	if !ok {
		newPool := &sync.Pool{
			New: func() interface{} {
				builder, err := dataset.NewColumnBuilder(name, opts)
				return &result{builder: builder, err: err}
			},
		}

		val, _ = builderPools.LoadOrStore(key, newPool)
	}

	pool := val.(*sync.Pool)

	res := pool.Get().(*result)
	res.builder.Reset()

	release := func() { pool.Put(res) }
	return res.builder, release, res.err
}
