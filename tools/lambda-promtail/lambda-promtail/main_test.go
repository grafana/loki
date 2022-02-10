package main

import (
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	err := os.Setenv("WRITE_ADDRESS", "http://localhost:8080/loki/api/v1/push")
	if err != nil {
		return
	}

	exitVal := m.Run()

	os.Exit(exitVal)
}

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
	require.Errorf(t, err, "Invalid value for environment variable EXTRA_LABELS. Expected a comma seperated list with an even number of entries. ")
}

func TestLambdaPromtail_TestParseLabelsNoneProvided(t *testing.T) {
	extraLabels, err := parseExtraLabels("")
	require.Len(t, extraLabels, 0)
	require.Nil(t, err)
}
