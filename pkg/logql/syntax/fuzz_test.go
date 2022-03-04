//go:build gofuzz
// +build gofuzz

package logql

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

const fuzzTestCaseEnvName = "FUZZ_TESTCASE_PATH"

func Test_Fuzz(t *testing.T) {
	fuzzTestPath := os.Getenv(fuzzTestCaseEnvName)
	data, err := ioutil.ReadFile(fuzzTestPath)
	require.NoError(t, err)
	_, _ = ParseExpr(string(data))
}
