package tsdb

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

type noopTSDBManager struct{ NoopIndex }

func (noopTSDBManager) BuildFromWALs(_ time.Time, _ []WALIdentifier) error { return nil }

func chunkMetasToChunkRefs(user string, fp uint64, xs index.ChunkMetas) (res []ChunkRef) {
	for _, x := range xs {
		res = append(res, ChunkRef{
			User:        user,
			Fingerprint: model.Fingerprint(fp),
			Start:       x.From(),
			End:         x.Through(),
			Checksum:    x.Checksum,
		})
	}
	return
}

// Test append
func Test_TenantHeads_Append(t *testing.T) {
	h := newTenantHeads(time.Now(), defaultHeadManagerStripeSize, NewHeadMetrics(nil), log.NewNopLogger())
	ls := mustParseLabels(`{foo="bar"}`)
	chks := []index.ChunkMeta{
		{
			Checksum: 0,
			MinTime:  1,
			MaxTime:  10,
			KB:       2,
			Entries:  30,
		},
	}
	_ = h.Append("fake", ls, chks)

	found, err := h.GetChunkRefs(
		context.Background(),
		"fake",
		0,
		100,
		nil, nil,
		labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"),
	)
	require.Nil(t, err)
	require.Equal(t, chunkMetasToChunkRefs("fake", ls.Hash(), chks), found)

}

// Test multitenant reads
func Test_TenantHeads_MultiRead(t *testing.T) {
	h := newTenantHeads(time.Now(), defaultHeadManagerStripeSize, NewHeadMetrics(nil), log.NewNopLogger())
	ls := mustParseLabels(`{foo="bar"}`)
	chks := []index.ChunkMeta{
		{
			Checksum: 0,
			MinTime:  1,
			MaxTime:  10,
			KB:       2,
			Entries:  30,
		},
	}

	tenants := []struct {
		user string
		ls   labels.Labels
	}{
		{
			user: "tenant1",
			ls: append(ls.Copy(), labels.Label{
				Name:  "tenant",
				Value: "tenant1",
			}),
		},
		{
			user: "tenant2",
			ls: append(ls.Copy(), labels.Label{
				Name:  "tenant",
				Value: "tenant2",
			}),
		},
	}

	// add data for both tenants
	for _, tenant := range tenants {
		_ = h.Append(tenant.user, tenant.ls, chks)

	}

	// ensure we're only returned the data from the correct tenant
	for _, tenant := range tenants {
		found, err := h.GetChunkRefs(
			context.Background(),
			tenant.user,
			0,
			100,
			nil, nil,
			labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"),
		)
		require.Nil(t, err)
		require.Equal(t, chunkMetasToChunkRefs(tenant.user, tenant.ls.Hash(), chks), found)
	}

}

// test head recover from wal

// test mgr recover from multiple wals across multiple periods
