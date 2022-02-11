package main

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestLambdaPromtail_TestParseLabelsValid(t *testing.T) {
	extraLabels, err := parseExtraLabels("A,a,B,b,C,c,D,d")
	require.Nil(t, err)
	require.Len(t, extraLabels, 4)
	require.Equal(t, ExtraLabel{key: "A", value: "a"}, extraLabels[0])
	require.Equal(t, ExtraLabel{key: "B", value: "b"}, extraLabels[1])
	require.Equal(t, ExtraLabel{key: "C", value: "c"}, extraLabels[2])
	require.Equal(t, ExtraLabel{key: "D", value: "d"}, extraLabels[3])
}

func TestLambdaPromtail_TestParseLabelsNotValid(t *testing.T) {
	extraLabels, err := parseExtraLabels("A,a,B,b,C,c,D")
	require.Nil(t, extraLabels)
	require.Errorf(t, err, invalidExtraLabelsError)
}

func TestLambdaPromtail_TestParseLabelsNoneProvided(t *testing.T) {
	extraLabels, err := parseExtraLabels("")
	require.Len(t, extraLabels, 0)
	require.Nil(t, err)
}
