package compute_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/grafana/regexp"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/compute"
	"github.com/grafana/loki/v3/pkg/compute/internal/computetest"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestCompute(t *testing.T) {
	require.NoError(t, filepath.WalkDir("testdata", func(path string, d fs.DirEntry, walkErr error) error {
		require.NoError(t, walkErr)

		if d.IsDir() {
			return nil
		} else if filepath.Ext(d.Name()) != ".test" {
			return nil
		}

		f, err := os.Open(path)
		require.NoError(t, err)
		defer f.Close()

		cases, err := computetest.ParseCases(f)
		require.NoError(t, err)

		for _, tc := range cases {
			t.Run(fmt.Sprintf("%s/source=%s:%d", tc.Function, d.Name(), tc.Line), func(t *testing.T) {
				var alloc memory.Allocator

				result, err := evalCaseFunction(t, &alloc, tc)
				require.NoError(t, err)

				columnartest.RequireDatumsEqual(t, tc.Expect, result)
			})
		}

		return nil
	}))
}

func evalCaseFunction(t *testing.T, alloc *memory.Allocator, tc computetest.Case) (columnar.Datum, error) {
	t.Helper()

	switch tc.Function {
	case "EQ":
		require.Len(t, tc.Arguments, 2, "EQ function requires two arguments")
		return compute.Equals(alloc, tc.Arguments[0], tc.Arguments[1], tc.Selection)
	case "NEQ":
		require.Len(t, tc.Arguments, 2, "NEQ function requires two arguments")
		return compute.NotEquals(alloc, tc.Arguments[0], tc.Arguments[1], tc.Selection)
	case "LT":
		require.Len(t, tc.Arguments, 2, "LT function requires two arguments")
		return compute.LessThan(alloc, tc.Arguments[0], tc.Arguments[1], tc.Selection)
	case "LTE":
		require.Len(t, tc.Arguments, 2, "LTE function requires two arguments")
		return compute.LessOrEqual(alloc, tc.Arguments[0], tc.Arguments[1], tc.Selection)
	case "GT":
		require.Len(t, tc.Arguments, 2, "GT function requires two arguments")
		return compute.GreaterThan(alloc, tc.Arguments[0], tc.Arguments[1], tc.Selection)
	case "GTE":
		require.Len(t, tc.Arguments, 2, "GTE function requires two arguments")
		return compute.GreaterOrEqual(alloc, tc.Arguments[0], tc.Arguments[1], tc.Selection)
	case "NOT":
		require.Len(t, tc.Arguments, 1, "NOT function requires one argument")
		return compute.Not(alloc, tc.Arguments[0], tc.Selection)
	case "AND":
		require.Len(t, tc.Arguments, 2, "AND function requires two arguments")
		return compute.And(alloc, tc.Arguments[0], tc.Arguments[1], tc.Selection)
	case "OR":
		require.Len(t, tc.Arguments, 2, "OR function requires two arguments")
		return compute.Or(alloc, tc.Arguments[0], tc.Arguments[1], tc.Selection)
	case "SUBSTR":
		require.Len(t, tc.Arguments, 2, "SUBSTR function requires two arguments")
		return compute.Substr(alloc, tc.Arguments[0], tc.Arguments[1], tc.Selection)
	case "SUBSTRI":
		require.Len(t, tc.Arguments, 2, "SUBSTRI function requires two arguments")
		return compute.SubstrInsensitive(alloc, tc.Arguments[0], tc.Arguments[1], tc.Selection)
	case "REGEXP":
		require.Len(t, tc.Arguments, 2, "REGEXP function requires two arguments")
		// Second argument should be a UTF8 scalar containing the regex pattern
		patternScalar, ok := tc.Arguments[1].(*columnar.UTF8Scalar)
		require.True(t, ok, "REGEXP pattern must be a UTF8 scalar")

		var re *regexp.Regexp
		if !patternScalar.IsNull() {
			var err error
			re, err = regexp.Compile(string(patternScalar.Value))
			require.NoError(t, err)
		}

		return compute.RegexpMatch(alloc, tc.Arguments[0], re, tc.Selection)
	case "ISMEMBER":
		require.Len(t, tc.Arguments, 2, "ISMEMBER function requires two arguments")
		// Second argument should be an array that we convert to a Set
		var set *columnar.Set
		switch arr := tc.Arguments[1].(type) {
		case *columnar.UTF8:
			values := make([]string, arr.Len())
			require.Equal(t, 0, arr.Nulls(), "ISMEMBER set must not contain null values")
			for i := 0; i < arr.Len(); i++ {
				values[i] = string(arr.Get(i))
			}
			set = columnar.NewUTF8Set(values...)
		case *columnar.Number[int64]:
			values := make([]int64, arr.Len())
			require.Equal(t, 0, arr.Nulls(), "ISMEMBER set must not contain null values")
			for i := 0; i < arr.Len(); i++ {
				values[i] = arr.Get(i)
			}
			set = columnar.NewNumberSet(values...)
		case *columnar.Number[uint64]:
			values := make([]uint64, arr.Len())
			require.Equal(t, 0, arr.Nulls(), "ISMEMBER set must not contain null values")
			for i := 0; i < arr.Len(); i++ {
				values[i] = arr.Get(i)
			}
			set = columnar.NewNumberSet(values...)
		default:
			require.FailNow(t, "unsupported type for ISMEMBER set", "got %T", tc.Arguments[1])
		}

		return compute.IsMember(alloc, tc.Arguments[0], set, tc.Selection)
	}

	require.FailNow(t, "unexpected function name", "unexpected function name %s", tc.Function)
	panic("unreachable")
}
