//go:build gofuzz
// +build gofuzz

package syntax

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

const fuzzTestCaseEnvName = "FUZZ_TESTCASE_PATH"

func Test_Fuzz(t *testing.T) {
	fuzzTestPath := os.Getenv(fuzzTestCaseEnvName)
	data, err := os.ReadFile(fuzzTestPath)
	require.NoError(t, err)
	_, _ = ParseExpr(string(data))
}
