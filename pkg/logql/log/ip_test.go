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
			name: "single IPv4",
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
			name: "single IPv6",
			pat:  "::1", // localhost address
			input: []string{
				"vpn ::1 connected to vm", // still a match
				"vpn 192.168.0.1 connected to vm",
				"x",
				"hello world!",
				"",
			},
			expected: []int{0}, // should match with only line at index `0` from the input

		},
		{
			name: "IPv4 range",
			pat:  "192.168.0.1-192.189.10.12",
			input: []string{
				"vpn 192.168.0.0 connected to vm",
				"vpn 192.168.0.0 192.168.0.1 connected to vm", // match
				"vpn 192.172.6.1 connected to vm",             // match
				"vpn 192.255.255.255 connected to vm",
				"x",
				"hello world!",
				"",
			},
			expected: []int{1, 2},
		},
		{
			name: "IPv6 range",
			pat:  "2001:db8::1-2001:db8::8",
			input: []string{
				"vpn 192.168.0.0 connected to vm", // not match
				"vpn 2001:db8::2 connected to vm", // match
				"vpn 2001:db8::9 connected to vm", // not match
				"vpn 2001:db8::5 connected to vm", // match
				"x",
				"hello world!",
				"",
			},
			expected: []int{1, 3},
		},
		{
			name: "wrong IP range syntax extra space", // NOTE(kavi): Should we handle this as normal valid range pattern?
			pat:  "192.168.0.1 - 192.189.10.12",
			fail: true,
			err:  ErrIPFilterInvalidPattern,
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
		{
			name: "CIDR IPv6",
			pat:  "2001:db8::/32",
			input: []string{
				"vpn 2001:db8::1 connected to vm",                 // match
				"vpn 2001:db9::3 connected to vm",                 // not a match
				"vpn 2001:dc8::1 and 2001:db8::2 connected to vm", // firt not match, but second did match. So overall its a match
				"vpn 192.168.255.255 connected to vm",             // not match
				"x",
				"hello world!",
				"",
			},
			expected: []int{0, 2},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ip := newIPFilter(c.pat)
			if c.fail {
				assert.Error(t, c.err, ip.patternError)
				return
			}
			assert.NoError(t, ip.patternError)

			got := make([]int, 0)
			for i, in := range c.input {
				if ip.filter([]byte(in)) {
					got = append(got, i)
				}
			}
			assert.Equal(t, c.expected, got)
		})
	}
}

func Test_IPLabelFilterOp(t *testing.T) {
	cases := []struct {
		name          string
		pat           string
		op            LabelFilterType
		label         string
		val           []byte
		expectedMatch bool

		fail bool
		err  string // label filter errors are just treated as strings :(
	}{
		{
			name:          "equal operator",
			pat:           "192.168.0.1",
			op:            LabelFilterEqual,
			label:         "addr",
			val:           []byte("192.168.0.1"),
			expectedMatch: true,
		},
		{
			name:          "not equal operator",
			pat:           "192.168.0.2",
			op:            LabelFilterNotEqual,
			label:         "addr",
			val:           []byte("192.168.0.1"), // match because !=ip("192.168.0.2")
			expectedMatch: true,
		},
		{
			name:  "great than operator-not-supported",
			pat:   "192.168.0.2",
			op:    LabelFilterGreaterThan, // not supported
			label: "addr",
			val:   []byte("192.168.0.1"),
			fail:  true,
			err:   ErrIPFilterInvalidOperation.Error(),
		},
		{
			name:  "great than or equal to operator-not-supported",
			pat:   "192.168.0.2",
			op:    LabelFilterGreaterThanOrEqual, // not supported
			label: "addr",
			val:   []byte("192.168.0.1"),
			fail:  true,
			err:   ErrIPFilterInvalidOperation.Error(),
		},
		{
			name:  "less than operator-not-supported",
			pat:   "192.168.0.2",
			op:    LabelFilterLesserThan, // not supported
			label: "addr",
			val:   []byte("192.168.0.1"),
			fail:  true,
			err:   ErrIPFilterInvalidOperation.Error(),
		},
		{
			name:  "less than or equal to operator-not-supported",
			pat:   "192.168.0.2",
			op:    LabelFilterLesserThanOrEqual, // not supported
			label: "addr",
			val:   []byte("192.168.0.1"),
			fail:  true,
			err:   ErrIPFilterInvalidOperation.Error(),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lf := NewIPLabelFilter(c.label, c.op, c.pat)
			require.NoError(t, lf.patternError)

			lbs := labels.Labels{labels.Label{Name: c.label, Value: string(c.val)}}
			lbb := NewBaseLabelsBuilder().ForLabels(lbs, lbs.Hash())
			_, ok := lf.Process([]byte("x"), lbb)
			if c.fail {
				assert.NotEmpty(t, lbb.GetErr())
				assert.False(t, ok)
				return
			}
			require.Empty(t, lbb.GetErr())
			assert.Equal(t, c.expectedMatch, ok)
		})
	}
}

func Benchmark_IPFilter(b *testing.B) {
	b.ReportAllocs()

	line := [][]byte{
		[]byte(`vpn 192.168.0.0 connected to vm`),
		[]byte(`vpn <missing-ip> connected to vm just wanted to make some long line without match`),
		[]byte(`vpn ::1 connected to vm just wanted to make some long line with match at the end 127.0.0.1`),
	}
	lbbb := NewBaseLabelsBuilder()
	lbb := lbbb.ForLabels(labels.Labels{labels.Label{Name: "foo", Value: "bar"}}, 0)

	for _, pattern := range []string{
		"127.0.0.1",
		"192.168.0.1-192.189.10.12",
		"192.168.4.5/16",
	} {
		b.Run(pattern, func(b *testing.B) {
			stage := newIPFilter(pattern)
			require.Nil(b, stage.patternError)
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
