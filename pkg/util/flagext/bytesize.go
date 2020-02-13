package flagext

import (
	"strings"

	"github.com/c2h5oh/datasize"
)

// ByteSize is a flag parsing compatibility type for constructing human friendly sizes.
// It implements flag.Value & flag.Getter.
type ByteSize uint64

func (bs ByteSize) String() string {
	return datasize.ByteSize(bs).String()
}

func (bs *ByteSize) Set(s string) error {
	var v datasize.ByteSize

	// Bytesize currently doesn't handle things like Mb, but only handles MB.
	// Therefore we capitalize just for convenience
	if err := v.UnmarshalText([]byte(strings.ToUpper(s))); err != nil {
		return err
	}
	*bs = ByteSize(v.Bytes())
	return nil
}

func (bs ByteSize) Get() interface{} {
	return bs.Val()
}

func (bs ByteSize) Val() int {
	return int(bs)
}

/// UnmarshalYAML the Unmarshaler interface of the yaml pkg.
func (bs *ByteSize) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var str string
	err := unmarshal(&str)
	if err != nil {
		return err
	}

	return bs.Set(str)
}
