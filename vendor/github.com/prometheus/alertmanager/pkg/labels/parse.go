// Copyright 2018 Prometheus Team
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package labels

import (
	"regexp"
	"strings"

	"github.com/pkg/errors"
)

var (
	re      = regexp.MustCompile(`(?:\s?)(\w+)(=|=~|!=|!~)(?:\"([^"=~!]+)\"|([^"=~!]+)|\"\")`)
	typeMap = map[string]MatchType{
		"=":  MatchEqual,
		"!=": MatchNotEqual,
		"=~": MatchRegexp,
		"!~": MatchNotRegexp,
	}
)

func ParseMatchers(s string) ([]*Matcher, error) {
	matchers := []*Matcher{}
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")

	var insideQuotes bool
	var token string
	var tokens []string
	for _, r := range s {
		if !insideQuotes && r == ',' {
			tokens = append(tokens, token)
			token = ""
			continue
		}
		token += string(r)
		if r == '"' {
			insideQuotes = !insideQuotes
		}
	}
	if token != "" {
		tokens = append(tokens, token)
	}
	for _, token := range tokens {
		m, err := ParseMatcher(token)
		if err != nil {
			return nil, err
		}
		matchers = append(matchers, m)
	}

	return matchers, nil
}

func ParseMatcher(s string) (*Matcher, error) {
	var (
		name, value string
		matchType   MatchType
	)

	ms := re.FindStringSubmatch(s)
	if len(ms) < 4 {
		return nil, errors.Errorf("bad matcher format: %s", s)
	}

	name = ms[1]
	if name == "" {
		return nil, errors.New("failed to parse label name")
	}

	matchType, found := typeMap[ms[2]]
	if !found {
		return nil, errors.New("failed to find match operator")
	}

	if ms[3] != "" {
		value = ms[3]
	} else {
		value = ms[4]
	}

	return NewMatcher(matchType, name, value)
}
