package types

import (
	fmt "fmt"
	strconv "strconv"
	strings "strings"
)

type LabelMatcherType int32

const (
	LABEL_MATCHER_TYPE_UNSPECIFIED LabelMatcherType = 0
	LABEL_MATCHER_TYPE_EQ          LabelMatcherType = 1
	LABEL_MATCHER_TYPE_NEQ         LabelMatcherType = 2
	LABEL_MATCHER_TYPE_RE          LabelMatcherType = 3
	LABEL_MATCHER_TYPE_NRE         LabelMatcherType = 4
)

var LabelMatcherType_name = map[int32]string{
	0: "LABEL_MATCHER_TYPE_UNSPECIFIED",
	1: "LABEL_MATCHER_TYPE_EQ",
	2: "LABEL_MATCHER_TYPE_NEQ",
	3: "LABEL_MATCHER_TYPE_RE",
	4: "LABEL_MATCHER_TYPE_NRE",
}

var LabelMatcherType_value = map[string]int32{
	"LABEL_MATCHER_TYPE_UNSPECIFIED": 0,
	"LABEL_MATCHER_TYPE_EQ":          1,
	"LABEL_MATCHER_TYPE_NEQ":         2,
	"LABEL_MATCHER_TYPE_RE":          3,
	"LABEL_MATCHER_TYPE_NRE":         4,
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
	s, ok := LabelMatcherType_name[int32(x)]
	if ok {
		return s
	}
	return strconv.Itoa(int(x))
}

func (this *LabelMatcher) String() string {
	if this == nil {
		return "nil"
	}
	s := strings.Join([]string{`&LabelMatcher{`,
		`Type:` + fmt.Sprintf("%v", this.Type) + `,`,
		`Name:` + fmt.Sprintf("%v", this.Name) + `,`,
		`Value:` + fmt.Sprintf("%v", this.Value) + `,`,
		`}`,
	}, "")
	return s
}

func (this *LabelMatcher) GoString() string {
	if this == nil {
		return "nil"
	}
	s := make([]string, 0, 7)
	s = append(s, "&types.LabelMatcher{")
	s = append(s, "Type: "+fmt.Sprintf("%#v", this.Type)+",\n")
	s = append(s, "Name: "+fmt.Sprintf("%#v", this.Name)+",\n")
	s = append(s, "Value: "+fmt.Sprintf("%#v", this.Value)+",\n")
	s = append(s, "}")
	return strings.Join(s, "")
}

func (this *LabelPolicy) String() string {
	if this == nil {
		return "nil"
	}
	repeatedStringForSelector := "[]*LabelMatcher{"
	for _, f := range this.Selector {
		repeatedStringForSelector += strings.Replace(f.String(), "LabelMatcher", "LabelMatcher", 1) + ","
	}
	repeatedStringForSelector += "}"
	s := strings.Join([]string{`&LabelPolicy{`,
		`Selector:` + repeatedStringForSelector + `,`,
		`}`,
	}, "")
	return s
}
