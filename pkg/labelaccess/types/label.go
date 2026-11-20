package types

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/prometheus/prometheus/model/labels"
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
	Selector []*labels.Matcher
}

// labelPolicyJSON is the JSON encoding of LabelPolicy. It keeps the encoding that was used when
// Selector held LabelMatchers, so that components running different versions can exchange it.
type labelPolicyJSON struct {
	Selector []*LabelMatcher `json:"selector,omitempty"`
}

var errNilMatcher = errors.New("label policy has a nil matcher")

// MarshalJSON encodes the policy using LabelMatcher's encoding of each matcher.
func (p LabelPolicy) MarshalJSON() ([]byte, error) {
	enc := labelPolicyJSON{Selector: make([]*LabelMatcher, len(p.Selector))}
	for i, m := range p.Selector {
		lm, err := toLabelMatcher(m)
		if err != nil {
			return nil, err
		}
		enc.Selector[i] = lm
	}
	return json.Marshal(enc)
}

// toLabelMatcher converts m to a LabelMatcher, so that it can be encoded as before Selector held
// compiled matchers.
func toLabelMatcher(m *labels.Matcher) (*LabelMatcher, error) {
	if m == nil {
		return nil, errNilMatcher
	}
	return LabelMatcherFromPromLabel(m)
}

// UnmarshalJSON decodes and compiles the policy's matchers. It returns an error if any matcher has
// an unknown match type or an invalid regex.
func (p *LabelPolicy) UnmarshalJSON(data []byte) error {
	var dec labelPolicyJSON
	if err := json.Unmarshal(data, &dec); err != nil {
		return err
	}
	var selector []*labels.Matcher
	if len(dec.Selector) > 0 {
		selector = make([]*labels.Matcher, len(dec.Selector))
	}
	for i, lm := range dec.Selector {
		if lm == nil {
			return errNilMatcher
		}
		m, err := LabelMatcherToPromLabel(lm)
		if err != nil {
			return fmt.Errorf("invalid label policy matcher for label %q: %w", lm.Name, err)
		}
		selector[i] = m
	}
	p.Selector = selector
	return nil
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

// String returns the same representation as when Selector held LabelMatchers. It is used to build
// cache keys, so it must not change.
func (p *LabelPolicy) String() string {
	if p == nil {
		return "nil"
	}
	var sb strings.Builder
	sb.WriteString("&LabelPolicy{Selector:[]*LabelMatcher{")
	for _, m := range p.Selector {
		if lm, err := toLabelMatcher(m); err == nil {
			sb.WriteString(lm.String())
		} else if m == nil {
			sb.WriteString("nil")
		} else {
			// A matcher with an unknown match type has no LabelMatcher form.
			sb.WriteString(m.String())
		}
		sb.WriteByte(',')
	}
	sb.WriteString("},}")
	return sb.String()
}
