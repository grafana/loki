// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/cortexproject/cortex/blob/master/tools/doc-generator/util_test.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Cortex Authors.

package parse

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_findFlagsPrefix(t *testing.T) {
	tests := []struct {
		input    []string
		expected []string
	}{
		{
			input:    []string{},
			expected: []string{},
		},
		{
			input:    []string{""},
			expected: []string{""},
		},
		{
			input:    []string{"", ""},
			expected: []string{"", ""},
		},
		{
			input:    []string{"foo", "foo", "foo"},
			expected: []string{"", "", ""},
		},
		{
			input:    []string{"ruler.endpoint", "alertmanager.endpoint"},
			expected: []string{"ruler", "alertmanager"},
		},
		{
			input:    []string{"ruler.endpoint.address", "alertmanager.endpoint.address"},
			expected: []string{"ruler", "alertmanager"},
		},
		{
			input:    []string{"ruler.first.address", "ruler.second.address"},
			expected: []string{"ruler.first", "ruler.second"},
		},
	}

	for _, test := range tests {
		assert.Equal(t, test.expected, FindFlagsPrefix(test.input))
	}
}
