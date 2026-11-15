package hintprovider

import (
	"strings"

	"github.com/grafana/loki/v3/pkg/logline/format"
	"github.com/grafana/loki/v3/pkg/logline/store"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func toProtoHeader(h *format.HeaderInfo) *logproto.HeaderInfo {
	if h == nil {
		return nil
	}
	return &logproto.HeaderInfo{
		Version:              h.Version,
		Flags:                h.Flags,
		DocumentCount:        h.DocumentCount,
		TermBlockCount:       h.TermBlockCount,
		PostingsBlockCount:   h.PostingsBlockCount,
		PostingsCompression:  h.PostingsCompression,
		TermCount:            h.TermCount,
		PostingsDataSize:     h.PostingsDataSize,
		TermDataSize:         h.TermDataSize,
		DocMetadataSize:      h.DocMetadataSize,
		TermBlockDirSize:     h.TermBlockDirSize,
		PostingsBlockDirSize: h.PostingsBlockDirSize,
	}
}

func fromProtoHeader(h *logproto.HeaderInfo) format.HeaderInfo {
	if h == nil {
		return format.HeaderInfo{}
	}
	return format.HeaderInfo{
		Version:              h.Version,
		Flags:                h.Flags,
		DocumentCount:        h.DocumentCount,
		TermBlockCount:       h.TermBlockCount,
		PostingsBlockCount:   h.PostingsBlockCount,
		PostingsCompression:  h.PostingsCompression,
		TermCount:            h.TermCount,
		PostingsDataSize:     h.PostingsDataSize,
		TermDataSize:         h.TermDataSize,
		DocMetadataSize:      h.DocMetadataSize,
		TermBlockDirSize:     h.TermBlockDirSize,
		PostingsBlockDirSize: h.PostingsBlockDirSize,
	}
}

func toProtoIndexMeta(m store.Meta) logproto.IndexMeta {
	return logproto.IndexMeta{
		ID:             m.ID(),
		Version:        m.Version,
		SizeBytes:      m.SizeBytes,
		MinLogTs:       m.MinLogTs,
		MaxLogTs:       m.MaxLogTs,
		ShardCount:     int64(m.ShardCount),
		ShardAlgorithm: m.ShardAlgorithm,
		ShardValue:     int64(m.ShardValue),
		IndexHeader:    toProtoHeader(m.IndexHeader),
	}
}

func ToProtoIndexMetas(ms []store.Meta) []logproto.IndexMeta {
	out := make([]logproto.IndexMeta, len(ms))
	for i, m := range ms {
		out[i] = toProtoIndexMeta(m)
	}
	return out
}

// fromProtoIndexMeta rebuilds a catalog Meta from a wire IndexMeta. The result
// is partial (no Hash, record timestamps, or Validate) and is only used to
// query already-selected indexes.
func fromProtoIndexMeta(idx logproto.IndexMeta) store.Meta {
	date, storageID, _ := strings.Cut(idx.ID, "/")
	var header *format.HeaderInfo
	if idx.IndexHeader != nil {
		h := fromProtoHeader(idx.IndexHeader)
		header = &h
	}
	return store.Meta{
		Date:           date,
		StorageID:      storageID,
		Version:        idx.Version,
		SizeBytes:      idx.SizeBytes,
		MinLogTs:       idx.MinLogTs,
		MaxLogTs:       idx.MaxLogTs,
		ShardCount:     int(idx.ShardCount),
		ShardAlgorithm: idx.ShardAlgorithm,
		ShardValue:     int(idx.ShardValue),
		IndexHeader:    header,
	}
}

func fromProtoIndexMetas(idxs []logproto.IndexMeta) []store.Meta {
	out := make([]store.Meta, len(idxs))
	for i, idx := range idxs {
		out[i] = fromProtoIndexMeta(idx)
	}
	return out
}
