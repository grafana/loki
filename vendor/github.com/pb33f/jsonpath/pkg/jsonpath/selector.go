package jsonpath

import (
	"fmt"
	"strconv"
	"strings"
)

type selectorSubKind int

const (
	selectorSubKindWildcard selectorSubKind = iota
	selectorSubKindName
	selectorSubKindArraySlice
	selectorSubKindArrayIndex
	selectorSubKindFilter
)

type slice struct {
	start *int64
	end   *int64
	step  *int64
}

type selector struct {
	kind   selectorSubKind
	name   string
	index  int64
	slice  *slice
	filter *filterSelector
}

func (s selector) ToString() string {
	switch s.kind {
	case selectorSubKindName:
		return "'" + escapeString(s.name) + "'"
	case selectorSubKindArrayIndex:
		// int to string
		return strconv.FormatInt(s.index, 10)
	case selectorSubKindFilter:
		return "?" + s.filter.ToString()
	case selectorSubKindWildcard:
		return "*"
	case selectorSubKindArraySlice:
		builder := strings.Builder{}
		if s.slice.start != nil {
			builder.WriteString(strconv.FormatInt(*s.slice.start, 10))
		}
		builder.WriteString(":")
		if s.slice.end != nil {
			builder.WriteString(strconv.FormatInt(*s.slice.end, 10))
		}

		if s.slice.step != nil {
			builder.WriteString(":")
			builder.WriteString(strconv.FormatInt(*s.slice.step, 10))
		}
		return builder.String()
	default:
		panic(fmt.Sprintf("unimplemented selector kind: %v", s.kind))
	}
	return ""
}
