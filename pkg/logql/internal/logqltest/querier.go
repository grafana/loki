package logqltest

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compactor/deletion"
	"github.com/grafana/loki/v3/pkg/compactor/deletion/deletionproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/querier"
	"github.com/grafana/loki/v3/pkg/validation"
)

// newScriptQuerier returns the production querier over stores.
//
// From dataObjStart on, the querier reads stream-first queries from data objects. It reads
// everything else from chunks. A zero-value dataObjStart makes it read only chunks.
//
// The querier is valid until the next stores.setStreams, which stops its chunk store.
//
// The data objects and the chunks both hold every stream, so the querier's routing alone decides
// which one a sample comes from.
func newScriptQuerier(t *testing.T, stores *scriptStores, dataObjStart time.Time) logql.Querier {
	t.Helper()
	chunks := stores.chunks

	var cfg querier.Config
	flagext.DefaultValues(&cfg)
	// The stack runs no ingesters, so the querier reads only the store.
	cfg.QueryStoreOnly = true

	var limits validation.Limits
	flagext.DefaultValues(&limits)

	var dataObjStore querier.Store
	// A script that loads no stream has no data objects to read.
	if !dataObjStart.IsZero() && stores.dataObjBucket != nil {
		cfg.DataObjEnabled = true
		limits.DataObjQueryStartTime = flagext.Time(dataObjStart)

		store, err := querier.NewDataObjStore(chunks.store, stores.dataObjBucket, stores.dataObjMetastore, nil)
		require.NoError(t, err)
		dataObjStore = store
	}

	overrides, err := validation.NewOverrides(limits, nil)
	require.NoError(t, err)

	q, err := querier.New(cfg, chunks.store, dataObjStore, nil, overrides, noDeletes{}, log.NewNopLogger(), nil, 0, 0)
	require.NoError(t, err)

	return q
}

// noDeletes is a deletion.DeleteGetter with no delete requests.
type noDeletes struct{}

func (noDeletes) GetAllDeleteRequestsForUser(context.Context, string, bool, *deletion.TimeRange) ([]deletionproto.DeleteRequest, error) {
	return nil, nil
}
