package log

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_parseNet4(t *testing.T) {
	cases := []struct {
		name           string
		pat            string
		expMin, expMax [4]uint
	}{
		{
			name:   "single IPv4",
			pat:    "127.0.0.1",
			expMin: [4]uint{127, 0, 0, 1},
			expMax: [4]uint{127, 0, 0, 1},
		},
		{
			name:   "IPv4 Range",
			pat:    "127.0.0.1-127.0.0.5",
			expMin: [4]uint{127, 0, 0, 1},
			expMax: [4]uint{127, 0, 0, 5},
		},
		{
			name:   "IPv4 CIDR format",
			pat:    "192.168.4.1/16",
			expMin: [4]uint{192, 168, 0, 0},
			expMax: [4]uint{192, 168, 255, 255},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseNet4(c.pat)
			require.NoError(t, err)
			exp := &net4{
				min: uint32(buildIP4(c.expMin)),
				max: uint32(buildIP4(c.expMax)),
			}
			assert.Equal(t, exp, got)
		})
	}
}

func Test_ip(t *testing.T) {
	cases := []struct {
		name     string
		pat      string
		input    []string
		expected []int // matched line indexes from the input
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
			got := make([]int, 0)
			for i, in := range c.input {
				ok, err := ip(c.pat, in)
				require.NoError(t, err)
				if ok {
					got = append(got, i)
				}
			}
			assert.Equal(t, c.expected, got)
		})
	}
}
