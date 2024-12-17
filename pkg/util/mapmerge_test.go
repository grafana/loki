package util

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMerge(t *testing.T) {
	var base = map[string]string{
		"a": "b",
		"c": "10",
		"y": "z",
	}

	var overlay = map[string]string{
		"a": "z",
		"c": "10",
		"d": "e",
	}

	merged := MergeMaps(base, overlay)
	require.Equal(t, merged, map[string]string{
		"a": "z",
		"c": "10",
		"d": "e",
		"y": "z",
	})
}

func TestCopy(t *testing.T) {
	var base = map[string]string{
		"a": "b",
		"c": "10",
		"y": "z",
	}

	cp := CopyMap(base)
	require.EqualValues(t, base, cp)
	require.NotSame(t, &base, &cp)
}

func TestNilCopy(t *testing.T) {
	var base map[string]string

	cp := CopyMap(base)
	require.EqualValues(t, base, cp)
	require.NotSame(t, &base, &cp)
}

func TestNilBase(t *testing.T) {
	var overlay = map[string]string{
		"a": "z",
		"c": "10",
		"d": "e",
	}

	merged := MergeMaps(nil, overlay)
	require.Equal(t, merged, overlay)
}

func TestNilOverlay(t *testing.T) {
	var base = map[string]string{
		"a": "b",
		"c": "10",
		"y": "z",
	}

	merged := MergeMaps(base, nil)
	require.Equal(t, merged, base)
}

// TestImmutability tests that both given maps are unaltered
func TestImmutability(t *testing.T) {
	var base = map[string]string{
		"a": "b",
		"c": "10",
		"y": "z",
	}

	var overlay = map[string]string{
		"a": "z",
		"c": "10",
		"d": "e",
	}

	beforeBase := CopyMap(base)
	beforeOverlay := CopyMap(overlay)
	require.EqualValues(t, base, beforeBase)
	require.EqualValues(t, overlay, beforeOverlay)

	MergeMaps(base, overlay)

	require.EqualValues(t, base, beforeBase)
	require.EqualValues(t, overlay, beforeOverlay)
}
