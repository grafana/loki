package querier

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
)

func TestMultiTenantQuerier_SelectLogs(t *testing.T) {
	querier := newQuerierMock()
	querier.On("SelectLogs", mock.Anything, mock.Anything).Return(func() iter.EntryIterator { return mockStreamIterator(1, 2) }, nil)

	multi_tenant := NewMultiTenantQuerier(querier, log.NewNopLogger())

	ctx := user.InjectOrgID(context.Background(), "1|2")
	params := logql.SelectLogParams{QueryRequest: &logproto.QueryRequest{
		Selector:  `{type="test"}`,
		Direction: logproto.BACKWARD,
		Limit:     0,
		Shards:    nil,
		Start:     time.Unix(0, 1),
		End:       time.Unix(0, time.Now().UnixNano()),
	}}
	iter, err := multi_tenant.SelectLogs(ctx, params)
	require.NoError(t, err)

	iter.Next()
	require.Equal(t, `{type="test", __tenant_id__="1"}`, iter.Labels())
	iter.Next()
	require.Equal(t, `{type="test", __tenant_id__="1"}`, iter.Labels())
	iter.Next()
	require.Equal(t, `{type="test", __tenant_id__="2"}`, iter.Labels())
	iter.Next()
	require.Equal(t, `{type="test", __tenant_id__="2"}`, iter.Labels())
}
