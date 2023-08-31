package loghttp

import "net/url"

type EncodingFlag string

const (
	EncodeFlags     string       = "encode_flags"
	FlagGroupLabels EncodingFlag = "group_labels"
)

func GetEncodingFlags(values url.Values) []EncodingFlag {
	flags := make([]EncodingFlag, 0, len(values[EncodeFlags]))
	for _, value := range values[EncodeFlags] {
		flags = append(flags, EncodingFlag(value))
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
