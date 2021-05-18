package pattern

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_matcher_Matches(t *testing.T) {
	for _, tt := range []struct {
		expr     string
		in       string
		expected []string
	}{
		{
			"foo <foo> bar",
			"foo buzz bar",
			[]string{"buzz"},
		},
		{
			"foo <foo> bar<fuzz>",
			"foo buzz bar",
			[]string{"buzz", ""},
		},
		{
			"<foo> bar<fuzz>",
			" bar",
			[]string{"", ""},
		},
		{
			`<ip> <userid> <user> [<_>] "<method> <path> <_>" <status> <size>`,
			`127.0.0.1 user-identifier frank [10/Oct/2000:13:55:36 -0700] "GET /apache_pb.gif HTTP/1.0" 200 2326`,
			[]string{"127.0.0.1", "user-identifier", "frank", "GET", "/apache_pb.gif", "200", "2326"},
		},
	} {
		tt := tt
		t.Run(tt.expr, func(t *testing.T) {
			m, err := New(tt.expr)
			require.NoError(t, err)
			actual := m.Matches([]byte(tt.in))
			var actualStrings []string
			for _, a := range actual {
				actualStrings = append(actualStrings, string(a))
			}
			require.Equal(t, tt.expected, actualStrings)
		})
	}
}
