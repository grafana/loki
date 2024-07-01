package flagext

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

type customType int

// Parse implements ListValue.
func (l customType) Parse(s string) (any, error) {
	v, err := strconv.Atoi(s)
	if err != nil {
		return customType(0), err
	}
	return customType(v), nil
}

// String implements ListValue.
func (l customType) String() string {
	return strconv.Itoa(int(l))
}

var _ ListValue = customType(0)

func Test_CSV(t *testing.T) {
	for _, tc := range []struct {
		in  string
		err bool
		out []customType
	}{
		{
			in:  "",
			err: false,
			out: nil,
		},
		{
			in:  ",",
			err: true,
			out: []customType{},
		},
		{
			in:  "1",
			err: false,
			out: []customType{1},
		},
		{
			in:  "1,2",
			err: false,
			out: []customType{1, 2},
		},
		{
			in:  "1,",
			err: true,
			out: []customType{},
		},
		{
			in:  ",1",
			err: true,
			out: []customType{},
		},
	} {
		t.Run(tc.in, func(t *testing.T) {
			var v CSV[customType]

			err := v.Set(tc.in)
			if tc.err {
				require.NotNil(t, err)
			} else {
				require.Nil(t, err)
				require.Equal(t, tc.out, v.Get())
			}

		})
	}

}
