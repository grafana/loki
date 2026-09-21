package hintprovider

import (
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logline/store"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func metasToIndexRefs(metas []store.Meta) []logproto.IndexRef {
	irs := make([]logproto.IndexRef, len(metas))
	for idx, m := range metas {
		irs[idx] = logproto.IndexRef{
			Date:           m.Date,
			StorageID:      indexRefStorageID(m),
			Version:        m.Version,
			SizeBytes:      m.SizeBytes,
			MinLogTs:       model.TimeFromUnixNano(m.MinLogTs.UnixNano()),
			MaxLogTs:       model.TimeFromUnixNano(m.MaxLogTs.UnixNano()),
			ShardCount:     int64(m.ShardCount),
			ShardAlgorithm: m.ShardAlgorithm,
			ShardValue:     int64(m.ShardValue),
		}
	}
	return irs
}

func indexRefStorageID(m store.Meta) string {
	if m.StorageID != "" {
		return m.StorageID
	}
	return m.Hash
}

func indexRefsToMetas(refs []logproto.IndexRef) []store.Meta {
	metas := make([]store.Meta, len(refs))
	for i, r := range refs {
		metas[i] = store.Meta{
			Date:           r.Date,
			StorageID:      r.StorageID,
			Version:        r.Version,
			SizeBytes:      r.SizeBytes,
			MinLogTs:       r.MinLogTs.Time().UTC(),
			MaxLogTs:       r.MaxLogTs.Time().UTC(),
			ShardCount:     int(r.ShardCount),
			ShardAlgorithm: r.ShardAlgorithm,
			ShardValue:     int(r.ShardValue),
		}
	}
	return metas
}
