package querier

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/syntax"
)

func TestMultiTenantQuerier_SelectLogs(t *testing.T) {
	for _, tc := range []struct {
		desc      string
		orgID     string
		expLabels []string
		expLines  []string
	}{
		{
			"two tenants",
			"1|2",
			[]string{
				`{__tenant_id__="1", type="test"}`,
				`{__tenant_id__="1", type="test"}`,
				`{__tenant_id__="2", type="test"}`,
				`{__tenant_id__="2", type="test"}`,
			},
			[]string{"line 1", "line 2", "line 1", "line 2"},
		},
		{
			"one tenant",
			"1",
			[]string{
				`{type="test"}`,
				`{type="test"}`,
			},
			[]string{"line 1", "line 2"},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			querier := newQuerierMock()
			querier.On("SelectLogs", mock.Anything, mock.Anything).Return(func() iter.EntryIterator { return mockStreamIterator(1, 2) }, nil)

			multiTenantQuerier := NewMultiTenantQuerier(querier, log.NewNopLogger())

			ctx := user.InjectOrgID(context.Background(), tc.orgID)
			params := logql.SelectLogParams{QueryRequest: &logproto.QueryRequest{
				Selector:  `{type="test"}`,
				Direction: logproto.BACKWARD,
				Limit:     0,
				Shards:    nil,
				Start:     time.Unix(0, 1),
				End:       time.Unix(0, time.Now().UnixNano()),
			}}
			iter, err := multiTenantQuerier.SelectLogs(ctx, params)
			require.NoError(t, err)

			entriesCount := 0
			for iter.Next() {
				require.Equal(t, tc.expLabels[entriesCount], iter.Labels())
				require.Equal(t, tc.expLines[entriesCount], iter.Entry().Line)
				entriesCount++
			}
			require.Equalf(t, len(tc.expLabels), entriesCount, "Expected %d entries but got %d", len(tc.expLabels), entriesCount)
		})
	}
}

func TestMultiTenantQuerier_SelectSamples(t *testing.T) {
	for _, tc := range []struct {
		desc      string
		orgID     string
		expLabels []string
	}{
		{
			"two tenants",
			"1|2",
			[]string{
				`{__tenant_id__="1", app="foo"}`,
				`{__tenant_id__="2", app="foo"}`,
				`{__tenant_id__="2", app="bar"}`,
				`{__tenant_id__="1", app="bar"}`,
				`{__tenant_id__="1", app="foo"}`,
				`{__tenant_id__="2", app="foo"}`,
				`{__tenant_id__="2", app="bar"}`,
				`{__tenant_id__="1", app="bar"}`,
			},
		},
		{
			"one tenant",
			"1",
			[]string{
				`{app="foo"}`,
				`{app="bar"}`,
				`{app="foo"}`,
				`{app="bar"}`,
			},
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			querier := newQuerierMock()
			querier.On("SelectSamples", mock.Anything, mock.Anything).Return(func() iter.SampleIterator { return newSampleIterator() }, nil)

			multiTenantQuerier := NewMultiTenantQuerier(querier, log.NewNopLogger())

			ctx := user.InjectOrgID(context.Background(), tc.orgID)
			params := logql.SelectSampleParams{}
			iter, err := multiTenantQuerier.SelectSamples(ctx, params)
			require.NoError(t, err)

			entriesCount := 0
			for iter.Next() {
				require.Equalf(t, tc.expLabels[entriesCount], iter.Labels(), "Entry %d", entriesCount)
				entriesCount++
			}
			require.Equalf(t, len(tc.expLabels), entriesCount, "Expected %d entries but got %d", len(tc.expLabels), entriesCount)
		})
	}
}

var samples = []logproto.Sample{
	{Timestamp: time.Unix(2, 0).UnixNano(), Hash: 1, Value: 1.},
	{Timestamp: time.Unix(5, 0).UnixNano(), Hash: 2, Value: 1.},
}

var (
	labelFoo, _ = syntax.ParseLabels("{app=\"foo\"}")
	labelBar, _ = syntax.ParseLabels("{app=\"bar\"}")
)

func newSampleIterator() iter.SampleIterator {
	return iter.NewSortSampleIterator([]iter.SampleIterator{
		iter.NewSeriesIterator(logproto.Series{
			Labels:     labelFoo.String(),
			Samples:    samples,
			StreamHash: labelFoo.Hash(),
		}),
		iter.NewSeriesIterator(logproto.Series{
			Labels:     labelBar.String(),
			Samples:    samples,
			StreamHash: labelBar.Hash(),
		}),
	})
}

func BenchmarkTenantEntryIteratorLabels(b *testing.B) {
	it := NewMockEntryIterator(12)
	tenantIter := NewTenantEntryIterator(it, "tenant_1") 

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		tenantIter.Labels()
	}
}

type mockEntryIterator struct{
	labels string
}

func NewMockEntryIterator(numLabels int) mockEntryIterator {
	builder := labels.NewBuilder(nil)
	for i := 1; i <= numLabels; i++ {
		builder.Set(fmt.Sprintf("label_%d", i), strconv.Itoa(i))
	}
	return mockEntryIterator{ labels: builder.Labels().String()}
}

func (it mockEntryIterator) Labels() string {
	return it.labels
}

func (it mockEntryIterator) Entry() logproto.Entry {
	return logproto.Entry{}
}

func (it mockEntryIterator) Next() bool {
	return true
}

func (it mockEntryIterator) StreamHash() uint64 {
	return 0
}

func (it mockEntryIterator) Error() error {
	return nil
}

func (it mockEntryIterator) Close() error {
	return nil
}
