package main

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestLambdaPromtail_ExtraLabelsValid(t *testing.T) {
	extraLabels, err := parseExtraLabels("A1,a,B2,b,C3,c,D4,d", false)
	require.Nil(t, err)
	require.Len(t, extraLabels, 4)
	require.Equal(t, model.LabelValue("a"), extraLabels["__extra_A1"])
	require.Equal(t, model.LabelValue("b"), extraLabels["__extra_B2"])
	require.Equal(t, model.LabelValue("c"), extraLabels["__extra_C3"])
	require.Equal(t, model.LabelValue("d"), extraLabels["__extra_D4"])
}

func TestLambdaPromtail_ExtraLabelsMissingValue(t *testing.T) {
	extraLabels, err := parseExtraLabels("A,a,B,b,C,c,D", false)
	require.Nil(t, extraLabels)
	require.Errorf(t, err, invalidExtraLabelsError)
}

func TestLambdaPromtail_ExtraLabelsInvalidNames(t *testing.T) {
	extraLabels, err := parseExtraLabels("A!,%a,B?,$b,C-,c^", false)
	require.Nil(t, extraLabels)
	require.Errorf(t, err, "invalid name \"__extra_A!\"")
}

func TestLambdaPromtail_TestParseLabelsNoneProvided(t *testing.T) {
	extraLabels, err := parseExtraLabels("", false)
	require.Len(t, extraLabels, 0)
	require.Nil(t, err)
}
