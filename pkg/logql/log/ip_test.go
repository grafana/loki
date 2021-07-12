package log

import (
	"testing"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_IPFilter(t *testing.T) {
	cases := []struct {
		name     string
		pat      string
		input    []string
		expected []int // matched line indexes from the input

		// in case of expecting error
		err  error
		fail bool
	}{
		{
			name: "single IP",
			pat:  "192.168.0.1",
			input: []string{
				"vpn 192.168.0.5 connected to vm",
				"vpn 192.168.0.1 connected to vm",
				"x",
				"hello world!",
				"",
			},
			expected: []int{1}, // should match with only line at index `1` from the input
		},
		{
			name: "IP range",
			pat:  "192.168.0.1-192.189.10.12",
			input: []string{
				"vpn 192.168.0.0 connected to vm",
				"vpn 192.168.0.1 connected to vm",
				"vpn 192.172.6.1 connected to vm",
				"vpn 192.255.255.255 connected to vm",
				"x",
				"hello world!",
				"",
			},
			expected: []int{1, 2},
		},
		{
			name: "wrong IP range syntax extra space", // NOTE(kavi): Should we handle this as normal valid range pattern?
			pat:  "192.168.0.1 - 192.189.10.12",
			fail: true,
			err:  errInvalidPattern,
		},
		{
			name: "CIDR",
			pat:  "192.168.4.5/16",
			input: []string{
				"vpn 192.168.0.0 connected to vm",
				"vpn 192.168.0.1 connected to vm",
				"vpn 192.172.6.1 connected to vm",
				"vpn 192.168.255.255 connected to vm",
				"x",
				"hello world!",
				"",
			},
			expected: []int{0, 1, 3},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ip, err := NewIPFilter(c.pat)
			if c.fail {
				assert.Error(t, c.err, err)
				return
			}
			require.NoError(t, err)

			got := make([]int, 0)
			for i, in := range c.input {
				if ip.Filter(in) {
					got = append(got, i)
				}
			}
			assert.Equal(t, c.expected, got)
		})
	}
}

func Benchmark_IPFilter(b *testing.B) {
	b.ReportAllocs()

	line := [][]byte{
		[]byte(`vpn 192.168.0.0 connected to vm`),
		/// todo add more cases,long line without match, with match etc....
	}
	lbbb := NewBaseLabelsBuilder()
	lbb := lbbb.ForLabels(labels.Labels{labels.Label{Name: "foo", Value: "bar"}}, 0)

	for _, pattern := range []string{
		"127.0.0.1",
		"192.168.0.1-192.189.10.12",
		"192.168.4.5/16",
	} {
		b.Run(pattern, func(b *testing.B) {
			stage, err := NewIPFilter(pattern)
			require.Nil(b, err)
			b.ResetTimer()

			for n := 0; n < b.N; n++ {
				for _, l := range line {
					lbb.Reset()
					_, _ = stage.Process(l, lbb)
				}
			}
		})
	}

}
