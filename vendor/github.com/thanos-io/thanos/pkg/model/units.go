// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package model

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
