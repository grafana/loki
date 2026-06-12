package types

import (
	fmt "fmt"
	strconv "strconv"
	strings "strings"
)

type LabelMatcherType int32

//nolint:revive // Converted from protobuf
const (
	LABEL_MATCHER_TYPE_UNSPECIFIED LabelMatcherType = 0
	LABEL_MATCHER_TYPE_EQ          LabelMatcherType = 1
	LABEL_MATCHER_TYPE_NEQ         LabelMatcherType = 2
	LABEL_MATCHER_TYPE_RE          LabelMatcherType = 3
	LABEL_MATCHER_TYPE_NRE         LabelMatcherType = 4
)

var LabelMatcherTypeName = map[int32]string{
	0: "LABEL_MATCHER_TYPE_UNSPECIFIED",
	1: "LABEL_MATCHER_TYPE_EQ",
	2: "LABEL_MATCHER_TYPE_NEQ",
	3: "LABEL_MATCHER_TYPE_RE",
	4: "LABEL_MATCHER_TYPE_NRE",
}

// LabelMatcher is a simple representation of a label matcher, originally
// matching a backend protobuf but now just a standalone struct
// the format is similar to the prometheus type but the type numbers
// are not the same
type LabelMatcher struct {
	Type  LabelMatcherType `json:"type,omitempty"`
	Name  string           `json:"name,omitempty"`
	Value string           `json:"value,omitempty"`
}

type LabelPolicy struct {
	Selector []*LabelMatcher `json:"selector,omitempty"`
}

func (x LabelMatcherType) String() string {
	s, ok := LabelMatcherTypeName[int32(x)]
	if ok {
		return s
	}
	return strconv.Itoa(int(x))
}

func (m *LabelMatcher) String() string {
	if m == nil {
		return "nil"
	}
	s := strings.Join([]string{`&LabelMatcher{`,
		`Type:` + fmt.Sprintf("%v", m.Type) + `,`,
		`Name:` + fmt.Sprintf("%v", m.Name) + `,`,
		`Value:` + fmt.Sprintf("%v", m.Value) + `,`,
		`}`,
	}, "")
	return s
}

func (m *LabelMatcher) GoString() string {
	if m == nil {
		return "nil"
	}
	s := make([]string, 0, 7)
	s = append(s, "&types.LabelMatcher{")
	s = append(s, "Type: "+fmt.Sprintf("%#v", m.Type)+",\n")
	s = append(s, "Name: "+fmt.Sprintf("%#v", m.Name)+",\n")
	s = append(s, "Value: "+fmt.Sprintf("%#v", m.Value)+",\n")
	s = append(s, "}")
	return strings.Join(s, "")
}

func (p *LabelPolicy) String() string {
	if p == nil {
		return "nil"
	}
	repeatedStringForSelector := "[]*LabelMatcher{"
	for _, f := range p.Selector {
		repeatedStringForSelector += strings.Replace(f.String(), "LabelMatcher", "LabelMatcher", 1) + ","
	}
	repeatedStringForSelector += "}"
	s := strings.Join([]string{`&LabelPolicy{`,
		`Selector:` + repeatedStringForSelector + `,`,
		`}`,
	}, "")
	return s
}
