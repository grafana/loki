package logqltest

import (
	"testing"

	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/objtest"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/querier"
)

// newTestingDataObjStoreWithStreams builds data objects from streams and returns a store that
// reads them. It hands the requests it does not serve to chunks.
//
// It returns nil when there are no streams.
func newTestingDataObjStoreWithStreams(t *testing.T, chunks *testingChunkStore, streams []logproto.Stream) querier.Store {
	t.Helper()
	if len(streams) == 0 {
		return nil
	}

	builder := objtest.NewBuilder(t)
	builder.Append(user.InjectOrgID(t.Context(), tenant), streams...)
	builder.Close()

	store, err := querier.NewDataObjStore(chunks.store, builder.Location().Bucket, builder.Metastore(), nil)
	require.NoError(t, err)
	return store
}
