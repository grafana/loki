package logql

import "testing"

func TestMappingEquivalence(t *testing.T) {
	for _, tc := range []struct {
		query string
	}{} {
		q := NewMockQuerier(
			16,
			randomStreams(500, 200, 16, []string{"a", "b", "c", "d"}),
		)
		t.Run(tc.query, func(t *testing.T) {
		})
	}
}
