package logqltest

import (
	"testing"

	"github.com/grafana/dskit/user"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/objtest"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// newTestingDataObjectsWithStreams builds data objects from streams. It returns the bucket that
// holds them and the metastore that resolves them.
//
// streams must not be empty, because objtest.Builder.Close fails on a builder with no logs.
func newTestingDataObjectsWithStreams(t *testing.T, streams []logproto.Stream) (objstore.Bucket, metastore.Metastore) {
	t.Helper()

	builder := objtest.NewBuilder(t)
	builder.Append(user.InjectOrgID(t.Context(), tenant), streams...)
	builder.Close()

	return builder.Location().Bucket, builder.Metastore()
}
