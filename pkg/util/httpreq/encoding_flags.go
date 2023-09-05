package httpreq

import (
	"net/http"
	"strings"
)

type EncodingFlag string

const (
	LokiEncodingFlagsHeader              = "X-Loki-Response-Encoding-Flags"
	FlagCategorizeLabels    EncodingFlag = "categorize-labels"

	EncodeFlagsDelimiter = ","
)

func ExtractEncodingFlags(req *http.Request) []EncodingFlag {
	// We try to extract the flags from the header first.
	// If the header is not set, we try to extract the flags from the context.
	var rawValue string
	if rawValue = req.Header.Get(LokiEncodingFlagsHeader); rawValue == "" {
		if rawValue = ExtractHeader(req.Context(), LokiEncodingFlagsHeader); rawValue == "" {
			return nil
		}
	}

	rawFlags := strings.Split(rawValue, EncodeFlagsDelimiter)
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
