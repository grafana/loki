package jsonpath

import (
	"fmt"
	"go.yaml.in/yaml/v4"
	"strings"
)

type segmentKind int

const (
	segmentKindChild                 segmentKind = iota // .
	segmentKindDescendant                               // ..
	segmentKindProperyName                              // ~ (extension only)
	segmentKindParent                                   // ^ (JSONPath Plus parent selector)
	segmentKindRecursivePropertyName                    // .~ (Spectral/JSONPath Plus recursive property names)
)

type segment struct {
	kind       segmentKind
	child      *innerSegment
	descendant *innerSegment
}

type segmentSubKind int

const (
	segmentDotWildcard   segmentSubKind = iota // .*
	segmentDotMemberName                       // .property
	segmentLongHand                            // [ selector[] ]
)

func (s segment) ToString() string {
	switch s.kind {
	case segmentKindChild:
		if s.child.kind != segmentLongHand {
			return "." + s.child.ToString()
		} else {
			return s.child.ToString()
		}
	case segmentKindDescendant:
		return ".." + s.descendant.ToString()
	case segmentKindProperyName:
		return "~"
	case segmentKindParent:
		return "^"
	case segmentKindRecursivePropertyName:
		return ".~"
	}
	panic("unknown segment kind")
}

type innerSegment struct {
	kind      segmentSubKind
	dotName   string
	selectors []*selector
}

func (s innerSegment) ToString() string {
	builder := strings.Builder{}
	switch s.kind {
	case segmentDotWildcard:
		builder.WriteString("*")
		break
	case segmentDotMemberName:
		builder.WriteString(s.dotName)
		break
	case segmentLongHand:
		builder.WriteString("[")
		for i, selector := range s.selectors {
			builder.WriteString(selector.ToString())
			if i < len(s.selectors)-1 {
				builder.WriteString(", ")
			}
		}
		builder.WriteString("]")
		break
	default:
		panic("unknown child segment kind")
	}
	return builder.String()
}

func descendApply(value *yaml.Node, apply func(*yaml.Node)) {
	if value == nil {
		return
	}
	apply(value)
	for _, child := range value.Content {
		descendApply(child, apply)
	}
}

func (s segment) IsSingular() bool {
	switch s.kind {
	case segmentKindDescendant, segmentKindRecursivePropertyName:
		return false
	case segmentKindParent:
		return true
	case segmentKindProperyName:
		return false
	case segmentKindChild:
		if s.child == nil {
			return false
		}
		return s.child.IsSingular()
	default:
		return false
	}
}

func (s innerSegment) IsSingular() bool {
	switch s.kind {
	case segmentDotWildcard:
		return false
	case segmentDotMemberName:
		return true
	case segmentLongHand:
		if len(s.selectors) != 1 {
			return false
		}
		return s.selectors[0].IsSingular()
	default:
		return false
	}
}

func (s selector) IsSingular() bool {
	switch s.kind {
	case selectorSubKindName:
		return true
	case selectorSubKindArrayIndex:
		return true
	case selectorSubKindWildcard:
		return false
	case selectorSubKindArraySlice:
		return false
	case selectorSubKindFilter:
		return false
	default:
		return false
	}
}

func (s segment) getSegmentInfo() ([]SegmentInfo, error) {
	switch s.kind {
	case segmentKindChild:
		if s.child == nil {
			return nil, fmt.Errorf("nil child segment")
		}
		return s.child.getSegmentInfo()
	case segmentKindDescendant:
		return nil, fmt.Errorf("recursive descent not supported for upsert")
	case segmentKindParent, segmentKindProperyName, segmentKindRecursivePropertyName:
		return nil, fmt.Errorf("parent/property selectors not supported for upsert")
	default:
		return nil, fmt.Errorf("unknown segment kind")
	}
}

func (s innerSegment) getSegmentInfo() ([]SegmentInfo, error) {
	switch s.kind {
	case segmentDotMemberName:
		return []SegmentInfo{{Kind: SegmentKindMemberName, Key: s.dotName}}, nil
	case segmentLongHand:
		if len(s.selectors) != 1 {
			return nil, fmt.Errorf("multiple selectors not supported for upsert")
		}
		return s.selectors[0].getSegmentInfo()
	case segmentDotWildcard:
		return nil, fmt.Errorf("wildcard not supported for upsert")
	default:
		return nil, fmt.Errorf("unknown inner segment kind")
	}
}

func (s selector) getSegmentInfo() ([]SegmentInfo, error) {
	switch s.kind {
	case selectorSubKindName:
		return []SegmentInfo{{Kind: SegmentKindMemberName, Key: s.name}}, nil
	case selectorSubKindArrayIndex:
		return []SegmentInfo{{Kind: SegmentKindArrayIndex, Index: s.index, HasIndex: true}}, nil
	case selectorSubKindWildcard, selectorSubKindArraySlice, selectorSubKindFilter:
		return nil, fmt.Errorf("%v selector not supported for upsert", s.kind)
	default:
		return nil, fmt.Errorf("unknown selector kind")
	}
}
