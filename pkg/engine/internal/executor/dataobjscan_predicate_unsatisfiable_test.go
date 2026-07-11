package executor

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func Test_executeDataObjScan_unsatisfiablePredicate(t *testing.T) {
	obj := buildDataobj(t, []logproto.Stream{
		{
			Labels: `{service="loki"}`,
			Entries: []logproto.Entry{
				{Timestamp: mustTimeUnix(5), Line: "hello"},
			},
		},
	})

	bucket := objstore.NewInMemBucket()
	const objectPath = "objects/test"
	reader, err := obj.Reader(context.Background())
	require.NoError(t, err)
	require.NoError(t, bucket.Upload(context.Background(), objectPath, reader))
	require.NoError(t, reader.Close())

	ctx := user.InjectOrgID(t.Context(), "tenant")
	c := &Context{
		bucket:    bucket,
		batchSize: 512,
		logger:    log.NewNopLogger(),
	}

	pipeline := c.executeDataObjScan(ctx, &physical.DataObjScan{
		Location:  objectPath,
		Section:   0,
		StreamIDs: []int64{1},
		Predicates: []physical.Expression{
			physical.NewLiteral(false),
		},
	})

	_, err = pipeline.Read(ctx)
	require.ErrorIs(t, err, EOF)
}

func mustTimeUnix(sec int64) time.Time {
	return time.Unix(sec, 0).UTC()
}
