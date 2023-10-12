package main

import (
	"os"
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

func TestLambdaPromtail_TestDropLabels(t *testing.T) {
	os.Setenv("DROP_LABELS", "A1,A2")

	// Reset the shared global variables
	defer func() {
		os.Unsetenv("DROP_LABELS")
		dropLabels = []model.LabelName{}
	}()

	var err error
	dropLabels, err = getDropLabels()
	require.Nil(t, err)
	require.Contains(t, dropLabels, model.LabelName("A1"))

	defaultLabelSet := model.LabelSet{
		model.LabelName("default"): model.LabelValue("default"),
		model.LabelName("A1"):      model.LabelValue("A1"),
		model.LabelName("B2"):      model.LabelValue("B2"),
	}
	modifiedLabels := applyLabels(defaultLabelSet)
	require.NotContains(t, modifiedLabels, model.LabelName("A1"))
	require.Contains(t, modifiedLabels, model.LabelName("B2"))
}
