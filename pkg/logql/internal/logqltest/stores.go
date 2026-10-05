package logqltest

import (
	"testing"

	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// scriptStores holds the stores of a script's streams: a chunk store and the data objects. Every
// execution stack of a script reads them. Building them is expensive, and the stacks only read
// them, so a script builds them once per set of streams.
type scriptStores struct {
	t *testing.T

	chunks *testingChunkStore

	// dataObjBucket and dataObjMetastore are nil when there are no streams.
	dataObjBucket    objstore.Bucket
	dataObjMetastore metastore.Metastore
}

func newScriptStores(t *testing.T) *scriptStores {
	return &scriptStores{t: t}
}

// setStreams rebuilds the stores with streams. It stops the previous chunk store. No query runs
// between evals, so that store is idle.
func (s *scriptStores) setStreams(streams []logproto.Stream) {
	if s.chunks != nil {
		s.chunks.close()
	}

	s.chunks = newTestingChunkStoreWithStreams(s.t, streams)
	s.dataObjBucket, s.dataObjMetastore = nil, nil
	if len(streams) > 0 {
		s.dataObjBucket, s.dataObjMetastore = newTestingDataObjectsWithStreams(s.t, streams)
	}
}
