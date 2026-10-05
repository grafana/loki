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
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/querier"
	"github.com/grafana/loki/v3/pkg/validation"
)

// testingQuerier serves queries through the production querier over the streams of a script.
//
// The querier reads from chunks, and optionally reads stream-first metric queries from data
// objects. The data objects and the chunks both hold every stream, so the querier's routing alone
// decides which one a sample comes from.
type testingQuerier struct {
	chunks *testingChunkStore
	q      *querier.SingleTenantQuerier
}

// newScriptQuerier builds a testingQuerier over streams. The querier reads the tenant's
// stream-first data from dataObjStart on from data objects. A zero dataObjStart makes it read
// only chunks.
func newScriptQuerier(t *testing.T, streams []logproto.Stream, dataObjStart time.Time) *testingQuerier {
	t.Helper()
	chunks := newTestingChunkStoreWithStreams(t, streams)

	var cfg querier.Config
	flagext.DefaultValues(&cfg)
	// The stack runs no ingesters, so the querier reads only the store.
	cfg.QueryStoreOnly = true

	var limits validation.Limits
	flagext.DefaultValues(&limits)

	var dataObjStore querier.Store
	if !dataObjStart.IsZero() {
		cfg.DataObjEnabled = true
		limits.DataObjQueryStartTime = flagext.Time(dataObjStart)
		dataObjStore = newTestingDataObjStoreWithStreams(t, chunks, streams)
	}

	overrides, err := validation.NewOverrides(limits, nil)
	require.NoError(t, err)

	q, err := querier.New(cfg, chunks.store, dataObjStore, nil, overrides, noDeletes{}, log.NewNopLogger(), nil, 0, 0)
	require.NoError(t, err)

	return &testingQuerier{chunks: chunks, q: q}
}

func (s *testingQuerier) querier() logql.Querier {
	return s.q
}

// close stops the querier's stores. It is safe to call more than once: setStreams closes the
// previous querier on refresh, and a t.Cleanup closes the final one at test end.
func (s *testingQuerier) close() {
	s.chunks.close()
}

// noDeletes is a deletion.DeleteGetter with no delete requests.
type noDeletes struct{}

func (noDeletes) GetAllDeleteRequestsForUser(context.Context, string, bool, *deletion.TimeRange) ([]deletionproto.DeleteRequest, error) {
	return nil, nil
}
