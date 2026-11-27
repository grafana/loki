package json

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecoder(t *testing.T) {
	const input = `{"a":"x\ny","b":5,"c":[1,"two",null],"a":"dup","d":{"skip":true}}`

	for name, newDecoder := range map[string]func() *Decoder{
		"reader": func() *Decoder { return NewDecoder(strings.NewReader(input)) },
		"pooled": func() *Decoder { return GetDecoder([]byte(input)) },
	} {
		t.Run(name, func(t *testing.T) {
			d := newDecoder()
			if name == "pooled" {
				defer d.Release()
			}
			got := map[string]string{}
			var elems []string
			err := d.EachField(func(key []byte) error {
				switch k := string(key); k {
				case "a", "b":
					v, err := d.ReadScalar()
					got[k] = v
					return err
				case "c":
					return d.EachElement(func() error {
						v, err := d.ReadScalar()
						elems = append(elems, v)
						return err
					})
				default:
					return d.SkipValue()
				}
			})
			require.NoError(t, err)
			require.Equal(t, map[string]string{"a": "dup", "b": "5"}, got) // last duplicate wins
			require.Equal(t, []string{"1", "two", "null"}, elems)
		})
	}
}

func TestDecoderErrors(t *testing.T) {
	d := GetDecoder([]byte(`[1]`))
	defer d.Release()
	require.Error(t, d.EachField(func([]byte) error { return nil }))

	d2 := GetDecoder([]byte(`{"a":{}}`))
	defer d2.Release()
	require.Error(t, d2.EachField(func([]byte) error {
		_, err := d2.ReadScalar()
		return err
	}))

	d3 := GetDecoder([]byte(`5`))
	defer d3.Release()
	_, err := d3.ReadString("want string")
	require.EqualError(t, err, "want string")
}
