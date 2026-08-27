package types

import (
	"errors"

	"github.com/prometheus/prometheus/model/labels"
)

var errInvalidMatchType = errors.New("unable to marshal unrecognized match type")

// LabelMatcherToPromLabel transforms a simple label matcher to a upstream Prometheus label.Matcher
func LabelMatcherToPromLabel(labelMatcherProto *LabelMatcher) (*labels.Matcher, error) {
	var matchType labels.MatchType
	switch labelMatcherProto.Type {
	case LABEL_MATCHER_TYPE_EQ:
		matchType = labels.MatchEqual
	case LABEL_MATCHER_TYPE_NEQ:
		matchType = labels.MatchNotEqual
	case LABEL_MATCHER_TYPE_RE:
		matchType = labels.MatchRegexp
	case LABEL_MATCHER_TYPE_NRE:
		matchType = labels.MatchNotRegexp
	default:
		return nil, errInvalidMatchType
	}

	matcher, err := labels.NewMatcher(matchType, labelMatcherProto.Name, labelMatcherProto.Value)
	if err != nil {
		return nil, err
	}
	return matcher, nil
}

// LabelMatcherFromPromLabel transforms a upstream Prometheus label.Matcher to a simple representation
func LabelMatcherFromPromLabel(matcher *labels.Matcher) (*LabelMatcher, error) {
	var matchType LabelMatcherType
	switch matcher.Type {
	case labels.MatchEqual:
		matchType = LABEL_MATCHER_TYPE_EQ
	case labels.MatchNotEqual:
		matchType = LABEL_MATCHER_TYPE_NEQ
	case labels.MatchRegexp:
		matchType = LABEL_MATCHER_TYPE_RE
	case labels.MatchNotRegexp:
		matchType = LABEL_MATCHER_TYPE_NRE
	default:
		return nil, errInvalidMatchType
	}
	return &LabelMatcher{
		Type:  matchType,
		Name:  matcher.Name,
		Value: matcher.Value,
	}, nil
}
