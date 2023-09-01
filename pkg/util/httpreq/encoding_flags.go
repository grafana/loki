package httpreq

import (
	"context"
	"net/http"
	"strings"

	"github.com/grafana/dskit/middleware"
)

type EncodingFlag string

const (
	LokiEncodeFlagsHeader              = "X-Loki-Response-Encoding-Flags"
	FlagGroupLabels       EncodingFlag = "group_labels"

	EncodeFlagsDelimiter = ","
)

func ExtractEncodeFlags(ctx context.Context) []EncodingFlag {
	value := ExtractHeader(ctx, LokiEncodeFlagsHeader)
	if value == "" {
		return nil
	}

	rawFlags := strings.Split(value, EncodeFlagsDelimiter)
	flags := make([]EncodingFlag, 0, len(rawFlags))
	for _, rawFlag := range rawFlags {
		flags = append(flags, EncodingFlag(rawFlag))
	}
	return flags
}

func EncodingFlagIsSet(flags []EncodingFlag, flag EncodingFlag) bool {
	for _, f := range flags {
		if f == flag {
			return true
		}
	}
	return false
}

func ExtractEncodingFlagsMiddleware() middleware.Interface {
	return middleware.Func(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx := req.Context()
			flags := req.Header.Get(LokiEncodeFlagsHeader)

			if flags != "" {
				ctx = context.WithValue(ctx, headerContextKey(LokiEncodeFlagsHeader), flags)
				req = req.WithContext(ctx)
			}
			next.ServeHTTP(w, req)
		})
	})
}
