// Provenance-includes-location: https://github.com/thanos-io/thanos/blob/main/pkg/model/units.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Thanos Authors.

package flagext

import (
	"github.com/alecthomas/units"
)

// Bytes is a data type which supports yaml serialization/deserialization
// with units.
type Bytes uint64

func (b *Bytes) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var value string
	err := unmarshal(&value)
	if err != nil {
		return err
	}

	bytes, err := units.ParseBase2Bytes(value)
	if err != nil {
		return err
	}

	*b = Bytes(bytes)
	return nil
}

func (b *Bytes) MarshalYAML() (interface{}, error) {
	return units.Base2Bytes(*b).String(), nil
}
