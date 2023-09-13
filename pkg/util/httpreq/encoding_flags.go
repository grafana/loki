package httpreq

import (
	"context"
	"net/http"
	"strings"
)

type EncodingFlag string

type EncodingFlags map[EncodingFlag]struct{}

func NewEncodingFlags(flags ...EncodingFlag) EncodingFlags {
	var ef EncodingFlags
	ef.Set(flags...)
	return ef
}

func (ef *EncodingFlags) Set(flags ...EncodingFlag) {
	if *ef == nil {
		*ef = make(EncodingFlags, len(flags))
	}

	for _, flag := range flags {
		(*ef)[flag] = struct{}{}
	}
}

func (ef *EncodingFlags) Has(flag EncodingFlag) bool {
	_, ok := (*ef)[flag]
	return ok
}

const (
	LokiEncodingFlagsHeader              = "X-Loki-Response-Encoding-Flags"
	FlagCategorizeLabels    EncodingFlag = "categorize-labels"

	EncodeFlagsDelimiter = ","
)

func ExtractEncodingFlags(req *http.Request) EncodingFlags {
	rawValue := req.Header.Get(LokiEncodingFlagsHeader)
	if rawValue == "" {
		return nil
	}

	return parseEncodingFlags(rawValue)
}

func ExtractEncodingFlagsFromCtx(ctx context.Context) EncodingFlags {
	rawValue := ExtractHeader(ctx, LokiEncodingFlagsHeader)
	if rawValue == "" {
		return nil
	}

	return parseEncodingFlags(rawValue)
}

func parseEncodingFlags(rawFlags string) EncodingFlags {
	split := strings.Split(rawFlags, EncodeFlagsDelimiter)
	flags := make(EncodingFlags, len(split))
	for _, rawFlag := range split {
		flags.Set(EncodingFlag(rawFlag))
	}
	return flags
}
