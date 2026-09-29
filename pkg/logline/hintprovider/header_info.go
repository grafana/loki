package hintprovider

import (
	"github.com/grafana/loki/v3/pkg/logline/format"
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
